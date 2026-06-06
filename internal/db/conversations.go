package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Conversation struct {
	ID        string        `json:"id"`
	OwnerID   string        `json:"-"`
	Title     string        `json:"title"`
	Model     string        `json:"model"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []ChatMessage `json:"messages,omitempty"`
}

type ChatMessage struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversation_id,omitempty"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	ModelUsed       string    `json:"model_used,omitempty"`
	TokensPerSecond float64   `json:"tokens_per_second,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Store) CreateConversation(ctx context.Context, item Conversation) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO chat_conversations (id, owner_id, title, model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, item.ID, item.OwnerID, item.Title, item.Model, formatTime(item.CreatedAt), formatTime(item.UpdatedAt))
	return err
}

func (s *Store) ListConversations(ctx context.Context, ownerID string, limit, offset int) ([]Conversation, int, error) {
	var total int
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_conversations WHERE owner_id = ?`, ownerID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, title, model, created_at, updated_at
		FROM chat_conversations WHERE owner_id = ?
		ORDER BY updated_at DESC LIMIT ? OFFSET ?
	`, ownerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]Conversation, 0)
	for rows.Next() {
		var item Conversation
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Title, &item.Model, &created, &updated); err != nil {
			return nil, 0, err
		}
		item.OwnerID = ownerID
		if item.CreatedAt, err = parseTime(created); err != nil {
			return nil, 0, err
		}
		if item.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *Store) ConversationByID(ctx context.Context, id, ownerID string) (Conversation, bool, error) {
	var item Conversation
	var created, updated string
	err := s.conn.QueryRowContext(ctx, `
		SELECT id, owner_id, title, model, created_at, updated_at
		FROM chat_conversations WHERE id = ? AND owner_id = ?
	`, id, ownerID).Scan(&item.ID, &item.OwnerID, &item.Title, &item.Model, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, false, nil
	}
	if err != nil {
		return Conversation{}, false, err
	}
	if item.CreatedAt, err = parseTime(created); err != nil {
		return Conversation{}, false, err
	}
	if item.UpdatedAt, err = parseTime(updated); err != nil {
		return Conversation{}, false, err
	}
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, model_used, tokens_per_second, created_at
		FROM chat_messages WHERE conversation_id = ? ORDER BY created_at, id
	`, id)
	if err != nil {
		return Conversation{}, false, err
	}
	defer rows.Close()
	item.Messages = make([]ChatMessage, 0)
	for rows.Next() {
		var message ChatMessage
		var at string
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &message.ModelUsed, &message.TokensPerSecond, &at); err != nil {
			return Conversation{}, false, err
		}
		if message.CreatedAt, err = parseTime(at); err != nil {
			return Conversation{}, false, err
		}
		item.Messages = append(item.Messages, message)
	}
	return item, true, rows.Err()
}

func (s *Store) AppendConversationMessages(ctx context.Context, id, ownerID string, messages []ChatMessage, updated time.Time) (bool, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_conversations WHERE id = ? AND owner_id = ?`, id, ownerID).Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	for _, message := range messages {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_messages (id, conversation_id, role, content, model_used, tokens_per_second, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, message.ID, id, message.Role, message.Content, message.ModelUsed, message.TokensPerSecond, formatTime(message.CreatedAt)); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE chat_conversations SET updated_at = ? WHERE id = ?`, formatTime(updated), id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) PatchConversation(ctx context.Context, id, ownerID string, title, model *string, updated time.Time) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `
		UPDATE chat_conversations
		SET title = COALESCE(?, title), model = COALESCE(?, model), updated_at = ?
		WHERE id = ? AND owner_id = ?
	`, title, model, formatTime(updated), id, ownerID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) DeleteConversation(ctx context.Context, id, ownerID string) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `DELETE FROM chat_conversations WHERE id = ? AND owner_id = ?`, id, ownerID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
