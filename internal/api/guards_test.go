package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Nothing changes while MyInfra moves: the change would stay behind.
func TestSteadyRefusesDuringATransfer(t *testing.T) {
	a := &API{}
	called := false
	handler := a.steady(func(w http.ResponseWriter, r *http.Request) { called = true })
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/api/servers", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("hors transfert : %v %d", called, rec.Code)
	}
	a.transfer.running = true
	called = false
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/api/servers", nil))
	if called || rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "transfer_running") {
		t.Fatalf("pendant le transfert : %v %d %s", called, rec.Code, rec.Body.String())
	}
}

func TestPersonalMachine(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:8080":       true,
		"http://192.168.1.4:8080":     true,
		"https://myinfra.example.com": false,
		"http://localhost:8080":       true,
	} {
		if got := (&API{PublicURL: url}).onPersonalMachine(); got != want {
			t.Errorf("%s : %v", url, got)
		}
	}
	if !(&API{Local: true, PublicURL: "https://myinfra.example.com"}).onPersonalMachine() {
		t.Error("l'exécutable local")
	}
}
