package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"myinfra/internal/box"
	"myinfra/internal/store"
)

// fakeOVHZone answers as the OVH API for one zone kept in memory.
type fakeOVHZone struct {
	mu      sync.Mutex
	records map[int64]map[string]any
	next    int64
}

func (f *fakeOVHZone) set(id int64, kind, sub, target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[id] = map[string]any{"id": id, "fieldType": kind, "subDomain": sub, "target": target, "ttl": 0}
}

func (f *fakeOVHZone) find(kind, sub string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.records {
		if r["fieldType"] == kind && r["subDomain"] == sub {
			out = append(out, fmt.Sprint(r["target"]))
		}
	}
	sort.Strings(out)
	return out
}

func (f *fakeOVHZone) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := "/domain/zone/moydomain.name/record"
	switch {
	case r.URL.Path == "/auth/time":
		fmt.Fprint(w, "1700000000")
	case r.URL.Path == "/domain/zone":
		fmt.Fprint(w, `["moydomain.name"]`)
	case r.URL.Path == "/domain/zone/moydomain.name/refresh":
	case r.URL.Path == base && r.Method == http.MethodGet:
		ids := []int64{}
		for id, rec := range f.records {
			sub, filtered := r.URL.Query()["subDomain"]
			kind := r.URL.Query().Get("fieldType")
			if (!filtered || rec["subDomain"] == sub[0]) && (kind == "" || rec["fieldType"] == kind) {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		raw, _ := json.Marshal(ids)
		w.Write(raw)
	case r.URL.Path == base && r.Method == http.MethodPost:
		var rec map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec)
		f.next++
		rec["id"] = f.next
		f.records[f.next] = rec
	case strings.HasPrefix(r.URL.Path, base+"/"):
		var id int64
		fmt.Sscan(strings.TrimPrefix(r.URL.Path, base+"/"), &id)
		switch r.Method {
		case http.MethodDelete:
			delete(f.records, id)
		case http.MethodGet:
			raw, _ := json.Marshal(f.records[id])
			w.Write(raw)
		}
	default:
		http.NotFound(w, r)
	}
}

func dnsFixture(t *testing.T) (*API, http.Handler, *fakeOVHZone) {
	t.Helper()
	t.Setenv("MYINFRA_TOFU_DIR", t.TempDir())
	a, h := tokenFixture(t)
	b, err := box.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	a.Box = b
	fake := &fakeOVHZone{records: map[int64]map[string]any{}, next: 100}
	fake.set(1, "MX", "", "10 mx1.old.net.")
	fake.set(2, "A", "www", "203.0.113.1")
	fake.set(3, "CNAME", "blog", "ghost.example.org.")
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	a.Verify.OVHBase = server.URL
	a.Verify.HTTP = server.Client()
	payload, _ := json.Marshal(storedToken{ApplicationKey: "ak", ApplicationSecret: "as", ConsumerKey: "ck"})
	sealed, err := a.seal(payload)
	if err != nil {
		t.Fatal(err)
	}
	sealed.ID, sealed.OwnerID, sealed.Kind, sealed.Name, sealed.Provider = "dns1", "owner", "dns_token", "ovh-perso", "ovh"
	if err := a.Store.InsertSecret(t.Context(), sealed); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateServer(t.Context(), "owner", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateApplication(t.Context(), store.NewApplication{
		ID: "app-cloud", OwnerID: "owner", Name: "nextcloud", SourceKind: "image", Source: "nextcloud:29",
		DestinationKind: "server", ServerID: "s1", Replicas: 1, DefaultID: "d1", DefaultHost: "nextcloud.apps.test",
		Names: []store.Name{{ID: "n1", Hostname: "cloud.moydomain.name", Zone: "moydomain.name", SecretID: "dns1"}},
	}); err != nil {
		t.Fatal(err)
	}
	return a, h, fake
}

func TestMCPDNSAsksWhenItTouchesSomething(t *testing.T) {
	_, h, fake := dnsFixture(t)
	admin := makeToken(t, h, "sess-owner", store.RoleAdmin, 0).Token
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token

	// A free name is written at once.
	text, failed := mcpCallTool(t, h, admin, "set_dns_record", map[string]any{"name": "docs.moydomain.name", "type": "CNAME", "values": []string{"monorg.github.io"}})
	if failed || !strings.HasPrefix(text, "Fait.") || strings.Join(fake.find("CNAME", "docs"), ",") != "monorg.github.io." {
		t.Fatal(text, fake.find("CNAME", "docs"))
	}
	// Replacing the MX asks first, with before and after and the mail warning.
	mx := map[string]any{"name": "moydomain.name", "type": "MX", "values": []string{"10 mail.protonmail.ch", "20 mailsec.protonmail.ch"}}
	text, failed = mcpCallTool(t, h, admin, "set_dns_record", mx)
	match := confirmCode.FindStringSubmatch(text)
	if failed || match == nil || !strings.Contains(text, "Avant :\n  MX 10 mx1.old.net") || !strings.Contains(text, "courrier") {
		t.Fatal(text)
	}
	if got := fake.find("MX", ""); len(got) != 1 {
		t.Fatal("écrit avant l'accord", got)
	}
	mx["confirmation"] = match[1]
	if text, failed := mcpCallTool(t, h, admin, "set_dns_record", mx); failed || !strings.HasPrefix(text, "Fait.") {
		t.Fatal(text)
	}
	if got := strings.Join(fake.find("MX", ""), ","); got != "10 mail.protonmail.ch.,20 mailsec.protonmail.ch." {
		t.Fatal(got)
	}
	// A record changed behind the recap: nothing is done, a new recap comes.
	www := map[string]any{"name": "www.moydomain.name", "type": "A", "values": []string{"198.51.100.7"}}
	text, _ = mcpCallTool(t, h, admin, "set_dns_record", www)
	code := confirmCode.FindStringSubmatch(text)[1]
	fake.set(2, "A", "www", "203.0.113.99")
	www["confirmation"] = code
	text, failed = mcpCallTool(t, h, admin, "set_dns_record", www)
	if failed || !strings.Contains(text, "La situation a changé") || !strings.Contains(text, "203.0.113.99") || strings.Join(fake.find("A", "www"), ",") != "203.0.113.99" {
		t.Fatal(text)
	}
	// The name of an application is left to the application.
	if text, failed := mcpCallTool(t, h, admin, "set_dns_record", map[string]any{"name": "cloud.moydomain.name", "type": "A", "values": []string{"198.51.100.7"}}); !failed || !strings.Contains(text, "nextcloud") {
		t.Fatal(text)
	}
	// Deleting always asks.
	del := map[string]any{"name": "blog.moydomain.name", "type": "CNAME"}
	text, _ = mcpCallTool(t, h, admin, "delete_dns_record", del)
	del["confirmation"] = confirmCode.FindStringSubmatch(text)[1]
	if text, failed := mcpCallTool(t, h, admin, "delete_dns_record", del); failed || len(fake.find("CNAME", "blog")) != 0 {
		t.Fatal(text)
	}
	// Writing DNS is for admins, as in the interface; reading is for all.
	if text, failed := mcpCallTool(t, h, deploy, "set_dns_record", map[string]any{"name": "x.moydomain.name", "type": "A", "values": []string{"198.51.100.7"}}); !failed || !strings.Contains(text, "Droits insuffisants") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, deploy, "list_dns_records", map[string]any{"zone": "moydomain.name"})
	if failed || !strings.Contains(text, "mail.protonmail.ch") || !strings.Contains(text, "docs.moydomain.name") {
		t.Fatal(text)
	}
	rec := call(t, h, http.MethodGet, "/api/events?source=mcp", "", "sess-owner", nil)
	if !strings.Contains(rec.Body.String(), `"kind":"dns.set"`) || !strings.Contains(rec.Body.String(), `"kind":"dns.delete"`) {
		t.Fatal(rec.Body.String())
	}
}
