package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"myinfra/internal/auth"
	"myinfra/internal/store"
)

func TestTOTPPrepareConfirmFlow(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})

	handler := a.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"password":"hunter2pass"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]

	prepare := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/account/totp", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(prepare, req)
	if prepare.Code != http.StatusOK {
		t.Fatalf("prepare %d %s", prepare.Code, prepare.Body.String())
	}
	var prep map[string]string
	if err := json.Unmarshal(prepare.Body.Bytes(), &prep); err != nil {
		t.Fatal(err)
	}
	secret := prep["secret"]
	if secret == "" || prep["uri"] == "" {
		t.Fatalf("prepare body %v", prep)
	}
	if prep["uri"] != auth.ProvisioningURI("MyInfra", "admin", secret) {
		t.Fatalf("uri mismatch: %q", prep["uri"])
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Validate(secret, code) {
		t.Fatal("validate mismatch")
	}

	body, _ := json.Marshal(map[string]string{"secret": secret, "totp": code})
	confirm := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/account/totp/confirm", bytes.NewReader(body))
	req.AddCookie(cookie)
	handler.ServeHTTP(confirm, req)
	if confirm.Code != http.StatusNoContent {
		t.Fatalf("confirm %d %s", confirm.Code, confirm.Body.String())
	}

	session := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(session, req)
	var sess map[string]any
	if err := json.Unmarshal(session.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	if sess["totp"] != true {
		t.Fatalf("session totp %v", sess["totp"])
	}
}

func TestLoginRequiresTOTPWithoutCodeOnSingleUser(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	handler := a.Handler()

	setup := httptest.NewRecorder()
	handler.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"password":"hunter2pass"}`)))
	cookie := setup.Result().Cookies()[0]

	// Active la 2FA sur le seul compte (login vide → chemin mono-utilisateur).
	prepare := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/account/totp", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(prepare, req)
	var prep map[string]string
	if err := json.Unmarshal(prepare.Body.Bytes(), &prep); err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(prep["secret"], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	confirmBody, _ := json.Marshal(map[string]string{"secret": prep["secret"], "totp": code})
	confirm := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/account/totp/confirm", bytes.NewReader(confirmBody))
	req.AddCookie(cookie)
	handler.ServeHTTP(confirm, req)
	if confirm.Code != http.StatusNoContent {
		t.Fatalf("confirm %d", confirm.Code)
	}

	login := httptest.NewRecorder()
	loginBody, _ := json.Marshal(map[string]string{"password": "hunter2pass"})
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/session", bytes.NewReader(loginBody)))
	if login.Code != http.StatusOK {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
	var step map[string]any
	if err := json.Unmarshal(login.Body.Bytes(), &step); err != nil {
		t.Fatal(err)
	}
	if step["totpRequired"] != true || step["challenge"] == "" {
		t.Fatalf("expected totp challenge, got %v", step)
	}
	if login.Result().Cookies() != nil && len(login.Result().Cookies()) > 0 && login.Result().Cookies()[0].Value != "" {
		t.Fatal("session must not start before totp")
	}

	bad := httptest.NewRecorder()
	badBody, _ := json.Marshal(map[string]string{"challenge": step["challenge"].(string), "totp": "000000"})
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/api/session/totp", bytes.NewReader(badBody)))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad totp %d", bad.Code)
	}

	// Le challenge est à usage unique : il faut se reconnecter.
	login2 := httptest.NewRecorder()
	handler.ServeHTTP(login2, httptest.NewRequest(http.MethodPost, "/api/session", bytes.NewReader(loginBody)))
	var step2 map[string]any
	if err := json.Unmarshal(login2.Body.Bytes(), &step2); err != nil {
		t.Fatal(err)
	}
	goodCode, err := totp.GenerateCode(prep["secret"], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	okBody, _ := json.Marshal(map[string]string{"challenge": step2["challenge"].(string), "totp": goodCode})
	ok := httptest.NewRecorder()
	// The password check and the code check can hit two replicas.
	other := &API{Store: a.Store}
	other.Handler().ServeHTTP(ok, httptest.NewRequest(http.MethodPost, "/api/session/totp", bytes.NewReader(okBody)))
	if ok.Code != http.StatusOK {
		t.Fatalf("good totp %d %s", ok.Code, ok.Body.String())
	}
}
