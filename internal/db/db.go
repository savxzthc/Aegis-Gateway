package db

import (
	"context"
	"database/sql"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite connection used by Aegis Gateway.
type Store struct {
	conn              *sql.DB
	pruneMu           sync.Mutex
	lastMetadataPrune time.Time
	activeKeysMu      sync.RWMutex
	activeKeys        []APIKeySecret
	activeKeysExpires time.Time
}

// Open initializes a SQLite store and applies migrations.
func Open(path string) (*Store, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(time.Hour)

	store := &Store{conn: conn}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Migrate(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return store, nil
}

// Close closes the SQLite connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

// Migrate applies the database schema.
func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			label TEXT NOT NULL,
			salt TEXT NOT NULL,
			hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_used TEXT,
			revoked_at TEXT,
			requests_total INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(revoked_at);`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			key_id TEXT NOT NULL,
			model_requested TEXT NOT NULL,
			model_used TEXT NOT NULL,
			fallback_triggered INTEGER NOT NULL,
			backend_type TEXT NOT NULL,
			latency_ms INTEGER NOT NULL,
			estimated_prompt_tokens INTEGER NOT NULL,
			estimated_completion_tokens INTEGER NOT NULL,
			status_code INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_timestamp ON request_logs(timestamp);`,
		`CREATE TABLE IF NOT EXISTS auth_failures (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ip TEXT NOT NULL,
			timestamp TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_auth_failures_timestamp ON auth_failures(timestamp);`,
	}
	for _, stmt := range statements {
		if _, err := s.conn.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
