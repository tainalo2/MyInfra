// Package siwc implements "Sign in with ChatGPT" (OpenAI, open-source token
// sharing flow): Authorization Code + PKCE + OIDC with dynamic client
// registration, ID-token verification and refresh-token rotation.
//
// Written from OpenAI's public documentation
// (developers.openai.com/siwc/token-sharing-open-source). It has NOT been run
// against the real OpenAI service: the tests use a local fake.
package siwc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DynamicClientID = "dynamic_agent_client"
	ScopeUse        = "chatgpt.tokens.use.direct"
	defaultIssuer   = "https://auth.openai.com"
	resource        = "https://api.openai.com/v1"
	pendingTTL      = 10 * time.Minute
	refreshMargin   = 5 * time.Minute
)

// Config holds everything that varies per instance. Nothing sensitive is hardcoded.
type Config struct {
	Issuer      string // default https://auth.openai.com
	AppName     string // agent_name_hint, same for all installations
	HostID      string // stable ext_agent_host_id of this instance
	RedirectURI string // must be http://127.0.0.1:<port>/auth/callback per OpenAI docs
	HTTP        *http.Client
	Now         func() time.Time
}

// Credential is one registration: issued client_id bound to a verified identity.
type Credential struct {
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	User         string    `json:"myinfra_user,omitempty"` // MyInfra account the verified sub is bound to
	Email        string    `json:"email,omitempty"`
	HostID       string    `json:"ext_agent_host_id"`
	IDToken      string    `json:"id_token"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	SavedAt      time.Time `json:"saved_at"`
}

func (c Credential) HasUseScope() bool {
	for _, s := range c.Scopes {
		if s == ScopeUse {
			return true
		}
	}
	return false
}

type pending struct {
	verifier, nonce, clientID string
	hint                      *Credential // reauthorization of a known registration
	created                   time.Time
}

// Client runs the flow. Safe for concurrent use.
type Client struct {
	cfg  Config
	mu   sync.Mutex
	wait map[string]pending // by state
	jwks struct {
		keys map[string]*rsa.PublicKey
		at   time.Time
	}
}

func New(cfg Config) (*Client, error) {
	if cfg.Issuer == "" {
		cfg.Issuer = defaultIssuer
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.AppName == "" || cfg.HostID == "" || cfg.RedirectURI == "" {
		return nil, errors.New("siwc: AppName, HostID and RedirectURI are required")
	}
	u, err := url.Parse(cfg.RedirectURI)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/auth/callback" {
		return nil, errors.New("siwc: RedirectURI must be http://127.0.0.1:<port>/auth/callback (OpenAI requirement)")
	}
	return &Client{cfg: cfg, wait: map[string]pending{}}, nil
}

func rnd(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Start returns the URL to open in the browser. With known == nil it starts a
// first-time dynamic registration; otherwise it reauthorizes that registration.
func (c *Client) Start(known *Credential) string {
	st, ver, nonce := rnd(24), rnd(48), rnd(24)
	sum := sha256.Sum256([]byte(ver))
	p := pending{verifier: ver, nonce: nonce, clientID: DynamicClientID, hint: known, created: c.cfg.Now()}
	q := url.Values{
		"response_type":         {"code"},
		"redirect_uri":          {c.cfg.RedirectURI},
		"scope":                 {"openid profile email offline_access resource.invoke " + ScopeUse},
		"resource":              {resource},
		"state":                 {st},
		"nonce":                 {nonce},
		"code_challenge_method": {"S256"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"ext_agent_host_id":     {c.cfg.HostID},
	}
	if known == nil {
		q.Set("client_id", DynamicClientID)
		q.Set("agent_name_hint", c.cfg.AppName)
	} else {
		p.clientID = known.ClientID
		q.Set("client_id", known.ClientID)
		if known.IDToken != "" {
			q.Set("id_token_hint", known.IDToken)
		}
		if known.Email != "" {
			q.Set("login_hint", known.Email)
		}
	}
	c.mu.Lock()
	for k, v := range c.wait { // drop expired attempts
		if c.cfg.Now().Sub(v.created) > pendingTTL {
			delete(c.wait, k)
		}
	}
	c.wait[st] = p
	c.mu.Unlock()
	return c.cfg.Issuer + "/api/accounts/authorize?" + q.Encode()
}

// Error carries a diagnosable step name; messages never contain tokens.
type Error struct{ Step, Msg string }

func (e *Error) Error() string            { return e.Step + ": " + e.Msg }
func fail(step, f string, a ...any) error { return &Error{step, fmt.Sprintf(f, a...)} }

// Complete handles the callback query. Nothing is returned (and so nothing can
// be persisted) unless every check passes: partial registrations are impossible.
func (c *Client) Complete(ctx context.Context, q url.Values) (*Credential, error) {
	c.mu.Lock()
	p, ok := c.wait[q.Get("state")]
	delete(c.wait, q.Get("state"))
	c.mu.Unlock()
	if !ok || c.cfg.Now().Sub(p.created) > pendingTTL {
		return nil, fail("state", "unknown or expired authorization attempt")
	}
	if e := q.Get("error"); e != "" {
		return nil, fail("authorize", "OpenAI returned %q (%s)", e, q.Get("error_description"))
	}
	clientID := q.Get("client_id")
	switch {
	case p.hint == nil && (clientID == "" || clientID == DynamicClientID):
		return nil, fail("registration", "callback carries no issued client_id: registration incomplete")
	case p.hint != nil && clientID != "" && clientID != p.hint.ClientID:
		return nil, fail("registration", "callback client_id differs from the registered one")
	case p.hint != nil:
		clientID = p.hint.ClientID
	}
	code := q.Get("code")
	if code == "" {
		return nil, fail("authorize", "no code in callback")
	}
	tok, err := c.token(ctx, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"code_verifier": {p.verifier}, "redirect_uri": {c.cfg.RedirectURI}, "resource": {resource},
	})
	if err != nil {
		return nil, err
	}
	claims, err := c.verifyID(ctx, tok.IDToken, clientID, p.nonce)
	if err != nil {
		return nil, err
	}
	if p.hint != nil && p.hint.Subject != claims.Sub {
		return nil, fail("identity", "signed-in account differs from the selected registration")
	}
	cred := c.credential(clientID, claims.Sub, claims.Email, tok)
	if !cred.HasUseScope() {
		return &cred, fail("scope", "granted scopes lack %s: ChatGPT plan usage is not enabled", ScopeUse)
	}
	return &cred, nil
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (c *Client) credential(clientID, sub, email string, t tokenResp) Credential {
	now := c.cfg.Now()
	return Credential{ClientID: clientID, Subject: sub, Email: email, HostID: c.cfg.HostID,
		IDToken: t.IDToken, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken,
		Scopes: strings.Fields(t.Scope), ExpiresAt: now.Add(time.Duration(t.ExpiresIn) * time.Second), SavedAt: now}
}

func (c *Client) token(ctx context.Context, form url.Values) (tokenResp, error) {
	var t tokenResp
	req, _ := http.NewRequestWithContext(ctx, "POST", c.cfg.Issuer+"/api/accounts/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return t, fail("token", "OpenAI unreachable: %v", scrubURLErr(err))
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != 200 {
		var e struct{ Error string }
		json.Unmarshal(b, &e)
		return t, fail("token", "HTTP %d %s", res.StatusCode, e.Error) // body not echoed: may hold secrets
	}
	if json.Unmarshal(b, &t) != nil || t.AccessToken == "" || t.IDToken == "" && form.Get("grant_type") == "authorization_code" {
		return t, fail("token", "malformed token response")
	}
	return t, nil
}

func scrubURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Refresh rotates the credential when it expires within refreshMargin. The new
// access token, expiry, scopes and refresh token replace the old ones together.
func (c *Client) Refresh(ctx context.Context, cred Credential) (Credential, bool, error) {
	if c.cfg.Now().Add(refreshMargin).Before(cred.ExpiresAt) {
		return cred, false, nil
	}
	if cred.RefreshToken == "" {
		return cred, false, fail("refresh", "no refresh token: sign in again")
	}
	t, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {cred.ClientID},
		"refresh_token": {cred.RefreshToken}, "resource": {resource}})
	if err != nil {
		return cred, false, err
	}
	n := c.credential(cred.ClientID, cred.Subject, cred.Email, t)
	if t.RefreshToken == "" {
		n.RefreshToken = cred.RefreshToken
	}
	if t.IDToken == "" {
		n.IDToken = cred.IDToken
	}
	if len(n.Scopes) == 0 {
		n.Scopes = cred.Scopes
	}
	return n, true, nil
}

type idClaims struct {
	Iss, Sub, Email, Nonce string
	Aud                    json.RawMessage
	Exp                    int64
}

func (c *Client) verifyID(ctx context.Context, tok, clientID, nonce string) (idClaims, error) {
	var cl idClaims
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return cl, fail("id_token", "not a JWT")
	}
	dec := func(s string) []byte { b, _ := base64.RawURLEncoding.DecodeString(s); return b }
	var h struct{ Alg, Kid string }
	if json.Unmarshal(dec(parts[0]), &h) != nil || h.Alg != "RS256" {
		return cl, fail("id_token", "unsupported algorithm")
	}
	key, err := c.key(ctx, h.Kid)
	if err != nil {
		return cl, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], dec(parts[2])) != nil {
		return cl, fail("id_token", "bad signature")
	}
	if json.Unmarshal(dec(parts[1]), &cl) != nil {
		return cl, fail("id_token", "bad claims")
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(dec(parts[1]), &raw)
	json.Unmarshal(raw["iss"], &cl.Iss)
	json.Unmarshal(raw["sub"], &cl.Sub)
	json.Unmarshal(raw["email"], &cl.Email)
	json.Unmarshal(raw["nonce"], &cl.Nonce)
	json.Unmarshal(raw["exp"], &cl.Exp)
	var aud any
	json.Unmarshal(raw["aud"], &aud)
	okAud := false
	switch a := aud.(type) {
	case string:
		okAud = a == clientID
	case []any:
		for _, v := range a {
			okAud = okAud || v == clientID
		}
	}
	switch {
	case cl.Iss != c.cfg.Issuer:
		return cl, fail("id_token", "wrong issuer")
	case !okAud:
		return cl, fail("id_token", "audience is not the issued client_id")
	case cl.Exp <= c.cfg.Now().Unix():
		return cl, fail("id_token", "expired")
	case cl.Nonce != nonce:
		return cl, fail("id_token", "nonce mismatch")
	case cl.Sub == "":
		return cl, fail("id_token", "no subject")
	}
	return cl, nil
}

func (c *Client) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	k := c.jwks.keys[kid]
	fresh := c.cfg.Now().Sub(c.jwks.at) < time.Hour
	c.mu.Unlock()
	if k != nil && fresh {
		return k, nil
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", c.cfg.Issuer+"/.well-known/jwks.json", nil)
	res, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return nil, fail("jwks", "cannot fetch OpenAI JWKS: %v", scrubURLErr(err))
	}
	defer res.Body.Close()
	var set struct {
		Keys []struct{ Kid, Kty, N, E string }
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&set) != nil {
		return nil, fail("jwks", "HTTP %d", res.StatusCode)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, j := range set.Keys {
		n, e := new(big.Int), 0
		nb, _ := base64.RawURLEncoding.DecodeString(j.N)
		eb, _ := base64.RawURLEncoding.DecodeString(j.E)
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		if j.Kty == "RSA" && len(nb) > 0 && e > 0 {
			keys[j.Kid] = &rsa.PublicKey{N: n.SetBytes(nb), E: e}
		}
	}
	c.mu.Lock()
	c.jwks.keys, c.jwks.at = keys, c.cfg.Now()
	c.mu.Unlock()
	if keys[kid] == nil {
		return nil, fail("jwks", "signing key %q not published", kid)
	}
	return keys[kid], nil
}
