package provision

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"myinfra/internal/catalog"
)

// A provider says whether it still has a machine: 404 means gone, any other
// trouble stays an error, never a removal.
func TestExistsReadsTheProviderAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("méthode %s", r.Method)
		}
		switch r.URL.Path {
		case "/instance/v1/zones/fr-par-1/servers/present", "/v2/instances/present":
			_, _ = w.Write([]byte(`{}`))
		case "/instance/v1/zones/fr-par-1/servers/refused":
			w.WriteHeader(http.StatusForbidden)
		case "/v2/instances/down":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := Client{ScalewayBase: srv.URL, VultrBase: srv.URL}
	ctx := context.Background()
	creds := catalog.Creds{Token: "t"}
	for _, tc := range []struct {
		provider, id string
		exists, err  bool
	}{
		{"scaleway", "fr-par-1/present", true, false},
		{"scaleway", "fr-par-1/absent", false, false},
		{"scaleway", "fr-par-1/refused", false, true},
		{"vultr", "present", true, false},
		{"vultr", "absent", false, false},
		{"vultr", "down", false, true},
	} {
		exists, err := c.Exists(ctx, tc.provider, creds, tc.id)
		if exists != tc.exists || (err != nil) != tc.err {
			t.Errorf("%s %s : %v %v", tc.provider, tc.id, exists, err)
		}
	}
	if _, err := c.Exists(ctx, "kamatera", creds, "x"); !errors.Is(err, ErrNoCheck) {
		t.Errorf("kamatera : %v", err)
	}
}
