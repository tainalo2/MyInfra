package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"myinfra/internal/commit"
	"myinfra/internal/store"
)

// sandboxRepoDir is where the throwaway templates repository lives when
// MYINFRA_TEMPLATES_REPO is not set.
func sandboxRepoDir() string {
	return filepath.Join(os.TempDir(), "myinfra-templates-sandbox")
}

// commitGateway builds the Commit Gateway the assistant writes through.
//
// The live templates repository comes from MYINFRA_TEMPLATES_REPO only.
// Without it, the gateway writes to a throwaway git repository under the
// temp dir, never to a real one: a dev or CI run that forgot the variable
// cannot touch production templates. A real path is refused unless it is a
// git work tree, and a relative one is made absolute.
func commitGateway(db *store.Store) (*commit.Gateway, error) {
	path := strings.TrimSpace(os.Getenv("MYINFRA_TEMPLATES_REPO"))
	if path == "" {
		path = sandboxRepoDir()
		if err := initSandboxRepo(path); err != nil {
			return nil, fmt.Errorf("dépôt de test des modèles : %w", err)
		}
		slog.Warn("MYINFRA_TEMPLATES_REPO absent : les commits de l'assistant vont dans un dépôt jetable", "path", path)
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = abs
		out, err := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree").CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			return nil, fmt.Errorf("MYINFRA_TEMPLATES_REPO %q n'est pas un dépôt git", path)
		}
		slog.Info("dépôt des modèles pour les commits de l'assistant", "path", path)
	}
	return &commit.Gateway{Repo: commit.LiveRepo{Path: path}, Staging: db}, nil
}

// initSandboxRepo makes sure dir is a git repository with one commit and a
// local identity, so the gateway has something to apply patches onto.
func initSandboxRepo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	git := func(args ...string) error {
		out, err := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s : %w\n%s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	readme := "# Dépôt de modèles jetable\n\nCréé par MyInfra faute de MYINFRA_TEMPLATES_REPO.\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "MyInfra Assistant"},
		{"config", "user.email", "assistant@myinfra.invalid"},
		{"add", "-A"},
		{"commit", "-q", "-m", "init: dépôt de modèles jetable"},
	} {
		if err := git(args...); err != nil {
			return err
		}
	}
	return nil
}
