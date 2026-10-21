package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"myinfra/internal/store"
)

// leadAPI opens a copy on a fresh database, with a real master key so that
// the copies may sync.
func leadAPI(t *testing.T, replica bool, role store.CopyRole) (*API, chan struct{}) {
	t.Helper()
	ctx := context.Background()
	var admin *pgx.Conn
	var user string
	for _, dsn := range []string{
		"postgres://127.0.0.1:54329/postgres?sslmode=disable",
		"postgres://myinfra@127.0.0.1:54329/postgres?sslmode=disable",
	} {
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			continue
		}
		if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&user); err != nil {
			conn.Close(ctx)
			continue
		}
		admin = conn
		break
	}
	if admin == nil {
		t.Skip("postgres indisponible")
	}
	const name = "myinfra_lead_api"
	drop := func() {
		_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name)
	}
	drop()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, "postgres://"+user+"@127.0.0.1:54329/"+name+"?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		drop()
		admin.Close(ctx)
	})
	if err := db.SetCopyRole(ctx, role); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte("test lead key"))
	restarted := make(chan struct{}, 1)
	roleRestartDelay = 0
	a := &API{Store: db, Master: key[:], Replica: replica, Restart: func() { restarted <- struct{}{} }}
	return a, restarted
}

func termRequest(a *API, handler http.HandlerFunc, epoch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/sync/x", bytes.NewBufferString(`{"epoch":`+epoch+`}`))
	req.Header.Set("Authorization", "Bearer "+syncToken(a.Master))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func waitRestart(t *testing.T, restarted chan struct{}) {
	t.Helper()
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("la copie ne redémarre pas")
	}
}

// A main copy asked to yield by a later term follows the new one; an
// earlier or equal term is refused.
func TestYieldNeedsALaterTerm(t *testing.T) {
	a, restarted := leadAPI(t, false, store.CopyRole{Epoch: 4})
	if rec := termRequest(a, a.syncYield, "4"); rec.Code != http.StatusConflict {
		t.Fatalf("mandat égal accepté : %d", rec.Code)
	}
	if rec := termRequest(a, a.syncYield, "5"); rec.Code != http.StatusOK {
		t.Fatalf("cession refusée : %d %s", rec.Code, rec.Body)
	}
	role, _ := a.Store.CopyRole(context.Background())
	if !role.Replica || role.Epoch != 5 || role.MainMethod != "local" {
		t.Fatalf("rôle inattendu : %+v", role)
	}
	waitRestart(t, restarted)
}

// A replica handed the lead drives, under the new term.
func TestHandedLeadDrives(t *testing.T) {
	a, restarted := leadAPI(t, true, store.CopyRole{Replica: true, MainMethod: "local", Epoch: 5})
	if rec := termRequest(a, a.syncLead, "6"); rec.Code != http.StatusOK {
		t.Fatalf("reprise refusée : %d %s", rec.Code, rec.Body)
	}
	role, _ := a.Store.CopyRole(context.Background())
	if role.Replica || role.Epoch != 6 {
		t.Fatalf("rôle inattendu : %+v", role)
	}
	waitRestart(t, restarted)
}

// Without the sync key, no copy changes the role of another.
func TestTermNeedsTheSyncKey(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	req := httptest.NewRequest(http.MethodPost, "/api/sync/yield", bytes.NewBufferString(`{"epoch":9}`))
	req.Header.Set("Authorization", "Bearer faux")
	rec := httptest.NewRecorder()
	a.syncYield(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cession sans clé : %d", rec.Code)
	}
	if role, _ := a.Store.CopyRole(context.Background()); role.Replica {
		t.Fatal("le rôle a changé")
	}
}

// A main copy that crashed and comes back finds, during the sync, a later
// term: it stops driving and follows the copy that took over.
func TestFenceStepsAsideForALaterTerm(t *testing.T) {
	a, restarted := leadAPI(t, false, store.CopyRole{Epoch: 2})
	a.fence(context.Background(), "https://myinfra.exemple", peerState{Driving: true, Epoch: 3})
	role, _ := a.Store.CopyRole(context.Background())
	if !role.Replica || role.Epoch != 3 || role.MainURL != "https://myinfra.exemple" {
		t.Fatalf("rôle inattendu : %+v", role)
	}
	waitRestart(t, restarted)
}

// Two copies that both drive under the same term are left as they are: the
// copies of before the terms.
func TestFenceKeepsEqualTerms(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{Epoch: 2})
	a.fence(context.Background(), "https://myinfra.exemple", peerState{Driving: true, Epoch: 2})
	if role, _ := a.Store.CopyRole(context.Background()); role.Replica {
		t.Fatal("la copie a cédé pour un mandat égal")
	}
}

// A replica whose main copy is gone takes the lead when forced, under a
// later term.
func TestForcedLeadWithoutMain(t *testing.T) {
	a, restarted := leadAPI(t, true, store.CopyRole{Replica: true, MainMethod: "pod", MainURL: "https://myinfra-absente.invalid", Epoch: 1})
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/copies/lead", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.takeLead(rec, req)
		return rec
	}
	if rec := call(`{}`); rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("main_unreachable")) {
		t.Fatalf("reprise sans réponse acceptée : %d %s", rec.Code, rec.Body)
	}
	if rec := call(`{"force":true}`); rec.Code != http.StatusAccepted {
		t.Fatalf("reprise forcée refusée : %d %s", rec.Code, rec.Body)
	}
	role, _ := a.Store.CopyRole(context.Background())
	if role.Replica || role.Epoch != 2 || role.MainURL != "https://myinfra-absente.invalid" {
		t.Fatalf("rôle inattendu : %+v", role)
	}
	waitRestart(t, restarted)
}

// A main copy that answers decides the takeover: an older version, which
// cannot step aside, blocks it even forced; a current one yields.
func TestLeadFollowsTheMainAnswer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		yield func(http.ResponseWriter)
		code  int
		want  string
	}{
		{"ancienne version", func(w http.ResponseWriter) { w.WriteHeader(http.StatusMethodNotAllowed) }, http.StatusConflict, "main_outdated"},
		{"page à la place", func(w http.ResponseWriter) { _, _ = w.Write([]byte("<!doctype html>")) }, http.StatusConflict, "main_outdated"},
		{"version à jour", func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"status":"ok"}`)) }, http.StatusAccepted, `"reached":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/sync/state":
					_, _ = w.Write([]byte(`{"instanceId":"principale","hash":"x","epoch":3}`))
				case "/api/sync/yield":
					tc.yield(w)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer main.Close()
			a, _ := leadAPI(t, true, store.CopyRole{Replica: true, MainMethod: "pod", MainURL: main.URL, Epoch: 1})
			req := httptest.NewRequest(http.MethodPost, "/api/copies/lead", bytes.NewBufferString(`{"force":true}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			a.takeLead(rec, req)
			if rec.Code != tc.code || !bytes.Contains(rec.Body.Bytes(), []byte(tc.want)) {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			role, _ := a.Store.CopyRole(context.Background())
			if tc.code == http.StatusAccepted && (role.Replica || role.Epoch != 4) {
				t.Fatalf("rôle inattendu : %+v", role)
			}
			if tc.code != http.StatusAccepted && !role.Replica {
				t.Fatal("la copie a pris la main à côté d'une principale qui pilote")
			}
		})
	}
}
