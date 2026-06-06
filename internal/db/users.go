package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrDuplicateUsername = errors.New("username already exists")

type User struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Hash        string     `json:"-"`
	Salt        string     `json:"-"`
	Role        string     `json:"role"`
	TOTPSecret  *string    `json:"-"`
	TOTPPending *string    `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLogin   *time.Time `json:"last_login"`
}

type UserView struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	TOTPEnabled bool       `json:"totp_enabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLogin   *time.Time `json:"last_login"`
}

func (s *Store) CreateUser(ctx context.Context, u User) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO users (id, username, hash, salt, role, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, u.ID, u.Username, u.Hash, u.Salt, u.Role, formatTime(u.CreatedAt))
	if err != nil && isSQLiteConstraint(err) {
		return ErrDuplicateUsername
	}
	return err
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT id, username, hash, salt, role, totp_secret, totp_pending, created_at, last_login
		FROM users WHERE username = ?
	`, username)
	return scanUser(row)
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT id, username, hash, salt, role, totp_secret, totp_pending, created_at, last_login
		FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

func (s *Store) ListUsers(ctx context.Context) ([]UserView, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, username, role, totp_secret, created_at, last_login
		FROM users ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserView
	for rows.Next() {
		var u UserView
		var totpSecret sql.NullString
		var created string
		var lastLogin sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &totpSecret, &created, &lastLogin); err != nil {
			return nil, err
		}
		u.TOTPEnabled = totpSecret.Valid && totpSecret.String != ""
		createdAt, err := parseTime(created)
		if err != nil {
			return nil, err
		}
		u.CreatedAt = createdAt
		if lastLogin.Valid {
			t, err := parseTime(lastLogin.String)
			if err != nil {
				return nil, err
			}
			u.LastLogin = &t
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserLastLogin(ctx context.Context, id string, at time.Time) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE users SET last_login = ? WHERE id = ?`, formatTime(at), id)
	return err
}

func (s *Store) UpdateUserRole(ctx context.Context, id, role string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id, hash, salt string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE users SET hash = ?, salt = ? WHERE id = ?`, hash, salt, id)
	return err
}

func (s *Store) SetUserTOTPPending(ctx context.Context, id, secret string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE users SET totp_pending = ? WHERE id = ?`, secret, id)
	return err
}

func (s *Store) ConfirmUserTOTP(ctx context.Context, id string) error {
	_, err := s.conn.ExecContext(ctx, `
		UPDATE users SET totp_secret = totp_pending, totp_pending = NULL WHERE id = ?
	`, id)
	return err
}

func (s *Store) DisableUserTOTP(ctx context.Context, id string) error {
	_, err := s.conn.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_pending = NULL WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count)
	return count, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var totpSecret, totpPending sql.NullString
	var created string
	var lastLogin sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.Hash, &u.Salt, &u.Role, &totpSecret, &totpPending, &created, &lastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if totpSecret.Valid {
		u.TOTPSecret = &totpSecret.String
	}
	if totpPending.Valid {
		u.TOTPPending = &totpPending.String
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = createdAt
	if lastLogin.Valid {
		t, err := parseTime(lastLogin.String)
		if err != nil {
			return nil, err
		}
		u.LastLogin = &t
	}
	return &u, nil
}

func isSQLiteConstraint(err error) bool {
	if err == nil {
		return false
	}
	return containsSubstring(err.Error(), "UNIQUE constraint failed") ||
		containsSubstring(err.Error(), "constraint failed")
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexSubstring(s, sub) >= 0)
}

func indexSubstring(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
