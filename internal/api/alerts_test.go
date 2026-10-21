package api

import (
	"context"
	"testing"
)

func TestAlertTransitionDedup(t *testing.T) {
	s := &alertState{last: map[string]string{}}
	ctx := context.Background()
	s.transition(ctx, "k", "lost", "t", "b")
	if s.last["k"] != "lost" {
		t.Fatal("état non mémorisé")
	}
	s.transition(ctx, "k", "lost", "t", "b")
	s.clear("k")
	if s.last["k"] != "" {
		t.Fatal("clear")
	}
}
