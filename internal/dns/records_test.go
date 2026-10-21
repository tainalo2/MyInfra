package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareRecordsListsTheAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/zones" && r.URL.Query().Get("name") == "exemple.fr":
			fmt.Fprint(w, `{"success":true,"result":[{"id":"z1","name":"exemple.fr"}],"result_info":{"page":1,"total_pages":1}}`)
		case strings.HasPrefix(r.URL.Path, "/zones/z1/dns_records"):
			if r.URL.Query().Get("type") == "AAAA" {
				fmt.Fprint(w, `{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"result":[{"name":"app.exemple.fr","content":"203.0.113.10"}],"result_info":{"page":1,"total_pages":1}}`)
		default:
			t.Fatalf("%s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	client := Client{CloudflareBase: server.URL, HTTP: server.Client()}
	records, err := client.Records(context.Background(), "cloudflare", Creds{Token: "tok"}, "exemple.fr")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != "app.exemple.fr" || records[0].Target != "203.0.113.10" {
		t.Fatalf("%+v", records)
	}
}

func TestOVHRecordsListsTheAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth/time":
			fmt.Fprint(w, "1700000000")
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.URL.Query().Get("fieldType") == "A":
			fmt.Fprint(w, `[1,2]`)
		case r.URL.Path == "/domain/zone/exemple.fr/record":
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/domain/zone/exemple.fr/record/1":
			fmt.Fprint(w, `{"subDomain":"myinfra2","target":"203.0.113.10"}`)
		case r.URL.Path == "/domain/zone/exemple.fr/record/2":
			fmt.Fprint(w, `{"subDomain":"","target":"203.0.113.1"}`)
		default:
			t.Fatalf("%s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	client := Client{OVHBase: server.URL, HTTP: server.Client()}
	records, err := client.Records(context.Background(), "ovh", Creds{ApplicationKey: "ak", ApplicationSecret: "s", ConsumerKey: "ck"}, "exemple.fr")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Name != "exemple.fr" || records[1].Name != "myinfra2.exemple.fr" || records[1].Target != "203.0.113.10" {
		t.Fatalf("%+v", records)
	}
}
