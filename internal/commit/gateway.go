package commit

import (
	"context"
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

type Gateway struct {
	Repo    LiveRepo
	Staging StagingStore
}

func (g *Gateway) Apply(ctx context.Context, selected []string) error {
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