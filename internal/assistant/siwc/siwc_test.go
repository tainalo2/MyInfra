package siwc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fake struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	scope   string
	issued  string
	nonce   *string
	sub     string
	refresh int
	badSig  bool
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (f *fake) idToken(aud, nonce string, exp int64) string {
	h := b64([]byte(`{"alg":"RS256","kid":"k1"}`))
	c, _ := json.Marshal(map[string]any{"iss": f.srv.URL, "sub": f.sub, "email": "u@example.com", "aud": aud, "nonce": nonce, "exp": exp})
	in := h + "." + b64(c)
	sum := sha256.Sum256([]byte(in))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if f.badSig {
		sig[0] ^= 1
	}
	return in + "." + b64(sig)
}

func newFake(t *testing.T, now func() time.Time) (*fake, *Client) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fake{key: k, scope: "openid email offline_access resource.invoke " + ScopeUse, issued: "oaiapp_test", sub: "sub-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kid": "k1", "kty": "RSA",
			"n": b64(k.N.Bytes()), "e": b64([]byte{1, 0, 1})}}})
	})
	mux.HandleFunc("/api/accounts/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_id") != f.issued {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		out := map[string]any{"access_token": "at-" + r.Form.Get("grant_type"), "token_type": "Bearer", "expires_in": 3600, "scope": f.scope, "refresh_token": "rt-new"}
		if r.Form.Get("grant_type") == "authorization_code" {
			if r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") == "" {
				w.WriteHeader(400)
				return
			}
			out["id_token"] = f.idToken(f.issued, *f.nonce, now().Add(time.Hour).Unix())
		} else {
			f.refresh++
		}
		json.NewEncoder(w).Encode(out)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	c, err := New(Config{Issuer: f.srv.URL, AppName: "MyInfra", HostID: "urn:uuid:h", RedirectURI: "http://127.0.0.1:1455/auth/callback", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

// start runs Start and returns the callback query a browser would deliver.
func start(t *testing.T, f *fake, c *Client, known *Credential) url.Values {
	u, _ := url.Parse(c.Start(known))
	q := u.Query()
	if known == nil && (q.Get("client_id") != DynamicClientID || q.Get("agent_name_hint") != "MyInfra") {
		t.Fatalf("registration params: %v", q)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("ext_agent_host_id") == "" || q.Get("resource") == "" {
		t.Fatalf("params: %v", q)
	}
	c.mu.Lock()
	f.nonce = new(string)
	*f.nonce = c.wait[q.Get("state")].nonce
	c.mu.Unlock()
	cb := url.Values{"code": {"c"}, "state": {q.Get("state")}, "client_id": {f.issued}}
	return cb
}

func TestRegistrationHappyPath(t *testing.T) {
	now := time.Now
	f, c := newFake(t, now)
	cred, err := c.Complete(context.Background(), start(t, f, c, nil))
	if err != nil {
		t.Fatal(err)
	}
	if cred.ClientID != "oaiapp_test" || cred.Subject != "sub-1" || cred.RefreshToken == "" || !cred.HasUseScope() {
		t.Fatalf("%+v", cred)
	}
}

func TestRejections(t *testing.T) {
	ctx := context.Background()
	f, c := newFake(t, time.Now)
	if _, err := c.Complete(ctx, url.Values{"state": {"nope"}, "code": {"c"}}); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatal("unknown state accepted")
	}
	q := start(t, f, c, nil)
	q.Del("client_id")
	if _, err := c.Complete(ctx, q); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("missing client_id: %v", err)
	}
	f.badSig = true
	if _, err := c.Complete(ctx, start(t, f, c, nil)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("bad signature: %v", err)
	}
	f.badSig = false
	f.scope = "openid email"
	cred, err := c.Complete(ctx, start(t, f, c, nil))
	if err == nil || !strings.Contains(err.Error(), ScopeUse) || cred == nil {
		t.Fatalf("missing scope must error: %v", err)
	}
	f.scope = "openid " + ScopeUse
	q = start(t, f, c, nil)
	*f.nonce = "other"
	if _, err := c.Complete(ctx, q); err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("nonce: %v", err)
	}
	// replay: state is single use
	q = start(t, f, c, nil)
	c.Complete(ctx, q)
	if _, err := c.Complete(ctx, q); err == nil {
		t.Fatal("state replay accepted")
	}
}

func TestReauthKeepsIdentity(t *testing.T) {
	ctx := context.Background()
	f, c := newFake(t, time.Now)
	cred, _ := c.Complete(ctx, start(t, f, c, nil))
	if _, err := c.Complete(ctx, start(t, f, c, cred)); err != nil {
		t.Fatal(err)
	}
	f.sub = "someone-else"
	if _, err := c.Complete(ctx, start(t, f, c, cred)); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("other account must be rejected: %v", err)
	}
}

func TestRefreshWithMockedClock(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	f, c := newFake(t, func() time.Time { return clock })
	cred, _ := c.Complete(ctx, start(t, f, c, nil))
	if _, did, _ := c.Refresh(ctx, *cred); did || f.refresh != 0 {
		t.Fatal("refreshed too early")
	}
	clock = clock.Add(56 * time.Minute) // inside the 5 min margin of a 1 h token
	n, did, err := c.Refresh(ctx, *cred)
	if err != nil || !did || f.refresh != 1 {
		t.Fatalf("did=%v err=%v", did, err)
	}
	if n.AccessToken == cred.AccessToken || !n.ExpiresAt.After(cred.ExpiresAt) || n.ClientID != cred.ClientID || n.Subject != cred.Subject {
		t.Fatalf("not rotated together: %+v", n)
	}
}

func TestConfigRejectsNonLoopback(t *testing.T) {
	if _, err := New(Config{AppName: "a", HostID: "h", RedirectURI: "https://myinfra.example/auth/callback"}); err == nil {
		t.Fatal("non-loopback accepted")
	}
}
