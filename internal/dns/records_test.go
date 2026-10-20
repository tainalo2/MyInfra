package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
