package commit

import (
	"context"
	"errors"
	"testing"
)

// failingStaging fails the test if the gateway reads it: a refused actor
// must be turned away before the staging, let alone the repository.
type failingStaging struct{ t *testing.T }

func (f failingStaging) GetPatch(context.Context, string) (Patch, error) {
	f.t.Fatal("staging read for a refused actor")
	return Patch{}, nil
}

func (f failingStaging) MarkApplied(context.Context, []string) error {
	f.t.Fatal("staging marked for a refused actor")
	return nil
}

func TestApplyRefusesActorsWithoutWriteTemplates(t *testing.T) {
	g := &Gateway{Repo: LiveRepo{Path: "/nonexistent/live/repo"}, Staging: failingStaging{t}}
	for _, actor := range []Actor{{}, {ID: "deployer"}, {ID: "reader", Admin: false, WriteTemplates: false}} {
		err := g.Apply(context.Background(), actor, []string{"p1"})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("%+v: err=%v, want ErrForbidden", actor, err)
		}
	}
}

func TestActorCanWrite(t *testing.T) {
	cases := map[Actor]bool{
		{}:                                  false,
		{WriteTemplates: true}:              true,
		{Admin: true}:                       true,
		{Admin: true, WriteTemplates: true}: true,
	}
	for actor, want := range cases {
		if got := actor.CanWrite(); got != want {
			t.Fatalf("%+v: %v", actor, got)
		}
	}
}
