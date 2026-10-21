package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPullConfigBuildsDockerHubAuth(t *testing.T) {
	registryMu.Lock()
	registryCache = map[string]registryLogin{
		"docker.io": {User: "hubuser", Password: "hubpass"},
		"ghcr.io":   {User: "gituser", Password: "gitpass"},
	}
	registryCached = time.Now()
	registryMu.Unlock()
	defer forgetRegistryLogins()

	a := &API{}
	raw := a.pullConfig(context.Background(), []string{"private/app:1.0", "ghcr.io/org/svc:latest"})
	if raw == "" {
		t.Fatal("config vide")
	}
	var doc struct {
		Auths map[string]map[string]string `json:"auths"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	hub := doc.Auths["https://index.docker.io/v1/"]
	if hub["username"] != "hubuser" || hub["password"] != "hubpass" {
		t.Fatalf("docker hub : %+v", hub)
	}
	if doc.Auths["ghcr.io"]["username"] != "gituser" {
		t.Fatalf("ghcr : %+v", doc.Auths["ghcr.io"])
	}
	if !strings.Contains(hub["auth"], "aHVid") { // base64 prefix of hubuser:
		t.Fatal("auth manquant")
	}
}
