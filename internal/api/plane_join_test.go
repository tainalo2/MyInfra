package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlaneJoinsComeOneAtATime(t *testing.T) {
	a := &API{}
	var inside, most atomic.Int32
	var wait sync.WaitGroup
	for range 5 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			release, err := a.holdPlaneJoin(context.Background(), "cluster")
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			now := inside.Add(1)
			if now > most.Load() {
				most.Store(now)
			}
			time.Sleep(5 * time.Millisecond)
			inside.Add(-1)
		}()
	}
	wait.Wait()
	if most.Load() != 1 {
		t.Fatalf("%d control planes joined together", most.Load())
	}
}

func TestPlaneJoinOfAnotherClusterDoesNotWait(t *testing.T) {
	a := &API{}
	release, err := a.holdPlaneJoin(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	other, err := a.holdPlaneJoin(ctx, "two")
	if err != nil {
		t.Fatal(err)
	}
	other()
}

// A cancelled deployment leaves the queue, and the turn stays free for the next.
func TestCancelledPlaneJoinLeavesTheQueue(t *testing.T) {
	a := &API{}
	release, err := a.holdPlaneJoin(context.Background(), "cluster")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.holdPlaneJoin(ctx, "cluster"); err == nil {
		t.Fatal("cancelled wait got the turn")
	}
	release()
	next, cancelNext := context.WithTimeout(context.Background(), time.Second)
	defer cancelNext()
	again, err := a.holdPlaneJoin(next, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	again()
}
