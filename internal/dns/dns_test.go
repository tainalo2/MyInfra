package dns

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJoinBuildsTheHostname(t *testing.T) {
	host, err := Join("App", "Exemple.fr")
	if err != nil || host != "app.exemple.fr" {
		t.Fatal(host, err)
	}
	if _, err := Join("", "exemple.fr"); err == nil {
		t.Fatal("empty subdomain")
	}
	if _, err := Join("app", "localhost"); err == nil {
		t.Fatal("single label zone")
	}
}

func TestCloudflareListsZonesAndWritesTheAddress(t *testing.T) {
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %s", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == "/zones" && r.URL.Query().Get("name") == "":
			fmt.Fprint(w, `{"success":true,"result":[{"id":"z1","name":"exemple.fr"}],"result_info":{"page":1,"total_pages":1}}`)
		case r.URL.Path == "/zones" && r.URL.Query().Get("name") == "exemple.fr":
			fmt.Fprint(w, `{"success":true,"result":[{"id":"z1","name":"exemple.fr"}],"result_info":{"page":1,"total_pages":1}}`)
		case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`)
		case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodPost:
			posts++
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"content":"203.0.113.8"`) || !strings.Contains(string(raw), `"proxied":false`) {
				t.Fatal(string(raw))
			}
			fmt.Fprint(w, `{"success":true,"result":{"id":"r1"}}`)
		default:
			t.Fatalf("%s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	client := Client{CloudflareBase: server.URL, HTTP: server.Client(), Confirm: func(context.Context, string, string, string) error { return nil }}
	zones, err := client.Zones(context.Background(), "cloudflare", Creds{Token: "tok"})
	if err != nil || len(zones) != 1 || zones[0] != "exemple.fr" {
		t.Fatal(zones, err)
	}
	if err := client.EnsureAddress(context.Background(), "cloudflare", Creds{Token: "tok"}, "exemple.fr", "app.exemple.fr", "203.0.113.8"); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatal(posts)
	}
}

func TestOVHRefreshesAfterCreatingTheRecord(t *testing.T) {
	const secret = "app-secret"
	const consumer = "ck"
	var created, refreshed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/time" {
			fmt.Fprint(w, "1700000000")
			return
		}
		body, _ := io.ReadAll(r.Body)
		sum := sha1.Sum([]byte(secret + "+" + consumer + "+" + r.Method + "+" + "http://" + r.Host + r.URL.RequestURI() + "+" + string(body) + "+" + "1700000000"))
		if r.Header.Get("X-Ovh-Signature") != fmt.Sprintf("$1$%x", sum) {
			t.Fatalf("signature %s %s", r.URL.RequestURI(), r.Header.Get("X-Ovh-Signature"))
		}
		switch {
		case r.URL.Path == "/domain/zone" && r.Method == http.MethodGet:
			fmt.Fprint(w, `["exemple.fr","autre.fr"]`)
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.Method == http.MethodGet:
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.Method == http.MethodPost:
			created = true
			if !strings.Contains(string(body), `"subDomain":"app"`) || !strings.Contains(string(body), `"target":"203.0.113.8"`) {
				t.Fatal(string(body))
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/domain/zone/exemple.fr/refresh":
			refreshed = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("%s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	client := Client{OVHBase: server.URL, HTTP: server.Client(), Confirm: func(context.Context, string, string, string) error { return nil }}
	creds := Creds{ApplicationKey: "ak", ApplicationSecret: secret, ConsumerKey: consumer}
	zones, err := client.Zones(context.Background(), "ovh", creds)
	if err != nil || len(zones) != 2 {
		t.Fatal(zones, err)
	}
	if err := client.EnsureAddress(context.Background(), "ovh", creds, "exemple.fr", "app.exemple.fr", "203.0.113.8"); err != nil {
		t.Fatal(err)
	}
	if !created || !refreshed {
		t.Fatal(created, refreshed)
	}
}

func TestOVHDropsTheRecord(t *testing.T) {
	const secret = "app-secret"
	const consumer = "ck"
	var deleted, refreshed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/time" {
			fmt.Fprint(w, "1700000000")
			return
		}
		body, _ := io.ReadAll(r.Body)
		sum := sha1.Sum([]byte(secret + "+" + consumer + "+" + r.Method + "+" + "http://" + r.Host + r.URL.RequestURI() + "+" + string(body) + "+" + "1700000000"))
		if r.Header.Get("X-Ovh-Signature") != fmt.Sprintf("$1$%x", sum) {
			t.Fatalf("signature %s %s", r.URL.RequestURI(), r.Header.Get("X-Ovh-Signature"))
		}
		switch {
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.URL.Query().Get("fieldType") == "A":
			fmt.Fprint(w, `[42]`)
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.URL.Query().Get("fieldType") == "AAAA":
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/domain/zone/exemple.fr/record/42" && r.Method == http.MethodDelete:
			deleted = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/domain/zone/exemple.fr/refresh":
			refreshed = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("%s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	client := Client{OVHBase: server.URL, HTTP: server.Client()}
	err := client.DropAddress(context.Background(), "ovh", Creds{ApplicationKey: "ak", ApplicationSecret: secret, ConsumerKey: consumer}, "exemple.fr", "app.exemple.fr")
	if err != nil || !deleted || !refreshed {
		t.Fatal(err, deleted, refreshed)
	}
}
