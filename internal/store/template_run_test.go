package store

import (
	"bytes"
	"context"
	"testing"
)

func TestPersistRunningTemplateRun(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_template_run"
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
	if err := db.CreateTemplateRun(ctx, "user-1", "run-1", "", "", []byte(`{"steps":[]}`)); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"steps":[{"kind":"server","status":"done"}]}`)
	if err := db.PersistRunningTemplateRun(ctx, "user-1", "run-1", "done", "cluster-1", body); err != nil {
		t.Fatal(err)
	}
	row, err := db.TemplateRun(ctx, "user-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "done" || row.ClusterID != "cluster-1" || !bytes.Contains(row.Body, []byte(`"done"`)) {
		t.Fatalf("suivi non enregistré : %s %s %s", row.Status, row.ClusterID, row.Body)
	}
	if err := db.PersistRunningTemplateRun(ctx, "user-1", "run-1", "failed", "", []byte(`{}`)); err != ErrStopped {
		t.Fatalf("un suivi terminé a été réécrit : %v", err)
	}
}
