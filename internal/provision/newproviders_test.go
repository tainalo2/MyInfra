package provision

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"myinfra/internal/catalog"
)

func fast(t *testing.T) {
	t.Helper()
	old := pollEvery
	pollEvery = time.Millisecond
	t.Cleanup(func() { pollEvery = old })
}

func TestCreateAndDestroyExoscale(t *testing.T) {
	fast(t)
	var mu sync.Mutex
	var instance, deleted string
	rules := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "EXO2-HMAC-SHA256 credential=EXOkey,") {
			t.Errorf("signature absente: %q", r.Header.Get("Authorization"))
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/template":
			if r.URL.Query().Get("family") != "debian" || !strings.Contains(r.Header.Get("Authorization"), "signed-query-args=family;visibility") {
				t.Errorf("requête de gabarit %s %s", r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			w.Write([]byte(`{"templates":[{"id":"t12","name":"Linux Debian 12 (Bookworm) 64-bit"},{"id":"t13","name":"Linux Debian 13 (Trixie) 64-bit"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/security-group":
			w.Write([]byte(`{"security-groups":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/security-group":
			w.Write([]byte(`{"id":"op-sg","state":"success","reference":{"id":"sg-1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/security-group/sg-1/rules":
			rules++
			w.Write([]byte(`{"id":"op-rule","state":"success","reference":{"id":"sg-1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/instance":
			body, _ := io.ReadAll(r.Body)
			instance = string(body)
			w.Write([]byte(`{"id":"op-1","state":"pending","reference":{"id":"inst-1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/operation/op-1":
			w.Write([]byte(`{"id":"op-1","state":"success","reference":{"id":"inst-1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/instance/inst-1":
			w.Write([]byte(`{"id":"inst-1","public-ip":"203.0.113.60"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/instance/inst-1":
			deleted = r.URL.Path
			w.Write([]byte(`{"id":"op-del","state":"pending"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := Client{HTTP: srv.Client(), ExoscaleBase: srv.URL}
	creds := catalog.Creds{ApplicationKey: "EXOkey", ApplicationSecret: "secret"}
	machine, err := client.createExoscale(context.Background(), creds, Spec{
		Name: "myinfra-ab", Location: "de-fra-1", OfferID: "type-1", RootGB: 20, UserData: "#!/bin/sh\necho ok",
	})
	if err != nil || machine.ID != "de-fra-1:inst-1" || machine.IPv4 != "203.0.113.60" || rules != 3 {
		t.Fatalf("machine %+v %v règles %d", machine, err, rules)
	}
	for _, want := range []string{`"id":"t13"`, `"id":"type-1"`, `"disk-size":20`, `"id":"sg-1"`, `"public-ip-assignment":"inet4"`} {
		if !strings.Contains(instance, want) {
			t.Fatalf("%s manquant dans %s", want, instance)
		}
	}
	if err := client.Destroy(context.Background(), "exoscale", creds, machine.ID); err != nil || deleted == "" {
		t.Fatalf("destroy %v %q", err, deleted)
	}
}

func TestCreateAndDestroyGcore(t *testing.T) {
	fast(t)
	var instance, deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "APIKey tok" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == "/cloud/v1/projects":
			w.Write([]byte(`{"results":[{"id":5,"is_default":true}]}`))
		case r.URL.Path == "/cloud/v1/images/5/76":
			w.Write([]byte(`{"results":[{"id":"img-12","name":"debian-12","os_distro":"debian","os_version":"12","min_disk":5,"status":"active"},{"id":"img-13","name":"debian-13","os_distro":"debian","os_version":"13","min_disk":15,"status":"active"},{"id":"win","name":"windows","os_distro":"windows","os_version":"2022","os_type":"windows"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cloud/v1/securitygroups/5/76":
			w.Write([]byte(`{"results":[{"id":"sg-9","name":"myinfra"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cloud/v2/instances/5/76":
			body, _ := io.ReadAll(r.Body)
			instance = string(body)
			w.Write([]byte(`{"tasks":["task-1"]}`))
		case r.URL.Path == "/cloud/v1/tasks/task-1":
			w.Write([]byte(`{"id":"task-1","state":"FINISHED","created_resources":{"instances":["vm-1"]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cloud/v1/instances/5/76/vm-1":
			w.Write([]byte(`{"id":"vm-1","addresses":{"ext":[{"addr":"10.0.0.4","type":"fixed"},{"addr":"203.0.113.70","type":"fixed"}]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/cloud/v1/instances/5/76/vm-1":
			deleted = r.URL.RawQuery
			w.Write([]byte(`{"tasks":["task-2"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := Client{HTTP: srv.Client(), GcoreBase: srv.URL}
	creds := catalog.Creds{Token: "tok"}
	machine, err := client.createGcore(context.Background(), creds.Token, Spec{
		Name: "myinfra-ab", Location: "luxembourg-2-76", OfferID: "g1-standard-1-2", RootGB: 10, UserData: "#!/bin/sh\necho ok",
	})
	if err != nil || machine.ID != "76:5:vm-1" || machine.IPv4 != "203.0.113.70" {
		t.Fatalf("machine %+v %v", machine, err)
	}
	for _, want := range []string{`"image_id":"img-13"`, `"size":15`, `"flavor":"g1-standard-1-2"`, `"id":"sg-9"`, `"type":"external"`} {
		if !strings.Contains(instance, want) {
			t.Fatalf("%s manquant dans %s", want, instance)
		}
	}
	if strings.Contains(instance, `"password"`) {
		t.Fatal("un mot de passe désactive user_data")
	}
	raw, err := base64.StdEncoding.DecodeString(userDataOf(instance))
	if err != nil || !strings.Contains(string(raw), "echo ok") {
		t.Fatalf("user_data %s %v", instance, err)
	}
	if err := client.Destroy(context.Background(), "gcore", creds, machine.ID); err != nil || deleted != "delete_floatings=true" {
		t.Fatalf("destroy %v %q", err, deleted)
	}
}

func TestCreateAndDestroyKamatera(t *testing.T) {
	fast(t)
	var create, terminated string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("AuthClientId") != "cid" || r.Header.Get("AuthSecret") != "sec" {
			t.Errorf("en-têtes %v", r.Header)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/service/server":
			if r.URL.Query().Get("datacenter") != "EU-FR" {
				t.Errorf("datacenter %s", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"id":"EU-FR:alma","os":"AlmaLinux","code":"9 64bit"},{"id":"EU-FR:img-11","os":"Debian","code":"11 64bit"},{"id":"EU-FR:img-13","os":"Debian","code":"13 64bit"},{"id":"EU-FR:win","os":"Windows","code":"2022 64bit"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/service/server":
			body, _ := io.ReadAll(r.Body)
			create = string(body)
			w.Write([]byte(`["cmd-1"]`))
		case r.URL.Path == "/service/queue":
			if r.URL.Query().Get("id") == "cmd-1" {
				w.Write([]byte(`[{"status":"complete","log":"Creating\nName: myinfra-ab\nDone"}]`))
				return
			}
			w.Write([]byte(`[{"status":"complete","log":""}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/service/server/info":
			w.Write([]byte(`[{"id":"srv-uuid","networks":[{"network":"lan-1","ips":["10.1.1.2"]},{"network":"wan-eu","ips":["203.0.113.80"]}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/service/server/terminate":
			body, _ := io.ReadAll(r.Body)
			terminated = string(body)
			w.Write([]byte(`["cmd-2"]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := Client{HTTP: srv.Client(), KamateraBase: srv.URL}
	creds := catalog.Creds{ApplicationKey: "cid", ApplicationSecret: "sec"}
	machine, err := client.createKamatera(context.Background(), creds, Spec{
		Name: "myinfra-ab", Location: "EU-FR", OfferID: "2A-4096-40", UserData: "#!/bin/sh\necho ok", SSHKeys: []string{"ssh-ed25519 AAA"},
	})
	if err != nil || machine.ID != "srv-uuid" || machine.IPv4 != "203.0.113.80" {
		t.Fatalf("machine %+v %v", machine, err)
	}
	for _, want := range []string{`"image":"EU-FR:img-13"`, `"cpu":"2A"`, `"ram":4096`, `"disk":"size=40"`, `"billingcycle":"monthly"`, `"monthlypackage":"t5000"`, `"ssh-key":"ssh-ed25519 AAA"`, `echo ok`} {
		if !strings.Contains(create, want) {
			t.Fatalf("%s manquant dans %s", want, create)
		}
	}
	if err := client.Destroy(context.Background(), "kamatera", creds, machine.ID); err != nil || !strings.Contains(terminated, `"id":"srv-uuid"`) || !strings.Contains(terminated, `"force":true`) {
		t.Fatalf("destroy %v %s", err, terminated)
	}
}

func TestCreateAndDestroyIonos(t *testing.T) {
	fast(t)
	var server, lan, deleted string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		async := func(body string) {
			w.Header().Set("Location", srv.URL+"/cloudapi/v6/requests/r1/status")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(body))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/cloudapi/v6/datacenters":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"location":"de/fra"`) {
				t.Errorf("lieu %s", body)
			}
			async(`{"id":"dc-1"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/cloudapi/v6/datacenters/dc-1/lans":
			body, _ := io.ReadAll(r.Body)
			lan = string(body)
			async(`{"id":"1"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/cloudapi/v6/datacenters/dc-1/servers":
			body, _ := io.ReadAll(r.Body)
			server = string(body)
			async(`{"id":"srv-1"}`)
		case r.URL.Path == "/cloudapi/v6/requests/r1/status":
			w.Write([]byte(`{"metadata":{"status":"DONE"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cloudapi/v6/datacenters/dc-1/servers/srv-1":
			w.Write([]byte(`{"entities":{"nics":{"items":[{"properties":{"ips":["203.0.113.90"]}}]}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/cloudapi/v6/datacenters/dc-1":
			deleted = r.URL.Path
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := Client{HTTP: srv.Client(), IonosBase: srv.URL}
	creds := catalog.Creds{Token: "tok"}
	machine, err := client.createIonos(context.Background(), creds.Token, Spec{
		Name: "myinfra-ab", Location: "de-fra", OfferID: "vcpu-2-4", RootGB: 30, UserData: "#!/bin/sh\necho ok", SSHKeys: []string{"ssh-ed25519 AAA"},
	})
	if err != nil || machine.ID != "dc-1" || machine.IPv4 != "203.0.113.90" {
		t.Fatalf("machine %+v %v", machine, err)
	}
	if !strings.Contains(lan, `"public":true`) {
		t.Fatalf("lan %s", lan)
	}
	for _, want := range []string{`"type":"VCPU"`, `"cores":2`, `"ram":4096`, `"size":30`, `"imageAlias":"debian:latest"`, `"lan":1`, `"sshKeys":["ssh-ed25519 AAA"]`} {
		if !strings.Contains(server, want) {
			t.Fatalf("%s manquant dans %s", want, server)
		}
	}
	if err := client.Destroy(context.Background(), "ionos", creds, machine.ID); err != nil || deleted == "" {
		t.Fatalf("destroy %v %q", err, deleted)
	}
}

func TestUnknownOffers(t *testing.T) {
	if _, _, _, err := kamateraOffer("bad"); err == nil {
		t.Fatal("offre Kamatera invalide")
	}
	if _, _, err := ionosOffer("bad"); err == nil {
		t.Fatal("offre IONOS invalide")
	}
	if _, err := (Client{}).Create(context.Background(), "clevercloud", catalog.Creds{}, Spec{}); err == nil || !strings.Contains(err.Error(), "inconnu") {
		t.Fatalf("Clever Cloud doit être inconnu, reçu %v", err)
	}
}
