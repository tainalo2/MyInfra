package provision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"myinfra/internal/catalog"
)

// Contract tests for provider Exists: same HTTP semantics as Scaleway/Vultr in
// exists_test.go, extended to the providers not yet covered there.
func TestExistsOtherProviders(t *testing.T) {
	ctx := context.Background()
	creds := catalog.Creds{Token: "t"}

	t.Run("hetzner", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/servers/ok":
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()
		c := Client{HetznerBase: srv.URL}
		ok, err := c.Exists(ctx, "hetzner", creds, "ok")
		if err != nil || !ok {
			t.Fatalf("ok: %v %v", ok, err)
		}
		gone, err := c.Exists(ctx, "hetzner", creds, "missing")
		if err != nil || gone {
			t.Fatalf("missing: %v %v", gone, err)
		}
	})

	t.Run("digitalocean", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/droplets/42" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := Client{DigitalOceanBase: srv.URL}
		ok, err := c.Exists(ctx, "digitalocean", creds, "42")
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	})

	t.Run("upcloud", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/1.3/server/u1" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := Client{UpCloudBase: srv.URL}
		ok, err := c.Exists(ctx, "upcloud", creds, "u1")
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	})

	t.Run("akamai", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v4/linode/instances/lin-1" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := Client{AkamaiBase: srv.URL}
		ok, err := c.Exists(ctx, "akamai", creds, "lin-1")
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	})

	t.Run("ionos", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/cloudapi/v6/datacenters/dc-1" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := Client{IonosBase: srv.URL}
		ok, err := c.Exists(ctx, "ionos", creds, "dc-1")
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	})

	t.Run("gcore", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/cloud/v1/instances/eu-frankfurt-1/9/inst-1" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := Client{GcoreBase: srv.URL}
		ok, err := c.Exists(ctx, "gcore", creds, "9:eu-frankfurt-1:inst-1")
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	})
}
