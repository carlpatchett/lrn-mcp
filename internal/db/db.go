package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store encapsulates SQLite database access
type Store struct {
	db *sql.DB
}

// Open opens and initializes the SQLite database
func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed opening sqlite database at %s: %w", dbPath, err)
	}

	// Single writer connection pool tuning for SQLite
	db.SetMaxOpenConns(1)

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed applying migrations: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns raw sql.DB handle
func (s *Store) DB() *sql.DB {
	return s.db
}
