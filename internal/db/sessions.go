package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const sessionTokenBytes = 32

type Session struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

func GenerateSessionToken() (string, error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, sess.Token, sess.UserID, formatTime(sess.ExpiresAt), formatTime(sess.CreatedAt))
	return err
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT token, user_id, expires_at, created_at
		FROM sessions
		WHERE token = ? AND expires_at > ?
	`, token, formatTime(time.Now().UTC()))
	var sess Session
	var expiresAt, createdAt string
	if err := row.Scan(&sess.Token, &sess.UserID, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t1, err := parseTime(expiresAt)
	if err != nil {
		return nil, err
	}
	t2, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	sess.ExpiresAt = t1
	sess.CreatedAt = t2
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) PruneSessions(ctx context.Context) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatTime(time.Now().UTC()))
	return err
}
