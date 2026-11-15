package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"myinfra/internal/assistant/siwc"
)

func subSvc(t *testing.T) *Service {
	s, err := New(bytes.Repeat([]byte{3}, 32), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnableSubscriptionStableHostAndLoopbackOnly(t *testing.T) {
	s := subSvc(t)
	ctx := context.Background()
	if err := s.EnableSubscription(ctx, "MyInfra", "https://x.example/auth/callback"); err == nil {
		t.Fatal("non-loopback accepted")
	}
	if err := s.EnableSubscription(ctx, "MyInfra", "http://127.0.0.1:8080/auth/callback"); err != nil {
		t.Fatal(err)
	}
	a, _ := s.hostID(ctx)
	b, _ := s.hostID(ctx)
	if a == "" || a != b || !strings.HasPrefix(a, "urn:uuid:") {
		t.Fatalf("host id not stable: %q %q", a, b)
	}
}

func TestInternalEntriesNeverListedAndSubscriptionEnablesCodex(t *testing.T) {
	s := subSvc(t)
	ctx := context.Background()
	s.hostID(ctx)
	if got := s.providerNames(); len(got) != 0 {
		t.Fatalf("internal entry leaked: %v", got)
	}
	s.saveSubscription(ctx, siwc.Credential{ClientID: "oaiapp_x", AccessToken: "at", ExpiresAt: time.Now().Add(time.Hour)})
	if got := s.providerNames(); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("%v", got)
	}
	k, err := s.credentialFor(ctx, "codex")
	if err != nil || k != "at" {
		t.Fatalf("%q %v", k, err)
	}
	if k, _ := s.credentialFor(ctx, "claude"); k != "" {
		t.Fatal("subscription leaked to another provider")
	}
	s.Keys.Set("codex", "sk-api")
	if k, _ := s.credentialFor(ctx, "codex"); k != "sk-api" {
		t.Fatal("API key must win")
	}
}

// OpenAI rotates the refresh token on use and refuses a spent one. Ten turns
// arriving together with an expired token must refresh once, all succeed, and
// leave the rotated token saved.
func TestConcurrentTurnsRefreshOnce(t *testing.T) {
	var mu sync.Mutex
	valid, refreshes := "rt-0", 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		// A real round trip: without it the turns barely overlap.
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if r.Form.Get("refresh_token") != valid {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		refreshes++
		valid = fmt.Sprintf("rt-%d", refreshes)
		json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("at-%d", refreshes), "refresh_token": valid, "expires_in": 3600, "scope": siwc.ScopeUse})
	}))
	defer srv.Close()

	s := subSvc(t)
	ctx := context.Background()
	c, err := siwc.New(siwc.Config{Issuer: srv.URL, AppName: "MyInfra", HostID: "urn:uuid:h", RedirectURI: "http://127.0.0.1:8080/auth/callback"})
	if err != nil {
		t.Fatal(err)
	}
	s.siwc.client = c
	s.saveSubscription(ctx, siwc.Credential{ClientID: "oaiapp_x", AccessToken: "at-old", RefreshToken: "rt-0", ExpiresAt: time.Now().Add(-time.Minute)})

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, err := s.credentialFor(ctx, "codex")
			if err == nil && k != "at-1" {
				err = fmt.Errorf("token %q", k)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if refreshes != 1 {
		t.Fatalf("%d refreshes, want 1", refreshes)
	}
	if cred, _ := s.loadSubscription(); cred.RefreshToken != "rt-1" {
		t.Fatalf("saved refresh token %q", cred.RefreshToken)
	}
}
