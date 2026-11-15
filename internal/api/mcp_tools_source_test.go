package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"myinfra/internal/store"
)

func TestRawLink(t *testing.T) {
	for link, want := range map[string]string{
		"https://github.com/suitenumerique/docs/blob/main/compose.yml":    "https://raw.githubusercontent.com/suitenumerique/docs/main/compose.yml",
		"https://github.com/o/r/blob/v1.2/deploy/compose.yaml?plain=1#L3": "https://raw.githubusercontent.com/o/r/v1.2/deploy/compose.yaml",
		"https://gitlab.com/o/r/-/blob/main/docker-compose.yml":           "https://gitlab.com/o/r/-/raw/main/docker-compose.yml",
		"https://raw.githubusercontent.com/o/r/main/compose.yml":          "https://raw.githubusercontent.com/o/r/main/compose.yml",
		"https://example.com/compose.yml":                                 "https://example.com/compose.yml",
	} {
		if got := rawLink(link); got != want {
			t.Errorf("%s: %s, want %s", link, got, want)
		}
	}
}

func TestGitHubFolder(t *testing.T) {
	for link, want := range map[string]string{
		"https://github.com/suitenumerique/docs":                                  "https://api.github.com/repos/suitenumerique/docs/contents/",
		"https://github.com/suitenumerique/docs/tree/main/documentation/examples": "https://api.github.com/repos/suitenumerique/docs/contents/documentation/examples?ref=main",
	} {
		u, _ := url.Parse(link)
		if got, ok := githubFolder(u); !ok || got != want {
			t.Errorf("%s: %s, want %s", link, got, want)
		}
	}
	u, _ := url.Parse("https://github.com/o/r/blob/main/x.yml")
	if _, ok := githubFolder(u); ok {
		t.Error("a file is not a folder")
	}
}

// read_source reads what anyone may read: never the machine's own network.
func TestReadSourceRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	out, err := readSource(context.Background(), srv.URL+"/compose.yml")
	if err == nil || strings.Contains(out, "secret") {
		t.Fatalf("%q %v", out, err)
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.4", "192.168.1.2", "169.254.169.254", "::1", "fd00::1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Errorf("%s is not public", ip)
		}
	}
	if !publicIP(net.ParseIP("185.199.108.133")) {
		t.Error("a public address is public")
	}
}

// convert_to_package hands the assistant the converted package with its
// warnings, which the API sends as {service, message}.
func TestMCPConvertKeepsWarnings(t *testing.T) {
	_, h := tokenFixture(t)
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token
	compose := "services:\n  db:\n    image: postgres:16\n    env_file: [db.env]\n  web:\n    image: example/web:1\n    depends_on: [db]\n"
	text, failed := mcpCallTool(t, h, deploy, "convert_to_package", map[string]any{"text": compose, "name": "app"})
	if failed || !strings.Contains(text, "db : Le fichier env_file n'est pas lu") || !strings.Contains(text, "name: web\n    image: example/web:1\n    public: true") {
		t.Fatal(text)
	}
	guide, failed := mcpCallTool(t, h, deploy, "get_package_guide", map[string]any{})
	if failed || !strings.Contains(guide, "## Adapter un docker-compose") {
		t.Fatal(guide)
	}
}
