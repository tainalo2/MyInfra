package assistant

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"myinfra/internal/assistant/siwc"
)

// Names inside the KeyStore that are not providers. They hold sealed JSON.
const (
	subscriptionEntry = "codex.subscription" // siwc.Credential
	hostEntry         = "codex.host"         // stable ext_agent_host_id of this instance
)

type subscription struct {
	mu     sync.Mutex
	client *siwc.Client
}

func isInternal(name string) bool { return strings.Contains(name, ".") }

// providerNames lists providers with a usable credential: an API key or, for
// codex, a ChatGPT subscription. Internal entries are never listed.
func (s *Service) providerNames() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range s.Keys.List() {
		if n == subscriptionEntry {
			n = "codex"
		}
		if !isInternal(n) && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func (s *Service) persistEntry(ctx context.Context, name, value string) error {
	if err := s.Keys.Set(name, value); err != nil {
		return err
	}
	if s.repo != nil {
		return s.repo.PutAssistantKey(ctx, name, s.Keys.Export()[name])
	}
	return nil
}

// hostID returns this instance's stable ext_agent_host_id, created once.
func (s *Service) hostID(ctx context.Context) (string, error) {
	if v, ok := s.Keys.Get(hostEntry); ok {
		return v, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	id := fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return id, s.persistEntry(ctx, hostEntry, id)
}

// EnableSubscription turns on "Continue with ChatGPT". redirectURI must be the
// loopback callback OpenAI requires, http://127.0.0.1:<port>/auth/callback.
func (s *Service) EnableSubscription(ctx context.Context, appName, redirectURI string) error {
	if !s.Available() {
		return errors.New(noMasterKey)
	}
	host, err := s.hostID(ctx)
	if err != nil {
		return err
	}
	c, err := siwc.New(siwc.Config{AppName: appName, HostID: host, RedirectURI: redirectURI})
	if err != nil {
		return err
	}
	s.siwc.mu.Lock()
	s.siwc.client = c
	s.siwc.mu.Unlock()
	return nil
}

func (s *Service) siwcClient() *siwc.Client {
	s.siwc.mu.Lock()
	defer s.siwc.mu.Unlock()
	return s.siwc.client
}

func (s *Service) loadSubscription() (siwc.Credential, bool) {
	var c siwc.Credential
	raw, ok := s.Keys.Get(subscriptionEntry)
	if !ok || json.Unmarshal([]byte(raw), &c) != nil {
		return c, false
	}
	return c, true
}

func (s *Service) saveSubscription(ctx context.Context, c siwc.Credential) error {
	b, _ := json.Marshal(c)
	return s.persistEntry(ctx, subscriptionEntry, string(b))
}

// credentialFor returns the secret to hand to the provider adapter: the API key
// when there is one, else (codex only) the subscription access token, refreshed
// first when it is about to expire.
func (s *Service) credentialFor(ctx context.Context, provider string) (string, error) {
	if k, ok := s.Keys.Get(provider); ok && k != "" {
		return k, nil
	}
	if provider != "codex" {
		return "", nil
	}
	cred, ok := s.loadSubscription()
	if !ok {
		return "", nil
	}
	c := s.siwcClient()
	if c == nil {
		return cred.AccessToken, nil
	}
	n, changed, err := c.Refresh(ctx, cred)
	if err != nil {
		slog.Warn("renouvellement du jeton ChatGPT échoué", "err", err)
		return "", fmt.Errorf("ChatGPT sign-in expired, reconnect from settings (%w)", err)
	}
	if changed {
		// The rotated refresh token replaces the old one: lose this write and
		// the next refresh fails, so a failure is surfaced, not ignored.
		if err := s.saveSubscription(ctx, n); err != nil {
			return "", fmt.Errorf("renewed ChatGPT token not saved: %w", err)
		}
	}
	return n.AccessToken, nil
}

func (s *Service) registerSubscription(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/assistant/openai/start", wrap(s.siwcStart))
	mux.HandleFunc("GET /auth/callback", wrap(s.siwcCallback))
	mux.HandleFunc("DELETE /api/assistant/openai", wrap(func(w http.ResponseWriter, r *http.Request) {
		if !s.Available() {
			writeJSON(w, 503, map[string]string{"error": noMasterKey})
			return
		}
		s.Keys.Delete(subscriptionEntry)
		if s.repo != nil {
			if err := s.repo.DeleteAssistantKey(r.Context(), subscriptionEntry); err != nil {
				writeJSON(w, 500, map[string]string{"error": "not removed"})
				return
			}
		}
		w.WriteHeader(204)
	}))
}

func (s *Service) siwcStart(w http.ResponseWriter, r *http.Request) {
	c := s.siwcClient()
	if c == nil {
		writeJSON(w, 503, map[string]string{"error": "Sign in with ChatGPT is not enabled on this instance"})
		return
	}
	if role := RoleOf(r.Context()); !CanChat(role) {
		writeJSON(w, 403, map[string]string{"error": "Droits insuffisants."})
		return
	}
	var known *siwc.Credential
	if cur, ok := s.loadSubscription(); ok {
		known = &cur // reauthorize the existing registration instead of creating another
	}
	http.Redirect(w, r, c.Start(known), http.StatusFound)
}

func (s *Service) siwcCallback(w http.ResponseWriter, r *http.Request) {
	c := s.siwcClient()
	if c == nil {
		writeJSON(w, 503, map[string]string{"error": "Sign in with ChatGPT is not enabled on this instance"})
		return
	}
	cred, err := c.Complete(r.Context(), r.URL.Query())
	if err != nil {
		var se *siwc.Error
		step := "unknown"
		if errors.As(err, &se) {
			step = se.Step
		}
		slog.Warn("Sign in with ChatGPT refusé", "step", step, "err", err)
		// Nothing was saved: a failed attempt leaves no partial registration.
		writeJSON(w, 400, map[string]string{"error": err.Error(), "step": step})
		return
	}
	cred.User = UserOf(r.Context())
	if err := s.saveSubscription(r.Context(), *cred); err != nil {
		writeJSON(w, 500, map[string]string{"error": "credential not saved"})
		return
	}
	http.Redirect(w, r, "/parametres?chatgpt=ok", http.StatusFound)
}
