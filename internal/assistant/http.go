package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	harness "myinfra/internal/harness-router"
)

// Service wires keys, pricing, providers, router and meter behind HTTP.
//
// Keys is nil when no BYOK master key is configured: the routes answer
// assistant_unavailable and MyInfra serves everything else as usual.
type Service struct {
	Keys      *KeyStore
	Pricing   *Pricing
	Meter     *Meter
	Router    *harness.Router
	Providers map[string]Provider

	// repo keeps the sealed keys at rest; nil keeps them in memory only.
	repo KeyRepo
	// rekeyed lists the providers New sealed again with the current master:
	// Persist writes them back, or the rotation would be done at each start
	// and the previous master could never be retired.
	rekeyed []string
}

// KeyRepo keeps the sealed provider keys, in the exact form Export gives.
type KeyRepo interface {
	PutAssistantKey(ctx context.Context, provider, sealed string) error
	DeleteAssistantKey(ctx context.Context, provider string) error
}

// New builds the service around the BYOK master key. With a nil master the
// assistant is off: the service still answers, as unavailable. With a
// previous master, the sealed entries the current master does not open are
// sealed again with it.
func New(master, previous []byte, sealed map[string]string) (*Service, error) {
	p := DefaultPricing()
	s := &Service{
		Pricing:   p,
		Meter:     NewMeter(p),
		Router:    harness.NewRouter(harness.DefaultConfig()),
		Providers: map[string]Provider{"claude": Claude{}, "codex": OpenAI{}},
	}
	if master == nil {
		return s, nil
	}
	keys, err := NewKeyStore(master)
	if err != nil {
		return nil, err
	}
	keys.Import(sealed)
	if previous != nil {
		rekeyed, lost, err := keys.Rekey(previous)
		if err != nil {
			return nil, err
		}
		s.rekeyed = rekeyed
		if len(rekeyed) > 0 {
			slog.Info("clés BYOK rechiffrées avec la nouvelle clé maîtresse", "providers", rekeyed)
		}
		if len(lost) > 0 {
			slog.Warn("clés BYOK illisibles avec l'ancienne et la nouvelle clé maîtresse, à saisir de nouveau", "providers", lost)
		}
	}
	s.Keys = keys
	return s, nil
}

// Available tells whether provider keys can be kept: a BYOK master key is
// configured.
func (s *Service) Available() bool { return s != nil && s.Keys != nil }

// SupportsSubscriptionAuth checks whether a provider accepts subscription tokens.
func (s *Service) SupportsSubscriptionAuth(provider string) bool {
	prov, ok := s.Providers[provider]
	return ok && prov.SupportsSubscriptionAuth()
}

// Persist keeps the keys in repo from now on, and writes back the entries
// New sealed again with the current master.
func (s *Service) Persist(ctx context.Context, repo KeyRepo) error {
	if !s.Available() || repo == nil {
		return nil
	}
	s.repo = repo
	sealed := s.Keys.Export()
	for _, provider := range s.rekeyed {
		if err := repo.PutAssistantKey(ctx, provider, sealed[provider]); err != nil {
			return fmt.Errorf("clé BYOK %s rechiffrée non enregistrée : %w", provider, err)
		}
	}
	s.rekeyed = nil
	return nil
}

// Register mounts routes on mux under /api/assistant. Wrap auth outside if needed.
func (s *Service) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	if wrap == nil {
		wrap = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	mux.HandleFunc("GET /api/assistant/status", wrap(s.status))
	mux.HandleFunc("GET /api/assistant/pricing", wrap(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Pricing.List()) }))
	mux.HandleFunc("GET /api/assistant/providers", wrap(s.listProviders))
	mux.HandleFunc("PUT /api/assistant/pricing", wrap(s.setPrice))
	mux.HandleFunc("PUT /api/assistant/keys/{provider}", wrap(s.setKey))
	mux.HandleFunc("DELETE /api/assistant/keys/{provider}", wrap(s.deleteKey))
	mux.HandleFunc("GET /api/assistant/usage", wrap(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.Meter.Get(r.URL.Query().Get("session")))
	}))
	mux.HandleFunc("GET /api/assistant/usage/stream", wrap(s.stream))
	mux.HandleFunc("POST /api/assistant/chat", wrap(s.chat))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

const noMasterKey = "assistant unavailable: no BYOK master key configured"

func (s *Service) listProviders(w http.ResponseWriter, r *http.Request) {
	type ProviderCapability struct {
		Provider              string `json:"provider"`
		SupportsSubscriptionAuth bool   `json:"supports_subscription_auth"`
	}
	caps := make([]ProviderCapability, 0, len(s.Providers))
	for name, prov := range s.Providers {
		caps = append(caps, ProviderCapability{
			Provider:              name,
			SupportsSubscriptionAuth: prov.SupportsSubscriptionAuth(),
		})
	}
	writeJSON(w, 200, caps)
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	if !s.Available() {
		writeJSON(w, 200, map[string]any{"enabled": false, "providers": []string{}, "assistant_unavailable": true, "reason": "no_master_key"})
		return
	}
	ps := s.Keys.List()
	writeJSON(w, 200, map[string]any{"enabled": len(ps) > 0, "providers": ps})
}

func (s *Service) setPrice(w http.ResponseWriter, r *http.Request) {
	var p Price
	if json.NewDecoder(r.Body).Decode(&p) != nil || p.Provider == "" || p.Model == "" || p.InPerM < 0 || p.OutPerM < 0 {
		writeJSON(w, 400, map[string]string{"error": "invalid price"})
		return
	}
	s.Pricing.Set(p)
	w.WriteHeader(204)
}

func (s *Service) setKey(w http.ResponseWriter, r *http.Request) {
	if !s.Available() {
		writeJSON(w, 503, map[string]string{"error": noMasterKey})
		return
	}
	var b struct {
		Key  string `json:"key"`
		Type string `json:"type"` // "api_key" or "subscription_token"
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Key == "" {
		writeJSON(w, 400, map[string]string{"error": "invalid key"})
		return
	}
	provider := r.PathValue("provider")
	
	// Validate credential type against provider capability
	if b.Type == "subscription_token" && !s.SupportsSubscriptionAuth(provider) {
		writeJSON(w, 403, map[string]string{"error": "subscription_token not supported for this provider"})
		return
	}
	
	// Default to api_key if not specified (backward compatibility)
	if b.Type == "" {
		b.Type = "api_key"
	}
	
	if s.Keys.Set(provider, b.Key) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid key"})
		return
	}
	if s.repo != nil {
		if err := s.repo.PutAssistantKey(r.Context(), provider, s.Keys.Export()[provider]); err != nil {
			slog.Error("clé BYOK non enregistrée", "provider", provider, "err", err)
			writeJSON(w, 500, map[string]string{"error": "key not saved"})
			return
		}
	}
	w.WriteHeader(204)
}

func (s *Service) deleteKey(w http.ResponseWriter, r *http.Request) {
	if !s.Available() {
		writeJSON(w, 503, map[string]string{"error": noMasterKey})
		return
	}
	provider := r.PathValue("provider")
	if s.repo != nil {
		if err := s.repo.DeleteAssistantKey(r.Context(), provider); err != nil {
			slog.Error("clé BYOK non supprimée", "provider", provider, "err", err)
			writeJSON(w, 500, map[string]string{"error": "key not deleted"})
			return
		}
	}
	s.Keys.Delete(provider)
	w.WriteHeader(204)
}

func (s *Service) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	id := r.URL.Query().Get("session")
	ch, cancel := s.Meter.Subscribe(id)
	defer cancel()
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	send := func(v Snapshot) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); fl.Flush() }
	send(s.Meter.Get(id))
	for {
		select {
		case <-r.Context().Done():
			return
		case v := <-ch:
			send(v)
		}
	}
}

type chatReq struct {
	Session  string    `json:"session"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Context  string    `json:"context"`
	Messages []Message `json:"messages"`
}

func (s *Service) chat(w http.ResponseWriter, r *http.Request) {
	var q chatReq
	if json.NewDecoder(r.Body).Decode(&q) != nil || q.Session == "" {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	// Authorization is checked here too, before the provider lookup: a
	// read-only role never opens a turn, whatever the UI showed.
	if role := RoleOf(r.Context()); !CanChat(role) {
		writeJSON(w, 403, map[string]string{"error": "Droits insuffisants."})
		return
	}
	if !s.Available() {
		writeJSON(w, 200, map[string]any{"assistant_unavailable": true, "reason": "no_master_key"})
		return
	}
	prov, ok := s.Providers[q.Provider]
	key, _ := s.Keys.Get(q.Provider)
	if !ok || key == "" {
		writeJSON(w, 200, map[string]any{"assistant_unavailable": true})
		return
	}
	route, err := s.Router.Route(harness.RouteRequest{Role: RoleOf(r.Context()), FunctionalContext: q.Context, ProviderKey: key})
	if err != nil {
		if errors.Is(err, harness.ErrReadOnly) {
			writeJSON(w, 403, map[string]string{"error": "Droits insuffisants."})
			return
		}
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	resp, err := prov.Complete(r.Context(), q.Model, route.SystemPrompt, q.Messages, key)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	snap := s.Meter.Record(q.Session, q.Provider, q.Model, resp.Usage)
	writeJSON(w, 200, map[string]any{"text": resp.Text, "usage": snap})
}
