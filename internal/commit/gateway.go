package commit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type Patch struct {
	ID      string
	HunkID  string
	Path    string
	Content string
}

type StagingStore interface {
	GetPatch(ctx context.Context, id string) (Patch, error)
	MarkApplied(ctx context.Context, ids []string) error
}

type LiveRepo struct {
	Path string
}

// ErrForbidden is returned by Apply when the actor may not write to the live
// repository. The repository is left untouched.
var ErrForbidden = errors.New("commit: actor may not write templates")

// Actor is who asks for a commit. Only an admin, or an account granted the
// explicit "write templates" permission, may write to the live repository.
// The deploy role alone never does: the permission is distinct from it.
type Actor struct {
	ID             string
	Admin          bool
	WriteTemplates bool
}

// CanWrite tells whether the actor may commit to the live repository.
func (a Actor) CanWrite() bool { return a.Admin || a.WriteTemplates }

type Gateway struct {
	Repo    LiveRepo
	Staging StagingStore
}

// Apply writes the selected patches to the live repository in one commit.
// The permission check happens here, before any read of the staging, so
// the gateway stays the choke point even for callers that skip the HTTP
// layer.
func (g *Gateway) Apply(ctx context.Context, actor Actor, selected []string) error {
	if !actor.CanWrite() {
		return ErrForbidden
	}
	if len(selected) == 0 {
		return fmt.Errorf("no hunks selected")
	}
	patches := make([]Patch, 0, len(selected))
	for _, id := range selected {
		p, err := g.Staging.GetPatch(ctx, id)
		if err != nil {
			return fmt.Errorf("get patch %s: %w", id, err)
		}
		patches = append(patches, p)
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].ID < patches[j].ID })

	for _, p := range patches {
		cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
		cmd.Dir = g.Repo.Path
		cmd.Stdin = strings.NewReader(p.Content)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git apply %s: %w\n%s", p.ID, err, string(out))
		}
	}

	if out, err := exec.CommandContext(ctx, "git", "-C", g.Repo.Path, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, string(out))
	}

	ids := make([]string, 0, len(patches))
	for _, p := range patches {
		ids = append(ids, p.ID)
	}
	msg := "apply: " + strings.Join(ids, ", ")

	if out, err := exec.CommandContext(ctx, "git", "-C", g.Repo.Path, "commit", "-m", msg).CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, string(out))
	}

	if err := g.Staging.MarkApplied(ctx, selected); err != nil {
		return fmt.Errorf("mark applied: %w", err)
	}
	return nil
}
