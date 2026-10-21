package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"myinfra/internal/store"
)

func TestSyncYieldRequiresPeerToken(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{Epoch: 1})
	req := httptest.NewRequest(http.MethodPost, "/api/sync/yield", bytes.NewBufferString(`{"epoch":2}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.syncYield(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sans jeton : %d", rec.Code)
	}
}

func TestSyncSnapshotRejectsWrongToken(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{Epoch: 1})
	req := httptest.NewRequest(http.MethodPost, "/api/sync/snapshot", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.syncSnapshot(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("jeton faux : %d", rec.Code)
	}
}
