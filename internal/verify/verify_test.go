package verify

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTestTokenSkipsNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("le réseau ne doit pas être appelé")
	}))
	defer srv.Close()
	c := Client{AllowTest: true, HetznerBase: srv.URL, HTTP: srv.Client()}
	if err := c.Hetzner(context.Background(), "test-hetzner-atelier-0001"); err != nil {
		t.Fatal(err)
	}
	if err := c.GitHub(context.Background(), "test-github"); err != nil {
		t.Fatal(err)
	}
	if err := c.GitLab(context.Background(), "test-gitlab"); err != nil {
		t.Fatal(err)
	}
	c.DigitalOceanBase = srv.URL
	c.InfomaniakBase = srv.URL
	if err := c.DigitalOcean(context.Background(), "test-digitalocean"); err != nil {
		t.Fatal(err)
	}
	if err := c.Exoscale(context.Background(), "test-key", "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := c.Gcore(context.Background(), "test-gcore"); err != nil {
		t.Fatal(err)
	}
	if err := c.Kamatera(context.Background(), "test-id", "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := c.Ionos(context.Background(), "test-ionos"); err != nil {
		t.Fatal(err)
	}
	if err := c.Infomaniak(context.Background(), "test-id", "test-secret"); err != nil {
		t.Fatal(err)
	}
}

func TestScalewayUsesInstanceList(t *testing.T) {
	var path, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		token = r.Header.Get("X-Auth-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), ScalewayBase: srv.URL}
	if err := c.Scaleway(context.Background(), "secret-key"); err != nil {
		t.Fatal(err)
	}
	if path != "/instance/v1/zones/fr-par-1/servers" {
		t.Fatalf("path %s", path)
	}
	if token != "secret-key" {
		t.Fatalf("token %s", token)
	}
}

func TestHetznerRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), HetznerBase: srv.URL}
	if err := c.Hetzner(context.Background(), "mauvais-jeton"); err == nil {
		t.Fatal("un jeton refusé doit bloquer la création")
	}
}

func TestHetznerOK(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := Client{HTTP: srv.Client(), HetznerBase: srv.URL}
	if err := c.Hetzner(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer tok" {
		t.Fatalf("auth %q", got)
	}
}

func TestOVHSignature(t *testing.T) {
	var signature, stamp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1.0/auth/time":
			fmt.Fprint(w, time.Now().Unix())
		case "/1.0/me":
			signature = r.Header.Get("X-Ovh-Signature")
			stamp = r.Header.Get("X-Ovh-Timestamp")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := Client{HTTP: srv.Client(), OVHBase: srv.URL + "/1.0"}
	err := c.OVH(context.Background(), OVH{
		ApplicationKey:    "ak",
		ApplicationSecret: "as",
		ConsumerKey:       "ck",
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum([]byte("as+ck+GET+" + srv.URL + "/1.0/me++" + stamp))
	want := fmt.Sprintf("$1$%x", sum)
	if signature != want {
		t.Fatalf("signature %s want %s", signature, want)
	}
	if _, err := strconv.ParseInt(stamp, 10, 64); err != nil {
		t.Fatal(err)
	}
}

func TestOVHRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1.0/auth/time" {
			fmt.Fprint(w, time.Now().Unix())
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), OVHBase: srv.URL + "/1.0"}
	if err := c.OVH(context.Background(), OVH{ApplicationKey: "a", ApplicationSecret: "b", ConsumerKey: "c"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewProviderProbes(t *testing.T) {
	var path, auth, clientID, secret string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		clientID = r.Header.Get("AuthClientId")
		secret = r.Header.Get("AuthSecret")
		w.WriteHeader(status)
		if r.URL.Path == "/service/servers" {
			w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), ExoscaleBase: srv.URL, GcoreBase: srv.URL, KamateraBase: srv.URL, IonosBase: srv.URL}
	ctx := context.Background()
	if err := c.Gcore(ctx, " gcore-token "); err != nil || path != "/cloud/v1/projects" || auth != "APIKey gcore-token" {
		t.Fatalf("gcore %v %s %s", err, path, auth)
	}
	if err := c.Ionos(ctx, "ionos-token"); err != nil || path != "/cloudapi/v6/datacenters" || auth != "Bearer ionos-token" {
		t.Fatalf("ionos %v %s %s", err, path, auth)
	}
	if err := c.Kamatera(ctx, "id", "secret"); err != nil || path != "/service/servers" || clientID != "id" || secret != "secret" {
		t.Fatalf("kamatera %v %s", err, path)
	}
	if err := c.Exoscale(ctx, "EXOkey", "secret"); err != nil || path != "/v2/zone" || !strings.HasPrefix(auth, "EXO2-HMAC-SHA256 credential=EXOkey,expires=") {
		t.Fatalf("exoscale %v %s %s", err, path, auth)
	}
	status = http.StatusUnauthorized
	if err := c.Gcore(ctx, "bad"); err == nil || err.Error() != "Le jeton a été refusé." {
		t.Fatalf("refus attendu, reçu %v", err)
	}
}

func TestKamateraRefusesErrorObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"code":1,"info":"Invalid clientId or secret"}]}`))
	}))
	defer srv.Close()
	err := (Client{HTTP: srv.Client(), KamateraBase: srv.URL}).Kamatera(context.Background(), "a", "b")
	if err == nil || err.Error() != "Les identifiants ont été refusés." {
		t.Fatalf("refus attendu, reçu %v", err)
	}
}
