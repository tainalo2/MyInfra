package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStopMyInfraOnlyForTheApp(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	a.stopMyInfra(rec, httptest.NewRequest(http.MethodPost, "/api/myinfra/stop", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("sans Stop : %d", rec.Code)
	}

	stopped := make(chan struct{})
	a.Stop = func() { close(stopped) }
	rec = httptest.NewRecorder()
	a.stopMyInfra(rec, httptest.NewRequest(http.MethodPost, "/api/myinfra/stop", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("avec Stop : %d", rec.Code)
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("MyInfra ne s'arrête pas")
	}
}
