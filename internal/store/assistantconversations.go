package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AssistantConversation is one conversation with the assistant built into
// the interface. Messages are kept as the interface sent them.
type AssistantConversation struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Messages  json.RawMessage `json:"messages,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// AssistantConversations lists a person's conversations, the latest first,
// without their messages.
func (s *Store) AssistantConversations(ctx context.Context, userID string, limit int) ([]AssistantConversation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, updated_at FROM assistant_conversations
		WHERE user_id = $1 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssistantConversation{}
	for rows.Next() {
		var c AssistantConversation
		if err := rows.Scan(&c.ID, &c.Title, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AssistantConversationOf returns one of the person's conversations, with
// its messages. Another person's is not found.
func (s *Store) AssistantConversationOf(ctx context.Context, userID, id string) (AssistantConversation, error) {
	var c AssistantConversation
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, messages, updated_at FROM assistant_conversations
		WHERE user_id = $1 AND id = $2`, userID, id).Scan(&c.ID, &c.Title, &c.Messages, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// PutAssistantConversation creates or replaces one of the person's
// conversations. An id that belongs to someone else is not touched.
func (s *Store) PutAssistantConversation(ctx context.Context, userID, id, title string, messages json.RawMessage) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO assistant_conversations (id, user_id, title, messages) VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, messages = EXCLUDED.messages, updated_at = now()
		WHERE assistant_conversations.user_id = EXCLUDED.user_id`,
		id, userID, title, messages)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAssistantConversation forgets one of the person's conversations.
// A missing one is no error.
func (s *Store) DeleteAssistantConversation(ctx context.Context, userID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM assistant_conversations WHERE user_id = $1 AND id = $2`, userID, id)
	return err
}
