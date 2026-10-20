package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const someDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestDigestReadsTheManifestHeader(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/library/postgres/manifests/16":
			if r.Method != http.MethodHead {
				t.Errorf("méthode %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="reg"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if !strings.Contains(r.Header.Get("Accept"), "manifest.list") {
				t.Errorf("accept %q", r.Header.Get("Accept"))
			}
			w.Header().Set("Docker-Content-Digest", someDigest)
		case "/token":
			_, _ = w.Write([]byte(`{"token":"tok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	got, err := headDigest(context.Background(), srv.Client(), srv.URL+"/v2/library/postgres/manifests/16")
	if err != nil || got != someDigest {
		t.Fatalf("%q %v", got, err)
	}
}

func TestPinnedImages(t *testing.T) {
	pinned, err := Pinned("postgres:16", someDigest)
	if err != nil || pinned != "postgres:16@"+someDigest {
		t.Fatalf("%q %v", pinned, err)
	}
	if DigestOf(pinned) != someDigest || Unpinned(pinned) != "postgres:16" {
		t.Fatalf("%q %q", DigestOf(pinned), Unpinned(pinned))
	}
	// Pinning again replaces the build, and a new version drops it.
	again, _ := Pinned(pinned, someDigest)
	if again != pinned {
		t.Fatalf("%q", again)
	}
	if next, _ := WithTag(pinned, "17"); next != "postgres:17" {
		t.Fatalf("%q", next)
	}
	if _, err := Pinned("postgres:16", "sha256:nope"); err == nil {
		t.Fatal("empreinte acceptée")
	}
	if DigestOf("postgres:16") != "" || Unpinned("postgres:16") != "postgres:16" {
		t.Fatal("image sans empreinte")
	}
}

func TestLevel(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"miniflux/miniflux:2.2.6", "miniflux/miniflux:2.2.7", "patch"},
		{"miniflux/miniflux:2.2.6", "miniflux/miniflux:2.3.0", "minor"},
		{"postgres:16", "postgres:17", "major"},
		{"ghost:5-alpine", "ghost:latest", "minor"},
	} {
		if got := Level(c.from, c.to); got != c.want {
			t.Errorf("%s → %s : %s", c.from, c.to, got)
		}
	}
}
