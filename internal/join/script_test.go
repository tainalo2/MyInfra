package join

import (
	"os/exec"
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

func TestKeysFileKeepsMyInfraKeysApart(t *testing.T) {
	script, err := KeysFile([]string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample myinfra"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/etc/ssh/myinfra_authorized_keys", "02-myinfra-keys.conf", "sshd -t", "grep -qxF 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample myinfra' /root/.ssh/authorized_keys"} {
		if !strings.Contains(script, want) {
			t.Fatalf("%q absent :\n%s", want, script)
		}
	}
	if out, err := exec.Command("sh", "-n", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := KeysFile([]string{"ssh-ed25519 AAAA' ; rm -rf /"}); err == nil {
		t.Fatal("clé avec apostrophe acceptée")
	}
}
