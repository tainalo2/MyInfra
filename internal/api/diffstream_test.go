package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The stream tells the owner's open connection, and nobody else's, about
// each hunk of a patch and its end.
func TestDiffStreamDeliversHunksToTheOwnerOnly(t *testing.T) {
	a := &API{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.diffStream(w, r.WithContext(context.WithValue(r.Context(), userKey, r.Header.Get("X-User"))))
	}))
	t.Cleanup(srv.Close) // runs after the bodies below are closed

	open := func(user string) *bufio.Scanner {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		req.Header.Set("X-User", user)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("content-type %q", ct)
		}
		sc := bufio.NewScanner(res.Body)
		sc.Scan() // ": ouvert"
		sc.Scan() // blank line
		return sc
	}
	owner, other := open("u1"), open("u2")

	deadline := time.Now().Add(2 * time.Second)
	for {
		a.diffs.mu.Lock()
		n := len(a.diffs.subs["u1"]) + len(a.diffs.subs["u2"])
		a.diffs.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connexions non enregistrées")
		}
		time.Sleep(10 * time.Millisecond)
	}

	a.announce("u1", []diffHunk{{ID: "p1", File: "a.yaml"}, {ID: "p2", File: "a.yaml"}}, map[string]bool{"p2": true})

	var got []string
	for len(got) < 5 && owner.Scan() {
		if l := owner.Text(); l != "" {
			got = append(got, l)
		}
	}
	text := strings.Join(got, "\n")
	for _, want := range []string{`"id":"p1"`, "event: modified", `"id":"p2"`, "event: done"} {
		if !strings.Contains(text, want) {
			t.Errorf("flux sans %q :\n%s", want, text)
		}
	}
	if strings.Index(text, "event: modified") < strings.Index(text, `"id":"p1"`) {
		t.Errorf("p1 doit précéder p2 :\n%s", text)
	}

	// The other account's stream stays quiet (only keep-alive comments).
	done := make(chan string, 1)
	go func() {
		if other.Scan() {
			done <- other.Text()
		}
	}()
	select {
	case l := <-done:
		t.Errorf("l'autre compte a reçu %q", l)
	case <-time.After(200 * time.Millisecond):
	}
}
