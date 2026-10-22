package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"myinfra/internal/store"
)

func TestReadOnlyCanBrowseNotMutate(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	handler := a.Handler()

	setup := httptest.NewRecorder()
	handler.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"password":"hunter2pass"}`)))
	if setup.Code != http.StatusOK {
		t.Fatalf("setup %d %s", setup.Code, setup.Body.String())
	}
	admin := setup.Result().Cookies()[0]

	onboard := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding", nil)
	req.AddCookie(admin)
	handler.ServeHTTP(onboard, req)
	if onboard.Code != http.StatusOK {
		t.Fatalf("onboarding %d %s", onboard.Code, onboard.Body.String())
	}

	create := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewBufferString(`{"login":"reader","password":"readerpass","role":"read"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(create, req)
	if create.Code != http.StatusCreated {
		t.Fatalf("create user %d %s", create.Code, create.Body.String())
	}

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/session", bytes.NewBufferString(`{"login":"reader","password":"readerpass"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
	reader := login.Result().Cookies()[0]

	read := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.AddCookie(reader)
	handler.ServeHTTP(read, req)
	if read.Code != http.StatusOK {
		t.Fatalf("list servers %d %s", read.Code, read.Body.String())
	}

	forbidden := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodDelete, "/api/servers/deadbeef", `{"name":"srv"}`},
		{http.MethodPost, "/api/servers", `{"name":"x"}`},
		{http.MethodDelete, "/api/applications/deadbeef", `{"name":"app"}`},
		{http.MethodPost, "/api/applications", `{"name":"x"}`},
		{http.MethodDelete, "/api/clusters/deadbeef", ""},
		{http.MethodPost, "/api/clusters", `{"name":"c"}`},
		{http.MethodPost, "/api/deployments/cancel", `{"kind":"server","id":"x"}`},
		{http.MethodPost, "/api/secrets/tokens", `{"name":"t","token":"x"}`},
		{http.MethodDelete, "/api/secrets/deadbeef", ""},
		{http.MethodPost, "/api/templates", `{"name":"t","body":""}`},
		{http.MethodPost, "/api/catalogue/nope", `{}`},
		{http.MethodGet, "/api/applications/deadbeef/variables/SECRET", ""},
		{http.MethodGet, "/api/applications/deadbeef/backups/db", ""},
	}
	for _, call := range forbidden {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(call.method, call.path, bytes.NewBufferString(call.body))
		req.AddCookie(reader)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: want 403 got %d %s", call.method, call.path, rec.Code, rec.Body.String())
		}
	}

	allowed := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/applications", nil)
	req.AddCookie(reader)
	handler.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("list apps %d %s", allowed.Code, allowed.Body.String())
	}
}
