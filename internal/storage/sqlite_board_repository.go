package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/theanh2906/AI-Product-Team/internal/kanban"
)

// SQLiteBoardRepository is the sqlite adapter for kanban.BoardRepository. It
// stores each board as a JSON payload keyed by id so the domain model does not
// need a relational schema of its own.
type SQLiteBoardRepository struct {
	db *sql.DB
}

func NewSQLiteBoardRepository(db *sql.DB) (*SQLiteBoardRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}
	const schema = `CREATE TABLE IF NOT EXISTS boards (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL UNIQUE,
		payload TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create boards table: %w", err)
	}
	return &SQLiteBoardRepository{db: db}, nil
}

func (r *SQLiteBoardRepository) Create(ctx context.Context, board kanban.Board) error {
	payload, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("encode board: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO boards (id, project_id, payload) VALUES (?, ?, ?)`, board.ID, board.ProjectID, payload)
	if err != nil {
		if isSQLiteUniqueConstraintError(err) {
			return kanban.ErrConflict
		}
		return fmt.Errorf("insert board: %w", err)
	}
	return nil
}

func (r *SQLiteBoardRepository) Get(ctx context.Context, id string) (kanban.Board, error) {
	return r.queryOne(ctx, `SELECT payload FROM boards WHERE id = ?`, id)
}

func (r *SQLiteBoardRepository) FindByProject(ctx context.Context, projectID string) (kanban.Board, error) {
	return r.queryOne(ctx, `SELECT payload FROM boards WHERE project_id = ?`, projectID)
}

func (r *SQLiteBoardRepository) queryOne(ctx context.Context, query string, arg string) (kanban.Board, error) {
	var payload string
	err := r.db.QueryRowContext(ctx, query, arg).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Board{}, kanban.ErrNotFound
	}
	if err != nil {
		return kanban.Board{}, fmt.Errorf("read board: %w", err)
	}
	return decodeBoard(payload)
}

func (r *SQLiteBoardRepository) List(ctx context.Context) ([]kanban.Board, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM boards`)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer rows.Close()
	boards := make([]kanban.Board, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan board: %w", err)
		}
		board, err := decodeBoard(payload)
		if err != nil {
			return nil, err
		}
		boards = append(boards, board)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].UpdatedAt.After(boards[j].UpdatedAt) })
	return boards, nil
}

func (r *SQLiteBoardRepository) Update(ctx context.Context, board kanban.Board) error {
	payload, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("encode board: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE boards SET project_id = ?, payload = ? WHERE id = ?`, board.ProjectID, payload, board.ID)
	if err != nil {
		return fmt.Errorf("update board: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update board: %w", err)
	}
	if affected == 0 {
		return kanban.ErrNotFound
	}
	return nil
}

func (r *SQLiteBoardRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete board: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete board: %w", err)
	}
	if affected == 0 {
		return kanban.ErrNotFound
	}
	return nil
}

func decodeBoard(payload string) (kanban.Board, error) {
	var board kanban.Board
	if err := json.Unmarshal([]byte(payload), &board); err != nil {
		return kanban.Board{}, fmt.Errorf("decode board: %w", err)
	}
	normalizeBoardCollections(&board)
	return board, nil
}
