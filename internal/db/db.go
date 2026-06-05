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
	base := []string{
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
		`CREATE TABLE IF NOT EXISTS api_key_model_acls (
			key_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (key_id, model_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_model_acls_key ON api_key_model_acls(key_id);`,
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
		`CREATE TABLE IF NOT EXISTS prompt_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			system_prompt TEXT NOT NULL,
			prompt TEXT NOT NULL,
			model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_templates_updated ON prompt_templates(updated_at);`,
		`CREATE TABLE IF NOT EXISTS users (
			id          TEXT PRIMARY KEY,
			username    TEXT NOT NULL UNIQUE,
			hash        TEXT NOT NULL,
			salt        TEXT NOT NULL,
			role        TEXT NOT NULL DEFAULT 'operator',
			totp_secret TEXT,
			totp_pending TEXT,
			created_at  TEXT NOT NULL,
			last_login  TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id),
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);`,
		`CREATE TABLE IF NOT EXISTS _schema_migrations (
			id         INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range base {
		if _, err := s.conn.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return s.runVersionedMigrations(ctx)
}

func (s *Store) runVersionedMigrations(ctx context.Context) error {
	type migration struct {
		id  int
		sql string
	}
	migrations := []migration{
		{1, `ALTER TABLE api_keys ADD COLUMN owner_id TEXT`},
		{2, `ALTER TABLE api_keys ADD COLUMN key_role TEXT NOT NULL DEFAULT 'inference'`},
		{3, `ALTER TABLE api_keys ADD COLUMN rate_limit_rpm INTEGER NOT NULL DEFAULT 0`},
		{4, `ALTER TABLE api_keys ADD COLUMN max_prompt_tokens INTEGER NOT NULL DEFAULT 0`},
		{5, `ALTER TABLE api_keys ADD COLUMN allowed_ips TEXT`},
	}
	for _, m := range migrations {
		var count int
		if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM _schema_migrations WHERE id = ?`, m.id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if _, err := s.conn.ExecContext(ctx, m.sql); err != nil {
			return err
		}
		if _, err := s.conn.ExecContext(ctx, `INSERT INTO _schema_migrations (id, applied_at) VALUES (?, ?)`, m.id, formatTime(time.Now().UTC())); err != nil {
			return err
		}
	}
	return nil
}
