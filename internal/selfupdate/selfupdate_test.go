package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.2.4", "1.2.3", true},
		{"1.10.0", "1.9.9", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.3-rc.1", true},
		{"1.2.3-rc.2", "1.2.3-rc.1", true},
		{"1.2.3-rc.1", "1.2.3", false},
		{"2.0.0", "dev", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestLatestSkipsDraftsAndPrereleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+Repo()+"/releases" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[
			{"tag_name":"v1.4.0-rc.1","prerelease":true,"html_url":"pre"},
			{"tag_name":"v1.5.0","draft":true},
			{"tag_name":"v1.3.0","html_url":"stable","assets":[{"name":"SHA256SUMS","browser_download_url":"x"}]},
			{"tag_name":"v1.2.0"}
		]`)
	}))
	defer srv.Close()
	before := API
	API = srv.URL
	defer func() { API = before }()
	rel, err := Latest(context.Background(), "1.2.0")
	if err != nil || rel.Version != "1.3.0" || rel.URL != "stable" || rel.Assets["SHA256SUMS"] != "x" {
		t.Fatalf("%+v %v", rel, err)
	}
	rel, err = Latest(context.Background(), "1.3.0-rc.1")
	if err != nil || rel.Version != "1.4.0-rc.1" {
		t.Fatalf("préversion : %+v %v", rel, err)
	}
}

func TestDownloadChecksTheSum(t *testing.T) {
	body := []byte("#!/bin/sh\necho 1.3.0\n")
	sum := sha256.Sum256(body)
	name := AssetName("1.3.0", runtime.GOOS, runtime.GOARCH)
	good := hex.EncodeToString(sum[:])
	sums := good + "  " + name + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sums":
			fmt.Fprint(w, sums)
		case "/bin":
			_, _ = w.Write(body)
		}
	}))
	defer srv.Close()
	exe := filepath.Join(t.TempDir(), "myinfra")
	rel := Release{Version: "1.3.0", Assets: map[string]string{name: srv.URL + "/bin", "SHA256SUMS": srv.URL + "/sums"}}
	next, err := Download(context.Background(), rel, exe)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(next); string(got) != string(body) {
		t.Fatalf("%q", got)
	}
	sums = strings.Repeat("0", 64) + "  " + name + "\n"
	if _, err := Download(context.Background(), rel, exe); err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
		t.Fatalf("somme fausse acceptée : %v", err)
	}
	if _, err := os.Stat(next); !os.IsNotExist(err) {
		t.Fatal("le fichier refusé est resté")
	}
}

func TestSwapKeepsThePrevious(t *testing.T) {
	dir := t.TempDir()
	exe, next := filepath.Join(dir, "myinfra"), filepath.Join(dir, "myinfra.new")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	_ = os.WriteFile(next, []byte("new"), 0o755)
	if err := Swap(exe, next); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("%q", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old" {
		t.Fatalf("%q", got)
	}
}

func TestCheckReadsTheVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script shell")
	}
	next := filepath.Join(t.TempDir(), "next")
	_ = os.WriteFile(next, []byte("#!/bin/sh\necho 1.3.0\n"), 0o755)
	if err := Check(context.Background(), next, "1.3.0"); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), next, "1.4.0"); err == nil {
		t.Fatal("mauvaise version acceptée")
	}
}

func TestPostgresOfARelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"1.3.0","postgres":"17"}`)
	}))
	defer srv.Close()
	got, err := Postgres(context.Background(), Release{Version: "1.3.0", Assets: map[string]string{"release.json": srv.URL}})
	if err != nil || got != "17" {
		t.Fatalf("%q %v", got, err)
	}
	if old, _ := Postgres(context.Background(), Release{Assets: map[string]string{}}); old != "16" {
		t.Fatalf("ancienne release : %q", old)
	}
	if err := SamePostgres(Release{Version: "1.3.0"}, "16", "17"); err == nil || !strings.Contains(err.Error(), "PostgreSQL 17") {
		t.Fatalf("%v", err)
	}
	if err := SamePostgres(Release{}, "", "17"); err != nil {
		t.Fatal("sans PostgreSQL embarqué, la version ne compte pas")
	}
}

func TestAssetNameVariant(t *testing.T) {
	defer func() { Variant = "" }()
	for _, c := range []struct{ variant, goos, want string }{
		{"", "linux", "myinfra_1.4.0_linux_amd64"},
		{"", "windows", "myinfra_1.4.0_windows_amd64.exe"},
		{"app", "windows", "myinfra_1.4.0_windows_amd64_app.exe"},
		{"AppImage", "linux", "myinfra_1.4.0_linux_amd64.AppImage"},
	} {
		Variant = c.variant
		if got := AssetName("1.4.0", c.goos, "amd64"); got != c.want {
			t.Errorf("AssetName(%q, %s) = %s, want %s", c.variant, c.goos, got, c.want)
		}
	}
}
