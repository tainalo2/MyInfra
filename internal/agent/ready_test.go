package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBin puts docker and sleep scripts first in PATH: docker answers with
// state for every inspect, sleep returns at once.
func fakeBin(t *testing.T, state string) []string {
	t.Helper()
	dir := t.TempDir()
	scripts := map[string]string{
		"docker": "#!/bin/sh\ncase \"$3\" in *Running*) echo '" + state + "' ;; *) echo '' ;; esac\n",
		"sleep":  "#!/bin/sh\nexit 0\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return append(os.Environ(), "PATH="+dir+":/usr/bin:/bin")
}

func runReady(t *testing.T, state string) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", readyFunc+"ready web-next 0")
	cmd.Env = fakeBin(t, state)
	return cmd.Run()
}

func TestReadyWaitsForARunningContainer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell")
	}
	if err := runReady(t, "true 0"); err != nil {
		t.Fatalf("un conteneur qui tourne est refusé : %v", err)
	}
	if err := runReady(t, "true 2"); err == nil {
		t.Fatal("un conteneur qui redémarre en boucle est accepté")
	}
	if err := runReady(t, "false 0"); err == nil {
		t.Fatal("un conteneur arrêté est accepté")
	}
}

func TestSwapKeepsTheOldCopyWhenTheNewOneFails(t *testing.T) {
	script, err := Sync("https://myinfra.example.com", "0123456789abcdef0123456789abcdef", []Workload{{
		ID: "7d49736b-58e7-4386-b947-720823531792", Service: "web", Image: "nginx:1.27", Published: true, Hosts: []string{"app.example.com"}, Replicas: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ready 'myinfra-7d49736b58e74386b947720823531792-web-1-next' 80; then",
		"echo 'myinfra-kept myinfra-7d49736b58e74386b947720823531792-web-2'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("%q absent :\n%s", want, script)
		}
	}
}
