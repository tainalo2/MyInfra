package dns

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSJSONParses(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu absent")
	}
	for _, provider := range []string{"cloudflare", "ovh"} {
		body, _, err := recordHCL(provider, Creds{Token: "dns-token", ApplicationKey: "k", ApplicationSecret: "dns-secret", ConsumerKey: "c"}, "exemple.fr", "app.exemple.fr", []string{"203.0.113.10", "203.0.113.11"}, "A")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "dns-token") || strings.Contains(body, "dns-secret") {
			t.Fatalf("%s contient un secret", provider)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.tf.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("tofu", "validate")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "Missing required provider") || strings.Contains(string(out), "Invalid") {
			t.Fatalf("%s: %v\n%s", provider, err, out)
		}
	}
}
