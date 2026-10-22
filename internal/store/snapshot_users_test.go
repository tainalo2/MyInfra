package store

import (
	"context"
	"testing"
)

func TestSnapshotKeepsLoginRoleAndTOTP(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_snap_users"
	resetDB(t, admin, name)
	db, err := Open(ctx, dsn(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		dropDB(t, admin, name)
	}()

	if err := db.CreateUserAccount(ctx, "user-1", "tainalo2", "admin", "hash-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserTOTP(ctx, "user-1", "SECRET"); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishOnboarding(ctx, "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUserAccount(ctx, "user-2", "reader", "read", "hash-2"); err != nil {
		t.Fatal(err)
	}

	snap, err := db.ExportSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Users) != 2 || snap.Users[0].Login != "tainalo2" || snap.Users[0].TOTPSecret != "SECRET" || !snap.Users[0].Onboarded {
		t.Fatalf("export %+v", snap.Users)
	}
	if snap.Users[1].Login != "reader" || snap.Users[1].Role != "read" || snap.Users[1].TOTPSecret != "" {
		t.Fatalf("second %+v", snap.Users[1])
	}

	const other = "myinfra_snap_users_b"
	resetDB(t, admin, other)
	dst, err := Open(ctx, dsn(other))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		dst.Close()
		dropDB(t, admin, other)
	}()
	if err := dst.ImportSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	account, err := dst.UserByLogin(ctx, "tainalo2")
	if err != nil {
		t.Fatal(err)
	}
	if account.Role != "admin" || account.TOTPSecret != "SECRET" || account.PasswordHash != "hash-1" {
		t.Fatalf("imported %+v", account)
	}
	reader, err := dst.UserByLogin(ctx, "reader")
	if err != nil {
		t.Fatal(err)
	}
	if reader.Role != "read" {
		t.Fatalf("role %s", reader.Role)
	}
}
