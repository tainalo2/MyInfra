package store

import (
	"context"
	"errors"
	"testing"
)

func TestUpdatesAndJobs(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_updates"
	resetDB(t, admin, name)
	db, err := Open(ctx, dsn(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		dropDB(t, admin, name)
	}()
	if err := db.CreateUser(ctx, "user-1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	found := []Update{
		{TargetID: "app-1", OwnerID: "user-1", Current: "nginx:1.26", Available: "nginx:1.27", Level: "minor"},
		{TargetID: "app-2", OwnerID: "user-1", Current: "redis:7", Available: "redis:7", Level: "build"},
	}
	if err := db.SaveUpdates(ctx, "app", found, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveUpdates(ctx, "myinfra", []Update{{TargetID: "myinfra", Current: "1.0.0", Available: "1.1.0", Level: "minor"}}, nil); err != nil {
		t.Fatal(err)
	}
	// A check of some servers keeps what was found for the others.
	if err := db.SaveUpdates(ctx, "server", []Update{{TargetID: "s1", OwnerID: "user-1", Level: "security"}, {TargetID: "s2", OwnerID: "user-1", Level: "reboot"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveUpdates(ctx, "server", nil, []string{"s1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Updates(ctx, "user-1")
	if err != nil || len(rows) != 4 {
		t.Fatalf("%d %v", len(rows), err)
	}
	if err := db.SaveUpdates(ctx, "app", found[:1], nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateOf(ctx, "app", "app-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("une application à jour reste listée : %v", err)
	}
	if err := db.ForgetUpdate(ctx, "app", "app-1"); err != nil {
		t.Fatal(err)
	}
	first, err := db.ImageBaseline(ctx, "redis:7", "sha256:a")
	if err != nil || first != "sha256:a" {
		t.Fatalf("%q %v", first, err)
	}
	if kept, _ := db.ImageBaseline(ctx, "redis:7", "sha256:b"); kept != "sha256:a" {
		t.Fatalf("le premier build vu est remplacé : %q", kept)
	}

	job := UpdateJob{ID: "job-1", OwnerID: "user-1", ClusterID: "c1", Steps: []byte(`[]`)}
	if err := db.StartUpdateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := db.StartUpdateJob(ctx, UpdateJob{ID: "job-2", OwnerID: "user-1", ClusterID: "c1", ServerID: "s1", Steps: []byte(`[]`)}); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("deux mises à jour sur le même cluster : %v", err)
	}
	if err := db.StartUpdateJob(ctx, UpdateJob{ID: "job-3", OwnerID: "user-1", ServerID: "m1", Steps: []byte(`[]`)}); err != nil {
		t.Fatalf("une machine seule attend un autre cluster : %v", err)
	}
	if err := db.SaveUpdateJob(ctx, "job-1", "done", []byte(`[{"action":"packages","status":"done"}]`)); err != nil {
		t.Fatal(err)
	}
	running, err := db.RunningUpdateJobs(ctx)
	if err != nil || len(running) != 1 || running[0].ID != "job-3" {
		t.Fatalf("%+v %v", running, err)
	}
	done, err := db.UpdateJob(ctx, "user-1", "job-1")
	if err != nil || done.FinishedAt == nil {
		t.Fatalf("%+v %v", done, err)
	}

	if role, err := db.CopyRole(ctx); err != nil || role.Replica {
		t.Fatalf("une copie neuve pilote : %+v %v", role, err)
	}
	if err := db.SetCopyRole(ctx, CopyRole{Replica: true, MainMethod: "pod", MainClusterID: "c1", MainURL: "https://myinfra2.example.com"}); err != nil {
		t.Fatal(err)
	}
	if role, _ := db.CopyRole(ctx); !role.Replica || role.MainClusterID != "c1" || role.MainURL != "https://myinfra2.example.com" {
		t.Fatalf("%+v", role)
	}
}
