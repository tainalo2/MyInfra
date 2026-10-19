package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSyncTwoDatabasesConverge(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const leftName = "myinfra_sync_a"
	const rightName = "myinfra_sync_b"
	resetDB(t, admin, leftName)
	resetDB(t, admin, rightName)
	left, err := Open(ctx, dsn(leftName))
	if err != nil {
		t.Fatal(err)
	}
	right, err := Open(ctx, dsn(rightName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		left.Close()
		right.Close()
		dropDB(t, admin, leftName)
		dropDB(t, admin, rightName)
	}()

	if err := left.CreateUser(ctx, "user-1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := left.pool.Exec(ctx, `
		INSERT INTO secrets (id, owner_id, kind, name, ciphertext, nonce, wrapped_dek)
		VALUES ('sec-1', 'user-1', 'ssh_key', 'clé', '\x01', '\x02', '\x03')`); err != nil {
		t.Fatal(err)
	}
	if _, err := left.pool.Exec(ctx, `INSERT INTO installation (singleton, public_url) VALUES (true, 'https://gauche.example')`); err != nil {
		t.Fatal(err)
	}
	if _, err := left.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ('sess', 'user-1', now() + interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	converge(t, ctx, left, right)
	var n int
	if err := right.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("users %d %v", n, err)
	}
	if err := right.pool.QueryRow(ctx, `SELECT count(*) FROM secrets`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("secrets %d %v", n, err)
	}
	if err := right.pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sessions %d %v", n, err)
	}
	err = right.pool.QueryRow(ctx, `SELECT public_url FROM installation WHERE singleton`).Scan(new(string))
	if err == nil {
		t.Fatal("l'installation locale a été copiée")
	}

	if err := right.CreateUser(ctx, "user-2", "hash-2"); err != nil {
		t.Fatal(err)
	}
	converge(t, ctx, left, right)
	if err := left.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("users %d %v", n, err)
	}

	if _, err := left.pool.Exec(ctx, `DELETE FROM users WHERE id = 'user-1'`); err != nil {
		t.Fatal(err)
	}
	converge(t, ctx, left, right)
	if err := right.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = 'user-1'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("user-1 %d %v", n, err)
	}
	if err := right.pool.QueryRow(ctx, `SELECT count(*) FROM secrets`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("secret restant %d %v", n, err)
	}
	if err := right.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = 'user-2'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("user-2 %d %v", n, err)
	}
	leftDoc, err := left.SyncExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rightDoc, err := right.SyncExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if leftDoc.Hash() != rightDoc.Hash() {
		t.Fatal("les deux copies divergent")
	}
}

func converge(t *testing.T, ctx context.Context, left, right *Store) {
	t.Helper()
	sent, err := left.SyncExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := right.SyncMerge(ctx, sent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := left.SyncMerge(ctx, merged); err != nil {
		t.Fatal(err)
	}
	sent, err = right.SyncExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	merged, err = left.SyncMerge(ctx, sent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := right.SyncMerge(ctx, merged); err != nil {
		t.Fatal(err)
	}
}

func testAdmin(t *testing.T) (*pgx.Conn, func(string) string) {
	t.Helper()
	ctx := context.Background()
	for _, dsn := range []string{
		"postgres://127.0.0.1:54329/postgres?sslmode=disable",
		"postgres://myinfra@127.0.0.1:54329/postgres?sslmode=disable",
	} {
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			continue
		}
		if err := conn.Ping(ctx); err != nil {
			conn.Close(ctx)
			continue
		}
		var user string
		if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&user); err != nil {
			conn.Close(ctx)
			continue
		}
		return conn, func(name string) string {
			return "postgres://" + user + "@127.0.0.1:54329/" + name + "?sslmode=disable"
		}
	}
	t.Skip("postgres indisponible")
	return nil, nil
}

func resetDB(t *testing.T, conn *pgx.Conn, name string) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name); err != nil {
		t.Fatal(err)
	}
	execSimple(t, conn, "DROP DATABASE IF EXISTS "+name)
	execSimple(t, conn, "CREATE DATABASE "+name)
}

func dropDB(t *testing.T, conn *pgx.Conn, name string) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name); err != nil {
		t.Fatal(err)
	}
	execSimple(t, conn, "DROP DATABASE IF EXISTS "+name)
}

func execSimple(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	results, err := conn.PgConn().Exec(context.Background(), sql).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if res.Err != nil {
			t.Fatal(res.Err)
		}
	}
}
