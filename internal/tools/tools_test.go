package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPlacedBinaryIsFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MYINFRA_HOME", home)
	if !filepath.IsAbs(home) {
		t.Fatal(home)
	}
	dir := filepath.Join(home, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "tofu"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("tofu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Tofu() == "" {
		t.Fatal("binaire posé introuvable")
	}
	PreparePath()
	if !containsPath(os.Getenv("PATH"), dir) {
		t.Fatal("PATH inchangé")
	}
}

func containsPath(path, dir string) bool {
	for _, item := range filepath.SplitList(path) {
		if item == dir {
			return true
		}
	}
	return false
}
