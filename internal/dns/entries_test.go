package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeCloudflare keeps the records of one zone in memory.
type fakeCloudflare struct {
	mu      sync.Mutex
	records map[string]cfRecord
	next    int
	calls   []string
}

func (f *fakeCloudflare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/zones":
		fmt.Fprint(w, `{"success":true,"result":[{"id":"z1","name":"exemple.fr"}],"result_info":{"page":1,"total_pages":1}}`)
	case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodGet:
		var out []cfRecord
		for _, rec := range f.records {
			if name := r.URL.Query().Get("name"); name == "" || name == rec.Name {
				out = append(out, rec)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		raw, _ := json.Marshal(map[string]any{"success": true, "result": out, "result_info": map[string]int{"page": 1, "total_pages": 1}})
		w.Write(raw)
	case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodPost:
		var rec cfRecord
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec)
		f.next++
		rec.ID = fmt.Sprintf("r%d", f.next)
		f.records[rec.ID] = rec
		fmt.Fprint(w, `{"success":true}`)
	case strings.HasPrefix(r.URL.Path, "/zones/z1/dns_records/"):
		id := strings.TrimPrefix(r.URL.Path, "/zones/z1/dns_records/")
		if r.Method == http.MethodDelete {
			delete(f.records, id)
		} else {
			var rec cfRecord
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &rec)
			rec.ID = id
			f.records[id] = rec
		}
		fmt.Fprint(w, `{"success":true}`)
	default:
		http.NotFound(w, r)
	}
}

func prio(n int) *int { return &n }

func TestCloudflareRecordSets(t *testing.T) {
	fake := &fakeCloudflare{records: map[string]cfRecord{
		"a": {ID: "a", Type: "MX", Name: "exemple.fr", Content: "mx1.old.net", TTL: 300, Priority: prio(10)},
		"b": {ID: "b", Type: "TXT", Name: "exemple.fr", Content: `"v=spf1 -all"`, TTL: 1},
		"c": {ID: "c", Type: "A", Name: "www.exemple.fr", Content: "203.0.113.1", TTL: 1},
	}}
	server := httptest.NewServer(fake)
	defer server.Close()
	client := Client{CloudflareBase: server.URL, HTTP: server.Client()}
	ctx := context.Background()
	creds := Creds{Token: "tok"}

	all, err := client.Entries(ctx, "cloudflare", creds, "exemple.fr", "")
	if err != nil || len(all) != 3 {
		t.Fatal(all, err)
	}
	if all[1].Type != "TXT" || all[1].Content != "v=spf1 -all" || all[0].Priority != 10 {
		t.Fatalf("%+v", all)
	}
	// MX replaced: the old one goes, the two new ones come; TXT untouched.
	err = client.SetRecordSet(ctx, "cloudflare", creds, "exemple.fr", RecordSet{Name: "exemple.fr", Type: "MX", Values: []Value{{Content: "mail.protonmail.ch", Priority: 10}, {Content: "mailsec.protonmail.ch", Priority: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	apex, _ := client.Entries(ctx, "cloudflare", creds, "exemple.fr", "exemple.fr")
	var mx []string
	for _, e := range apex {
		if e.Type == "MX" {
			mx = append(mx, fmt.Sprint(e.Priority, " ", e.Content))
		}
	}
	if strings.Join(mx, ",") != "10 mail.protonmail.ch,20 mailsec.protonmail.ch" || len(apex) != 3 {
		t.Fatal(mx, apex)
	}
	// The same set again changes nothing.
	before := len(fake.calls)
	_ = client.SetRecordSet(ctx, "cloudflare", creds, "exemple.fr", RecordSet{Name: "exemple.fr", Type: "MX", Values: []Value{{Content: "mail.protonmail.ch", Priority: 10}, {Content: "mailsec.protonmail.ch", Priority: 20}}})
	for _, call := range fake.calls[before:] {
		if !strings.HasPrefix(call, "GET") {
			t.Fatal("écriture inutile", call)
		}
	}
	if err := client.DeleteRecordSet(ctx, "cloudflare", creds, "exemple.fr", "www.exemple.fr", "A"); err != nil {
		t.Fatal(err)
	}
	if left, _ := client.Entries(ctx, "cloudflare", creds, "exemple.fr", "www.exemple.fr"); len(left) != 0 {
		t.Fatal(left)
	}
	if err := client.SetRecordSet(ctx, "cloudflare", creds, "exemple.fr", RecordSet{Name: "x.exemple.fr", Type: "SRV", Values: []Value{{Content: "x"}}}); err == nil {
		t.Fatal("SRV écrit")
	}
}

func TestOVHRecordSets(t *testing.T) {
	var mu sync.Mutex
	records := map[string]ovhRecord{
		"1": {ID: 1, FieldType: "MX", SubDomain: "", Target: "10 mx1.old.net.", TTL: 0},
		"2": {ID: 2, FieldType: "TXT", SubDomain: "", Target: `"v=spf1 -all"`, TTL: 0},
	}
	next := int64(10)
	refreshed := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/auth/time":
			fmt.Fprint(w, "1700000000")
		case r.URL.Path == "/domain/zone/exemple.fr/refresh":
			refreshed++
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.Method == http.MethodGet:
			var ids []int64
			for _, rec := range records {
				if sub, ok := r.URL.Query()["subDomain"]; !ok || sub[0] == rec.SubDomain {
					ids = append(ids, rec.ID)
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			raw, _ := json.Marshal(ids)
			w.Write(raw)
		case r.URL.Path == "/domain/zone/exemple.fr/record" && r.Method == http.MethodPost:
			var rec ovhRecord
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &rec)
			next++
			rec.ID = next
			records[fmt.Sprint(next)] = rec
		case strings.HasPrefix(r.URL.Path, "/domain/zone/exemple.fr/record/"):
			id := strings.TrimPrefix(r.URL.Path, "/domain/zone/exemple.fr/record/")
			switch r.Method {
			case http.MethodDelete:
				delete(records, id)
			case http.MethodGet:
				raw, _ := json.Marshal(records[id])
				w.Write(raw)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := Client{OVHBase: server.URL, HTTP: server.Client()}
	ctx := context.Background()
	creds := Creds{ApplicationKey: "ak", ApplicationSecret: "s", ConsumerKey: "ck"}
	all, err := client.Entries(ctx, "ovh", creds, "exemple.fr", "")
	if err != nil || len(all) != 2 || all[0].Type != "MX" || all[0].Priority != 10 || all[0].Content != "mx1.old.net" || all[1].Content != "v=spf1 -all" {
		t.Fatalf("%+v %v", all, err)
	}
	if err := client.SetRecordSet(ctx, "ovh", creds, "exemple.fr", RecordSet{Name: "docs.exemple.fr", Type: "CNAME", Values: []Value{{Content: "monorg.github.io"}}}); err != nil {
		t.Fatal(err)
	}
	docs, _ := client.Entries(ctx, "ovh", creds, "exemple.fr", "docs.exemple.fr")
	if len(docs) != 1 || docs[0].Content != "monorg.github.io" || refreshed != 1 {
		t.Fatalf("%+v %d", docs, refreshed)
	}
	if records["11"].Target != "monorg.github.io." {
		t.Fatal(records["11"])
	}
	// Setting the same again writes nothing and does not refresh.
	_ = client.SetRecordSet(ctx, "ovh", creds, "exemple.fr", RecordSet{Name: "docs.exemple.fr", Type: "CNAME", Values: []Value{{Content: "monorg.github.io"}}})
	if refreshed != 1 {
		t.Fatal(refreshed)
	}
	if err := client.DeleteRecordSet(ctx, "ovh", creds, "exemple.fr", "exemple.fr", "MX"); err != nil {
		t.Fatal(err)
	}
	apex, _ := client.Entries(ctx, "ovh", creds, "exemple.fr", "exemple.fr")
	if len(apex) != 1 || apex[0].Type != "TXT" || refreshed != 2 {
		t.Fatalf("%+v %d", apex, refreshed)
	}
}
