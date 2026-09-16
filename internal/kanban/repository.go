package kanban

import (
	"context"
	"errors"
)

var (
	ErrNotFound = errors.New("board not found")
	ErrConflict = errors.New("board already exists")
)

// BoardRepository is the persistence boundary. JSON, SQL, or another database
// adapter must preserve these CRUD semantics.
type BoardRepository interface {
	Create(context.Context, Board) error
	Get(context.Context, string) (Board, error)
	List(context.Context) ([]Board, error)
	Update(context.Context, Board) error
	Delete(context.Context, string) error
	FindByProject(context.Context, string) (Board, error)
}
