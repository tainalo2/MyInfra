package assistant

import (
	"encoding/json"
	"fmt"
	"net/http"

	harness "myinfra/internal/harness-router"
)

// Service wires keys, pricing, providers, router and meter behind HTTP.
type Service struct {
	Keys      *KeyStore
	Pricing   *Pricing
	Meter     *Meter
	Router    *harness.Router
	Providers map[string]Provider
}

// Register mounts routes on mux under /api/assistant. Wrap auth outside if needed.
func (s *Service) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	if wrap == nil {
		wrap = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	mux.HandleFunc("GET /api/assistant/status", wrap(s.status))
	mux.HandleFunc("GET /api/assistant/pricing", wrap(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Pricing.List()) }))
	mux.HandleFunc("PUT /api/assistant/pricing", wrap(s.setPrice))
	mux.HandleFunc("PUT /api/assistant/keys/{provider}", wrap(s.setKey))
	mux.HandleFunc("DELETE /api/assistant/keys/{provider}", wrap(func(w http.ResponseWriter, r *http.Request) {
		s.Keys.Delete(r.PathValue("provider"))
		w.WriteHeader(204)
	}))
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

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
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
	var b struct {
		Key string `json:"key"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil || s.Keys.Set(r.PathValue("provider"), b.Key) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid key"})
		return
	}
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
	prov, ok := s.Providers[q.Provider]
	key, _ := s.Keys.Get(q.Provider)
	if !ok || key == "" {
		writeJSON(w, 200, map[string]any{"assistant_unavailable": true})
		return
	}
	route, err := s.Router.Route(harness.RouteRequest{FunctionalContext: q.Context, ProviderKey: key})
	if err != nil {
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
