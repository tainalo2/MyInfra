package api

import (
	"context"
	"encoding/json"
	"errors"

	"myinfra/internal/assistant"
	"myinfra/internal/store"
)

// conversationRepo keeps the assistant's conversations in PostgreSQL.
type conversationRepo struct{ s *store.Store }

func (c conversationRepo) List(ctx context.Context, user string, limit int) ([]assistant.Conversation, error) {
	rows, err := c.s.AssistantConversations(ctx, user, limit)
	if err != nil {
		return nil, err
	}
	out := make([]assistant.Conversation, 0, len(rows))
	for _, r := range rows {
		out = append(out, assistant.Conversation{ID: r.ID, Title: r.Title, UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}

func (c conversationRepo) Get(ctx context.Context, user, id string) (assistant.Conversation, error) {
	r, err := c.s.AssistantConversationOf(ctx, user, id)
	if errors.Is(err, store.ErrNotFound) {
		return assistant.Conversation{}, assistant.ErrNoConversation
	}
	if err != nil {
		return assistant.Conversation{}, err
	}
	return assistant.Conversation{ID: r.ID, Title: r.Title, Messages: r.Messages, UpdatedAt: r.UpdatedAt}, nil
}

func (c conversationRepo) Put(ctx context.Context, user, id, title string, messages json.RawMessage) error {
	err := c.s.PutAssistantConversation(ctx, user, id, title, messages)
	if errors.Is(err, store.ErrNotFound) {
		return assistant.ErrNoConversation
	}
	return err
}

func (c conversationRepo) Delete(ctx context.Context, user, id string) error {
	return c.s.DeleteAssistantConversation(ctx, user, id)
}
