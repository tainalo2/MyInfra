package join

import (
	"strings"
	"testing"
)

func TestScriptIncludesKeyAndToken(t *testing.T) {
	script, err := Script("http://localhost:8080", "abc", []string{"ssh-ed25519 AAAAC3 test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "chage -d 99999 root") || !strings.Contains(script, "ssh-ed25519 AAAAC3 test") || !strings.Contains(script, `"token":"abc"`) || !strings.Contains(script, "PermitRootLogin prohibit-password") {
		t.Fatal(script)
	}
	if !strings.Contains(script, "https://get.docker.com") || !strings.Contains(script, "/agent/abc/sync") {
		t.Fatal(script)
	}
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "echo") && (strings.Contains(line, "Docker") || strings.Contains(line, "Traefik") || strings.Contains(line, "443")) {
			t.Fatal(line)
		}
	}
}

func TestScriptRejectsQuote(t *testing.T) {
	if _, err := Script("http://localhost:8080", "a'b", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestNodeScriptLeavesTheWebPortsFree(t *testing.T) {
	script, err := NodeScript("http://localhost:8080", "abc", []string{"ssh-ed25519 AAAAC3 test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "ssh-ed25519 AAAAC3 test") || !strings.Contains(script, `"token":"abc"`) {
		t.Fatal(script)
	}
	if strings.Contains(script, "docker") || strings.Contains(script, "/agent/abc/sync") {
		t.Fatal(script)
	}
}
