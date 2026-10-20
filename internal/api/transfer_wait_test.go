package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A copy behind the default certificate of Traefik answers, but not over a
// trusted connection: the transfer says it waits for the certificate.
func TestWaitHealthTellsAMissingCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	var mu sync.Mutex
	var states []string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitHealth(ctx, srv.URL, func(state string) {
		mu.Lock()
		states = append(states, state)
		mu.Unlock()
	})
	if err == nil || !strings.Contains(err.Error(), "certificat") {
		t.Fatalf("%v", err)
	}
	if len(states) == 0 || states[0] != "certificate" {
		t.Fatalf("%v", states)
	}
}

func TestZoneOfURL(t *testing.T) {
	if got := zoneOfURL("https://myinfra.devisgenerator.fr"); got != "devisgenerator.fr" {
		t.Fatalf("%q", got)
	}
	if got := zoneOfURL("not a url"); got != "" {
		t.Fatalf("%q", got)
	}
}

// Only a copy the new one can reach is given to it as a peer: a laptop syncs
// by itself.
func TestPeersForNewCopy(t *testing.T) {
	for _, c := range []struct {
		url  string
		keep bool
		want int
	}{
		{"http://127.0.0.1:8080", true, 0},
		{"http://192.168.1.10:8080", true, 0},
		{"https://myinfra.example.com", true, 1},
		{"https://myinfra.example.com", false, 0},
	} {
		a := &API{PublicURL: c.url}
		a.setTransferKeep(c.keep)
		if got := a.peersForNewCopy(); len(got) != c.want {
			t.Errorf("%s keep=%v : %v", c.url, c.keep, got)
		}
	}
}
