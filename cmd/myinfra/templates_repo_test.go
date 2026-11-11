package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Without MYINFRA_TEMPLATES_REPO the gateway writes to a throwaway repository
// under the temp dir, never to a real one.
func TestCommitGatewayDefaultsToDisposableRepo(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("MYINFRA_TEMPLATES_REPO", "")
	g, err := commitGateway(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(g.Repo.Path, tmp) {
		t.Fatalf("dépôt %q hors du dossier temporaire %q", g.Repo.Path, tmp)
	}
	out, err := exec.Command("git", "-C", g.Repo.Path, "log", "--oneline").CombinedOutput()
	if err != nil || strings.Count(strings.TrimSpace(string(out)), "\n") != 0 || out == nil {
		t.Fatalf("git log : %v\n%s", err, out)
	}
	// Called again, the same repository is reused, not rebuilt.
	if g2, err := commitGateway(nil); err != nil || g2.Repo.Path != g.Repo.Path {
		t.Fatalf("second appel : %v %v", g2, err)
	}
}

func TestCommitGatewayUsesConfiguredRepoAndRefusesNonRepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYINFRA_TEMPLATES_REPO", dir)
	if _, err := commitGateway(nil); err == nil {
		t.Fatal("un dossier qui n'est pas un dépôt git doit être refusé")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	g, err := commitGateway(nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.Repo.Path != filepath.Clean(dir) {
		t.Fatalf("chemin %q", g.Repo.Path)
	}
}
