package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Comparison struct {
	ID          string     `json:"id"`
	OwnerID     string     `json:"-"`
	Prompt      string     `json:"prompt"`
	ModelA      string     `json:"model_a"`
	ModelB      string     `json:"model_b"`
	ResponseA   string     `json:"response_a"`
	ResponseB   string     `json:"response_b"`
	Winner      *string    `json:"winner"`
	IsBlind     bool       `json:"is_blind"`
	BlindMap    string     `json:"blind_map,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

func (s *Store) CreateComparison(ctx context.Context, item Comparison) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO comparisons (id, owner_id, prompt, model_a, model_b, response_a, response_b, winner, is_blind, blind_map, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.OwnerID, item.Prompt, item.ModelA, item.ModelB, item.ResponseA, item.ResponseB, item.Winner, boolInt(item.IsBlind), item.BlindMap, formatTime(item.CreatedAt), nullableTime(item.CompletedAt))
	return err
}

func (s *Store) CompleteComparison(ctx context.Context, id, ownerID, responseA, responseB string, at time.Time) error {
	_, err := s.conn.ExecContext(ctx, `
		UPDATE comparisons SET response_a = ?, response_b = ?, completed_at = ? WHERE id = ? AND owner_id = ?
	`, responseA, responseB, formatTime(at), id, ownerID)
	return err
}

func (s *Store) ComparisonByID(ctx context.Context, id, ownerID string) (Comparison, bool, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT id, owner_id, prompt, model_a, model_b, response_a, response_b, winner, is_blind, blind_map, created_at, completed_at
		FROM comparisons WHERE id = ? AND owner_id = ?
	`, id, ownerID)
	item, err := scanComparison(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Comparison{}, false, nil
	}
	return item, err == nil, err
}

func (s *Store) ListComparisons(ctx context.Context, ownerID string, limit, offset int) ([]Comparison, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, owner_id, prompt, model_a, model_b, response_a, response_b, winner, is_blind, blind_map, created_at, completed_at
		FROM comparisons WHERE owner_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?
	`, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Comparison, 0)
	for rows.Next() {
		item, err := scanComparison(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) VoteComparison(ctx context.Context, id, ownerID, winner string) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `UPDATE comparisons SET winner = ? WHERE id = ? AND owner_id = ?`, winner, id, ownerID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

type comparisonScanner interface {
	Scan(...interface{}) error
}

func scanComparison(scanner comparisonScanner) (Comparison, error) {
	var item Comparison
	var winner, completed sql.NullString
	var blind int
	var created string
	if err := scanner.Scan(&item.ID, &item.OwnerID, &item.Prompt, &item.ModelA, &item.ModelB, &item.ResponseA, &item.ResponseB, &winner, &blind, &item.BlindMap, &created, &completed); err != nil {
		return item, err
	}
	item.IsBlind = blind == 1
	if winner.Valid {
		item.Winner = &winner.String
	}
	var err error
	if item.CreatedAt, err = parseTime(created); err != nil {
		return item, err
	}
	if completed.Valid {
		at, err := parseTime(completed.String)
		if err != nil {
			return item, err
		}
		item.CompletedAt = &at
	}
	return item, nil
}

func nullableTime(value *time.Time) interface{} {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}
