package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// openSQLiteDB opens (creating if needed) the single SQLite database file
// shared by every durable entity repository for the sqlite datasource. A
// missing or unwritable target directory fails explicitly rather than
// silently falling back to another location.
func openSQLiteDB(path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite database path is required")
	}
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create sqlite data directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// A single connection matches the single-writer semantics of the local
	// JSON files and avoids SQLITE_BUSY errors under WAL mode.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite WAL mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to sqlite database: %w", err)
	}
	return db, nil
}

// isSQLiteUniqueConstraintError reports whether err came from a UNIQUE
// constraint violation. The driver surfaces this as a plain error string
// rather than a typed sentinel, so adapters match on the SQLite message.
func isSQLiteUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
