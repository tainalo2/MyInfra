package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAndRetag(t *testing.T) {
	ref, err := Parse("lissy93/dashy:3.1.1")
	if err != nil || ref.Host != "docker.io" || ref.Repo != "lissy93/dashy" || ref.Tag != "3.1.1" || ref.Name != "lissy93/dashy" {
		t.Fatalf("dashy: %+v %v", ref, err)
	}
	ref, err = Parse("wordpress:6-apache")
	if err != nil || ref.Repo != "library/wordpress" || ref.Tag != "6-apache" {
		t.Fatalf("wordpress: %+v %v", ref, err)
	}
	ref, err = Parse("ghcr.io/org/app:1.2")
	if err != nil || ref.Host != "ghcr.io" || ref.Repo != "org/app" {
		t.Fatalf("ghcr: %+v %v", ref, err)
	}
	next, err := WithTag("wordpress:6-apache", "latest")
	if err != nil || next != "wordpress:latest" {
		t.Fatalf("retag: %s %v", next, err)
	}
}

func TestSuggestKeepsTheFlavor(t *testing.T) {
	tags := []string{"latest", "6.8.1-apache", "6.8.1", "6.7.2-apache", "6-apache", "php8.2"}
	if got := Suggest("wordpress:6-apache", tags); got != "6.8.1-apache" {
		t.Fatalf("got %s", got)
	}
	if got := Suggest("postgres:16", []string{"latest", "16", "16.4", "17-alpine"}); got != "16.4" {
		t.Fatalf("got %s", got)
	}
	if got := Suggest("app:latest", []string{"latest", "2.0.0"}); got != "" {
		t.Fatalf("latest should stay, got %s", got)
	}
	if got := Suggest("lissy93/dashy:3.1.1", []string{"latest", "3.0.0"}); got != "latest" {
		t.Fatalf("missing tag should fall back to latest, got %s", got)
	}
}

func TestNextLinkResolvesRelative(t *testing.T) {
	got := nextLink(`</v2/library/demo/tags/list?n=100&last=1>; rel="next"`, "https://registry-1.docker.io/v2/library/demo/tags/list?n=100")
	if got != "https://registry-1.docker.io/v2/library/demo/tags/list?last=1&n=100" && got != "https://registry-1.docker.io/v2/library/demo/tags/list?n=100&last=1" {
		t.Fatal(got)
	}
}

func TestChoosePutsLatestFirst(t *testing.T) {
	got := Choose([]string{"1.0.0", "sha256-" + "abcdef", "latest", "2.0.0", "edge"})
	if len(got) < 2 || got[0] != "latest" || got[1] != "2.0.0" {
		t.Fatalf("%v", got)
	}
}

func TestListTagsFollowsTheRegistry(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/library/demo/tags/list" {
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="demo",scope="repository:library/demo:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tags":["1.2.3","latest","sha256-abcdefabcdefabcdefabcdefabcdefabcdefabcd"]}`))
			return
		}
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"tok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	tags, err := listTags(context.Background(), srv.Client(), srv.URL, "library/demo")
	if err != nil {
		t.Fatal(err)
	}
	chosen := Choose(tags)
	if len(chosen) != 2 || chosen[0] != "latest" || chosen[1] != "1.2.3" {
		t.Fatalf("%v", chosen)
	}
}

func TestImagePortPrefersWebPorts(t *testing.T) {
	cases := []struct {
		ports []int
		want  int
	}{
		{nil, 0},
		{[]int{8081}, 8081},
		{[]int{443, 80}, 80},
		{[]int{443}, 0},
		{[]int{22, 3000}, 3000},
	}
	for _, c := range cases {
		if got := (Image{Ports: c.ports}).Port(); got != c.want {
			t.Fatalf("%v: got %d want %d", c.ports, got, c.want)
		}
	}
}
