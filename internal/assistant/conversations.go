package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Conversation is one conversation with the assistant, kept for the person
// who had it. Messages are kept as the interface shows them.
type Conversation struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Messages  json.RawMessage `json:"messages,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ErrNoConversation is a conversation that is not the person's, or none.
var ErrNoConversation = errors.New("conversation not found")

// ConversationRepo keeps the conversations, each to one person.
type ConversationRepo interface {
	List(ctx context.Context, user string, limit int) ([]Conversation, error)
	Get(ctx context.Context, user, id string) (Conversation, error)
	Put(ctx context.Context, user, id, title string, messages json.RawMessage) error
	Delete(ctx context.Context, user, id string) error
}

const (
	conversationsListed = 100
	conversationMax     = 4 << 20
	titleMax            = 80
)

func (s *Service) registerConversations(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/assistant/conversations", wrap(s.listConversations))
	mux.HandleFunc("GET /api/assistant/conversations/{id}", wrap(s.getConversation))
	mux.HandleFunc("PUT /api/assistant/conversations/{id}", wrap(s.putConversation))
	mux.HandleFunc("DELETE /api/assistant/conversations/{id}", wrap(s.deleteConversation))
}

// conversationUser is whose conversations a request may touch: only the
// signed-in person's, and nobody's without a session.
func (s *Service) conversationUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.Conversations == nil {
		writeJSON(w, 503, map[string]string{"error": "history unavailable"})
		return "", false
	}
	user := UserOf(r.Context())
	if user == "" {
		writeJSON(w, 403, map[string]string{"error": "Droits insuffisants."})
		return "", false
	}
	return user, true
}

func (s *Service) listConversations(w http.ResponseWriter, r *http.Request) {
	user, ok := s.conversationUser(w, r)
	if !ok {
		return
	}
	list, err := s.Conversations.List(r.Context(), user, conversationsListed)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "history unavailable"})
		return
	}
	writeJSON(w, 200, list)
}

func (s *Service) getConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.conversationUser(w, r)
	if !ok {
		return
	}
	c, err := s.Conversations.Get(r.Context(), user, r.PathValue("id"))
	if errors.Is(err, ErrNoConversation) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "history unavailable"})
		return
	}
	writeJSON(w, 200, c)
}

// putConversation keeps the conversation as the interface has it, after each
// turn and each decision. Its title is the first thing the person asked.
func (s *Service) putConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.conversationUser(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	raw, err := io.ReadAll(io.LimitReader(r.Body, conversationMax+1))
	if err != nil || len(raw) > conversationMax || len(id) == 0 || len(id) > 64 {
		writeJSON(w, 400, map[string]string{"error": "invalid conversation"})
		return
	}
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &body) != nil || json.Unmarshal(body.Messages, &msgs) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid conversation"})
		return
	}
	title := ""
	for _, m := range msgs {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			title = shortTitle(m.Content)
			break
		}
	}
	err = s.Conversations.Put(r.Context(), user, id, title, body.Messages)
	if errors.Is(err, ErrNoConversation) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "not saved"})
		return
	}
	w.WriteHeader(204)
}

func (s *Service) deleteConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.conversationUser(w, r)
	if !ok {
		return
	}
	if err := s.Conversations.Delete(r.Context(), user, r.PathValue("id")); err != nil {
		writeJSON(w, 500, map[string]string{"error": "not deleted"})
		return
	}
	w.WriteHeader(204)
}

// shortTitle keeps the first line, cut on a rune boundary.
func shortTitle(s string) string {
	s = strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if utf8.RuneCountInString(s) <= titleMax {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:titleMax-1])) + "…"
}
