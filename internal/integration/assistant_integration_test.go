//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"myinfra/internal/store"
)

// newDisposableGitRepo creates a fresh git repository in a temp dir, seeded with
// a single commit, and returns its path. The repo is removed when the test ends.
func newDisposableGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# disposable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

// TestAssistantUnavailableNoNetwork verifies scenario 1.
func TestAssistantUnavailableNoNetwork(t *testing.T) {
	t.Skip("skeleton: wire BYOK key store + network interceptor")
}

// TestAssistantApplyCommit verifies scenario 2.
func TestAssistantApplyCommit(t *testing.T) {
	repo := newDisposableGitRepo(t)
	t.Logf("disposable repo: %s", repo)
	_ = store.Staging{}
	t.Skip("skeleton: wire staging + commit gateway")
}

// TestAssistantPartialApplyTwoCommits verifies scenario 3.
func TestAssistantPartialApplyTwoCommits(t *testing.T) {
	repo := newDisposableGitRepo(t)
	t.Logf("disposable repo: %s", repo)
	t.Skip("skeleton: wire partial apply")
}

// TestStagingTTLExpiry verifies scenario 4.
func TestStagingTTLExpiry(t *testing.T) {
	t.Skip("skeleton: wire mocked clock")
}
