package assistant

import (
	"bytes"
	"context"
	"strings"
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
