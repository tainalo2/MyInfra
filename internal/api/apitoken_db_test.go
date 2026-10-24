package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"myinfra/internal/store"
)

// call sends a request through the whole router, as a client would.
func call(t *testing.T, h http.Handler, method, path, bearer, cookie string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// tokenFixture opens a database with an admin (the owner) and a deploy
// login, each with a session.
func tokenFixture(t *testing.T) (*API, http.Handler) {
	t.Helper()
	a, _ := leadAPI(t, false, store.CopyRole{})
	ctx := t.Context()
	if err := a.Store.CreateFirstUser(ctx, "owner", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateUserAccount(ctx, "dep", "dep", store.RoleDeploy, "hash"); err != nil {
		t.Fatal(err)
	}
	for id, session := range map[string]string{"owner": "sess-owner", "dep": "sess-dep"} {
		if err := a.Store.CreateSession(ctx, hashToken(session), id, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	return a, a.Handler()
}

func makeToken(t *testing.T, h http.Handler, session, role string, days int) apiTokenView {
	t.Helper()
	rec := call(t, h, http.MethodPost, "/api/mcp/tokens", "", session, map[string]any{"name": "laptop " + role, "role": role, "days": days})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var view apiTokenView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(view.Token, tokenPrefix) {
		t.Fatal(view.Token)
	}
	return view
}

func TestAccessTokenOpensTheAPIWithItsRole(t *testing.T) {
	a, h := tokenFixture(t)
	read := makeToken(t, h, "sess-owner", store.RoleRead, 30)

	if rec := call(t, h, http.MethodGet, "/api/clusters", read.Token, "", nil); rec.Code != http.StatusOK {
		t.Fatal("lecture", rec.Code, rec.Body.String())
	}
	// A read token of an admin cannot create, as a read login could not.
	if rec := call(t, h, http.MethodPost, "/api/clusters", read.Token, "", map[string]string{"name": "SW"}); rec.Code != http.StatusForbidden {
		t.Fatal("écriture", rec.Code, rec.Body.String())
	}
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0)
	if rec := call(t, h, http.MethodPost, "/api/clusters", deploy.Token, "", map[string]string{"name": "SW"}); rec.Code != http.StatusCreated {
		t.Fatal("déploiement", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodGet, "/api/users", deploy.Token, "", nil); rec.Code != http.StatusForbidden {
		t.Fatal("admin", rec.Code, rec.Body.String())
	}

	// The event says the token did it.
	rec := call(t, h, http.MethodGet, "/api/events?source=api", "", "sess-owner", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"actor":"laptop deploy"`) {
		t.Fatal(rec.Code, rec.Body.String())
	}

	if rec := call(t, h, http.MethodGet, "/api/clusters", tokenPrefix+"faux", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatal("faux jeton", rec.Code)
	}
	if rec := call(t, h, http.MethodDelete, "/api/mcp/tokens/"+read.ID, "", "sess-owner", nil); rec.Code != http.StatusNoContent {
		t.Fatal("révocation", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodGet, "/api/clusters", read.Token, "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatal("jeton révoqué", rec.Code)
	}
	_ = a
}

func TestAccessTokenNeverExceedsItsPerson(t *testing.T) {
	a, h := tokenFixture(t)
	if rec := call(t, h, http.MethodPost, "/api/mcp/tokens", "", "sess-dep", map[string]any{"name": "x", "role": store.RoleAdmin}); rec.Code != http.StatusForbidden {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// An admin token left to a login since made deploy acts as deploy.
	secret := tokenPrefix + "kept-from-before"
	if err := a.Store.CreateAPIToken(t.Context(), store.APIToken{ID: "old", UserID: "dep", Name: "old", Role: store.RoleAdmin}, hashToken(secret)); err != nil {
		t.Fatal(err)
	}
	if rec := call(t, h, http.MethodGet, "/api/users", secret, "", nil); rec.Code != http.StatusForbidden {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// A login sees only its tokens; the admin sees everyone's.
	makeToken(t, h, "sess-dep", store.RoleRead, 0)
	makeToken(t, h, "sess-owner", store.RoleRead, 0)
	var mine, all []apiTokenView
	_ = json.Unmarshal(call(t, h, http.MethodGet, "/api/mcp/tokens", "", "sess-dep", nil).Body.Bytes(), &mine)
	_ = json.Unmarshal(call(t, h, http.MethodGet, "/api/mcp/tokens", "", "sess-owner", nil).Body.Bytes(), &all)
	if len(mine) != 2 || len(all) != 3 {
		t.Fatal(len(mine), len(all))
	}
	for _, row := range mine {
		if row.Token != "" {
			t.Fatal("secret relu")
		}
	}
}

func TestAccessTokenCannotManageTokens(t *testing.T) {
	_, h := tokenFixture(t)
	admin := makeToken(t, h, "sess-owner", store.RoleAdmin, 0)
	for _, rec := range []*httptest.ResponseRecorder{
		call(t, h, http.MethodGet, "/api/mcp/tokens", admin.Token, "", nil),
		call(t, h, http.MethodPost, "/api/mcp/tokens", admin.Token, "", map[string]any{"name": "x", "role": store.RoleRead}),
		call(t, h, http.MethodDelete, "/api/mcp/tokens/"+admin.ID, admin.Token, "", nil),
	} {
		if rec.Code != http.StatusForbidden {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
}

func TestExpiredAccessTokenIsRefused(t *testing.T) {
	a, h := tokenFixture(t)
	past := time.Now().Add(-time.Minute)
	secret := tokenPrefix + "expired"
	if err := a.Store.CreateAPIToken(t.Context(), store.APIToken{ID: "exp", UserID: "owner", Name: "exp", Role: store.RoleRead, ExpiresAt: &past}, hashToken(secret)); err != nil {
		t.Fatal(err)
	}
	if rec := call(t, h, http.MethodGet, "/api/clusters", secret, "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code)
	}
}
