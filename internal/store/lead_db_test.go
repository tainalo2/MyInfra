package store

import (
	"context"
	"testing"
	"time"
)

// Two copies on one database: one drives, the other waits and takes over.
func TestLeadIsHeldByOneCopy(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_lead"
	resetDB(t, admin, name)
	db, err := Open(ctx, dsn(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		dropDB(t, admin, name)
	}()
	first, stopFirst := context.WithCancel(ctx)
	if !db.Lead(first) {
		t.Fatal("la première copie ne pilote pas")
	}
	waiting, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if db.Lead(waiting) {
		t.Fatal("deux copies pilotent")
	}
	stopFirst()
	second, stopSecond := context.WithTimeout(ctx, 15*time.Second)
	defer stopSecond()
	if !db.Lead(second) {
		t.Fatal("la seconde copie ne reprend pas")
	}
}
