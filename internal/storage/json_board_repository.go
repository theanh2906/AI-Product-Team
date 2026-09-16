package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

const boardFileVersion = 1

type boardFile struct {
	Version int            `json:"version"`
	Boards  []kanban.Board `json:"boards"`
}

// JSONBoardRepository is the initial local persistence adapter. Its public
// behavior is intentionally limited to kanban.BoardRepository so a SQL adapter
// can replace it without changing the domain or HTTP layers.
type JSONBoardRepository struct {
	mu        sync.Mutex
	path      string
	directory string
}

func NewJSONBoardRepository(directory string) (*JSONBoardRepository, error) {
	if directory == "" {
		return nil, fmt.Errorf("board data directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create board data directory: %w", err)
	}
	return &JSONBoardRepository{directory: directory, path: filepath.Join(directory, "boards.json")}, nil
}

func (r *JSONBoardRepository) Path() string { return r.path }

func (r *JSONBoardRepository) Create(ctx context.Context, board kanban.Board) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for _, existing := range data.Boards {
		if existing.ID == board.ID || existing.ProjectID == board.ProjectID {
			return kanban.ErrConflict
		}
	}
	data.Boards = append(data.Boards, board)
	return r.writeLocked(data)
}

func (r *JSONBoardRepository) Get(ctx context.Context, id string) (kanban.Board, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return kanban.Board{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return kanban.Board{}, err
	}
	for _, board := range data.Boards {
		if board.ID == id {
			return board, nil
		}
	}
	return kanban.Board{}, kanban.ErrNotFound
}

func (r *JSONBoardRepository) List(ctx context.Context) ([]kanban.Board, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.readLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(data.Boards, func(i, j int) bool { return data.Boards[i].UpdatedAt.After(data.Boards[j].UpdatedAt) })
	return data.Boards, nil
}

func (r *JSONBoardRepository) Update(ctx context.Context, board kanban.Board) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for index := range data.Boards {
		if data.Boards[index].ID == board.ID {
			data.Boards[index] = board
			return r.writeLocked(data)
		}
	}
	return kanban.ErrNotFound
}

func (r *JSONBoardRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.readLocked()
	if err != nil {
		return err
	}
	for index := range data.Boards {
		if data.Boards[index].ID == id {
			data.Boards = append(data.Boards[:index], data.Boards[index+1:]...)
			return r.writeLocked(data)
		}
	}
	return kanban.ErrNotFound
}

func (r *JSONBoardRepository) FindByProject(ctx context.Context, projectID string) (kanban.Board, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return kanban.Board{}, err
	}
	data, err := r.readLocked()
	if err != nil {
		return kanban.Board{}, err
	}
	for _, board := range data.Boards {
		if board.ProjectID == projectID {
			return board, nil
		}
	}
	return kanban.Board{}, kanban.ErrNotFound
}

func (r *JSONBoardRepository) readLocked() (boardFile, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		backupPath := r.path + ".bak"
		data, err = os.ReadFile(backupPath)
		if errors.Is(err, os.ErrNotExist) {
			return boardFile{Version: boardFileVersion, Boards: []kanban.Board{}}, nil
		}
		if err != nil {
			return boardFile{}, fmt.Errorf("read boards.json backup: %w", err)
		}
		// replaceFile temporarily parks the previous file as .bak on Windows.
		// Recover that valid snapshot if the process stopped in that short gap.
		if restoreErr := os.Rename(backupPath, r.path); restoreErr != nil {
			return boardFile{}, fmt.Errorf("restore boards.json backup: %w", restoreErr)
		}
	}
	if err != nil {
		return boardFile{}, fmt.Errorf("read boards.json: %w", err)
	}
	var value boardFile
	if err := json.Unmarshal(data, &value); err != nil {
		return boardFile{}, fmt.Errorf("decode boards.json: %w", err)
	}
	if value.Version != boardFileVersion {
		return boardFile{}, fmt.Errorf("unsupported boards.json version %d", value.Version)
	}
	if value.Boards == nil {
		value.Boards = []kanban.Board{}
	}
	for index := range value.Boards {
		normalizeBoardCollections(&value.Boards[index])
	}
	// Self-healing cleanup: older board files may still carry a legacy
	// "activity" array that the domain model no longer defines. Go's
	// encoding/json silently ignores it on unmarshal, but rewrite the file
	// once through the normal save path so it does not linger on disk.
	if bytes.Contains(data, []byte(`"activity"`)) {
		if err := r.writeLocked(value); err != nil {
			return boardFile{}, fmt.Errorf("drop legacy activity data: %w", err)
		}
	}
	return value, nil
}

// normalizeBoardCollections upgrades older or partially written JSON values to
// the domain's collection invariants. The HTTP API can therefore keep exposing
// arrays (never null) even when an earlier app version omitted an empty slice.
func normalizeBoardCollections(board *kanban.Board) {
	if board.Plans == nil {
		board.Plans = []kanban.Plan{}
	}
	if board.Tasks == nil {
		board.Tasks = []kanban.Task{}
	}
	if board.Backlog == nil {
		board.Backlog = []kanban.BacklogItem{}
	}
	for planIndex := range board.Plans {
		plan := &board.Plans[planIndex]
		if plan.Request.AcceptanceCriteria == nil {
			plan.Request.AcceptanceCriteria = []string{}
		}
		if plan.Request.Attachments == nil {
			plan.Request.Attachments = []kanban.RequestAttachment{}
		}
		if plan.Documents == nil {
			plan.Documents = []kanban.Document{}
		}
		if plan.Reviews == nil {
			plan.Reviews = []kanban.PlanReview{}
		}
		for documentIndex := range plan.Documents {
			if plan.Documents[documentIndex].Audience == nil {
				plan.Documents[documentIndex].Audience = []kanban.AgentRole{}
			}
		}
	}
	for taskIndex := range board.Tasks {
		task := &board.Tasks[taskIndex]
		if task.AcceptanceCriteria == nil {
			task.AcceptanceCriteria = []string{}
		}
		if task.DependencyIDs == nil {
			task.DependencyIDs = []string{}
		}
		if task.DocumentIDs == nil {
			task.DocumentIDs = []string{}
		}
	}
	for itemIndex := range board.Backlog {
		item := &board.Backlog[itemIndex]
		if item.AcceptanceCriteria == nil {
			item.AcceptanceCriteria = []string{}
		}
		if item.Attachments == nil {
			item.Attachments = []kanban.RequestAttachment{}
		}
		if strings.TrimSpace(item.RequestID) == "" {
			for _, plan := range board.Plans {
				if plan.ID == item.PlanID && strings.TrimSpace(plan.Request.ID) != "" {
					item.RequestID = plan.Request.ID
					break
				}
			}
			if strings.TrimSpace(item.RequestID) == "" {
				item.RequestID = "request-legacy-" + strings.TrimPrefix(item.ID, "backlog-")
			}
		}
	}
}

func (r *JSONBoardRepository) writeLocked(value boardFile) error {
	value.Version = boardFileVersion
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode boards.json: %w", err)
	}
	temporary, err := os.CreateTemp(r.directory, "boards-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary board file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary board file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary board file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("flush temporary board file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary board file: %w", err)
	}
	if err := replaceFile(temporaryPath, r.path); err != nil {
		return fmt.Errorf("replace boards.json: %w", err)
	}
	removeTemporary = false
	return nil
}

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if _, err := os.Stat(target); err != nil {
		return err
	}
	backup := target + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}
