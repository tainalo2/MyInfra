package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"myinfra/internal/store"
)

func TestDeployRoleCredentialsAndTransfer(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	handler := a.Handler()

	setup := httptest.NewRecorder()
	handler.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"password":"hunter2pass"}`)))
	admin := setup.Result().Cookies()[0]

	onboard := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding", nil)
	req.AddCookie(admin)
	handler.ServeHTTP(onboard, req)

	create := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewBufferString(`{"login":"deployer","password":"deploypass","role":"deploy"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(create, req)
	if create.Code != http.StatusCreated {
		t.Fatalf("create deploy %d %s", create.Code, create.Body.String())
	}

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/session", bytes.NewBufferString(`{"login":"deployer","password":"deploypass"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
	deployer := login.Result().Cookies()[0]

	list := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	req.AddCookie(deployer)
	handler.ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list secrets %d", list.Code)
	}

	forbidden := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/secrets/tokens", `{"name":"t","kind":"provider_token","provider":"hetzner","token":"x"}`},
		{http.MethodDelete, "/api/secrets/deadbeef", ""},
		{http.MethodGet, "/api/secrets/deadbeef/private", ""},
		{http.MethodPost, "/api/app-secrets", `{"name":"s","entries":{"k":"v"}}`},
		{http.MethodPost, "/api/dns/records/publish", `{"zone":"z","name":"a"}`},
		{http.MethodPost, "/api/images/fetch", `{"url":"http://example.com/x.tar"}`},
		{http.MethodPost, "/api/transfer", `{}`},
		{http.MethodGet, "/api/applications/deadbeef/variables/SECRET", ""},
		{http.MethodGet, "/api/applications/deadbeef/backups/db", ""},
	}
	for _, call := range forbidden {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(call.method, call.path, bytes.NewBufferString(call.body))
		req.AddCookie(deployer)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: want 403 got %d %s", call.method, call.path, rec.Code, rec.Body.String())
		}
	}
}
