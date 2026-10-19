package provision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"myinfra/internal/catalog"
)

// Le provider hcloud est piloté par OpenTofu contre un faux serveur Hetzner.
// Le test exige le binaire `tofu` et l'accès au registre, il est ignoré sinon.
func TestHetznerThroughOpenTofu(t *testing.T) {
	bin, err := exec.LookPath("tofu")
	if err != nil || os.Getenv("MYINFRA_TOFU_TEST") == "" {
		t.Skip("MYINFRA_TOFU_TEST et le binaire tofu sont requis")
	}
	tok := strings.Repeat("a1", 32)
	var mu sync.Mutex
	var calls []string
	created := false
	deleted := false
	server := func() map[string]any {
		return map[string]any{
			"id": 4242, "name": "tofu-1", "status": "running",
			"public_net": map[string]any{
				"ipv4":         map[string]any{"ip": "203.0.113.77", "id": 1, "blocked": false, "dns_ptr": "x"},
				"ipv6":         nil,
				"floating_ips": []int{},
				"firewalls":    []any{},
			},
			"private_net": []any{}, "volumes": []int{}, "labels": map[string]string{},
			"server_type": map[string]any{"id": 22, "name": "cpx11", "architecture": "x86", "cores": 2, "memory": 2, "disk": 40, "cpu_type": "shared", "deprecated": false, "prices": []any{}},
			"datacenter":  map[string]any{"id": 1, "name": "fsn1-dc14", "description": "x", "location": map[string]any{"id": 1, "name": "fsn1", "description": "x", "country": "DE", "city": "Falkenstein", "latitude": 0, "longitude": 0, "network_zone": "eu-central"}},
			"image":       map[string]any{"id": 9, "name": "debian-13", "type": "system", "status": "available", "os_flavor": "debian", "os_version": "13", "architecture": "x86", "description": "Debian 13", "created": "2026-01-01T00:00:00+00:00", "labels": map[string]string{}},
			"created":     "2026-01-01T00:00:00+00:00", "protection": map[string]bool{"delete": false, "rebuild": false},
			"rescue_enabled": false, "locked": false, "backup_window": nil, "included_traffic": 0, "ingoing_traffic": 0, "outgoing_traffic": 0, "primary_disk_size": 40,
			"placement_group": nil, "iso": nil, "load_balancers": []int{},
		}
	}
	action := map[string]any{"id": 1, "command": "x", "status": "success", "progress": 100, "started": "2026-01-01T00:00:00+00:00", "finished": "2026-01-01T00:00:01+00:00", "resources": []any{}, "error": nil}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer "+tok {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		meta := map[string]any{"pagination": map[string]any{"page": 1, "per_page": 50, "previous_page": nil, "next_page": nil, "last_page": 1, "total_entries": 1}}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/servers":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body["user_data"].(string), "echo hello") {
				t.Errorf("user_data %v", body["user_data"])
			}
			created = true
			reply(map[string]any{"server": server(), "action": action, "next_actions": []any{}, "root_password": nil})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/servers/4242":
			if deleted {
				w.WriteHeader(http.StatusNotFound)
				reply(map[string]any{"error": map[string]any{"code": "not_found", "message": "server not found"}})
				return
			}
			reply(map[string]any{"server": server()})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/servers/4242":
			deleted = true
			reply(map[string]any{"action": action})
		case r.URL.Path == "/v1/servers" && r.Method == http.MethodGet:
			if created && !deleted {
				reply(map[string]any{"servers": []any{server()}, "meta": meta})
				return
			}
			reply(map[string]any{"servers": []any{}, "meta": meta})
		case strings.HasPrefix(r.URL.Path, "/v1/actions"):
			reply(map[string]any{"action": action, "actions": []any{action}, "meta": meta})
		case r.URL.Path == "/v1/server_types":
			reply(map[string]any{"server_types": []any{server()["server_type"]}, "meta": meta})
		case r.URL.Path == "/v1/images":
			reply(map[string]any{"images": []any{server()["image"]}, "meta": meta})
		case r.URL.Path == "/v1/locations":
			reply(map[string]any{"locations": []any{server()["datacenter"].(map[string]any)["location"]}, "meta": meta})
		default:
			t.Logf("route inconnue %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
			reply(map[string]any{"error": map[string]any{"code": "not_found", "message": "route"}})
		}
	}))
	defer srv.Close()

	client := Client{HTTP: srv.Client(), HetznerBase: srv.URL, TofuDir: t.TempDir(), TofuBin: bin}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	machine, err := client.Create(ctx, "hetzner", catalog.Creds{Token: tok}, Spec{
		Name: "tofu-1", Location: "fsn1", OfferID: "cpx11", UserData: "#!/bin/sh\necho hello",
	})
	if err != nil || machine.ID != "4242" || machine.IPv4 != "203.0.113.77" {
		t.Fatalf("machine %+v %v\n%v", machine, err, calls)
	}
	t.Logf("création via tofu : %s", time.Since(start).Round(time.Millisecond))
	start = time.Now()
	if err := client.Destroy(ctx, "hetzner", catalog.Creds{Token: tok}, machine.ID); err != nil {
		t.Fatalf("destroy %v\n%v", err, calls)
	}
	t.Logf("destruction via tofu : %s", time.Since(start).Round(time.Millisecond))
	if !deleted {
		t.Fatalf("la machine n'a pas été supprimée\n%v", calls)
	}
	t.Logf("appels API : %d", len(calls))
}

func TestScalewayThroughOpenTofu(t *testing.T) {
	bin, err := exec.LookPath("tofu")
	if err != nil || os.Getenv("MYINFRA_TOFU_TEST") == "" {
		t.Skip("MYINFRA_TOFU_TEST et le binaire tofu sont requis")
	}
	const (
		project = "11111111-1111-4111-8111-111111111111"
		srvID   = "22222222-2222-4222-8222-222222222222"
		ipID    = "33333333-3333-4333-8333-333333333333"
		volID   = "44444444-4444-4444-8444-444444444444"
		imageID = "55555555-5555-4555-8555-555555555555"
	)
	token := "66666666-6666-4666-8666-666666666666"
	var mu sync.Mutex
	var calls []string
	var unknown []string
	state := "absent"
	var userData string
	ip := func() map[string]any {
		return map[string]any{"id": ipID, "address": "203.0.113.88", "project": project, "zone": "fr-par-1", "tags": []string{}, "server": nil, "reverse": nil, "type": "routed_ipv4", "state": "attached", "organization": project}
	}
	volume := func() map[string]any {
		return map[string]any{"id": volID, "name": "root", "volume_type": "b_ssd", "size": 20000000000, "state": "available", "project": project, "zone": "fr-par-1", "server": map[string]any{"id": srvID, "name": "tofu-sw"}, "creation_date": "2026-01-01T00:00:00+00:00", "modification_date": "2026-01-01T00:00:00+00:00", "export_uri": nil, "organization": project, "tags": []string{}}
	}
	server := func() map[string]any {
		return map[string]any{
			"id": srvID, "name": "tofu-sw", "project": project, "organization": project, "commercial_type": "DEV1-S", "state": state, "zone": "fr-par-1",
			"tags": []string{}, "hostname": "tofu-sw", "arch": "x86_64", "dynamic_ip_required": false, "routed_ip_enabled": true, "enable_ipv6": false,
			"public_ip": ip(), "public_ips": []any{ip()}, "private_ip": nil, "ipv6": nil, "volumes": map[string]any{"0": volume()},
			"image":     map[string]any{"id": imageID, "name": "Debian Trixie", "arch": "x86_64", "state": "available"},
			"protected": false, "state_detail": "", "location": nil, "ipv6s": []any{}, "bootscript": nil, "boot_type": "local", "security_group": map[string]any{"id": "77777777-7777-4777-8777-777777777777", "name": "Default"},
			"placement_group": nil, "maintenances": []any{}, "allowed_actions": []string{"poweroff", "terminate", "reboot"}, "private_nics": []any{}, "creation_date": "2026-01-01T00:00:00+00:00", "modification_date": "2026-01-01T00:00:00+00:00",
			"admin_password_encryption_ssh_key_id": nil, "admin_password_encrypted_value": nil, "filesystems": []any{}, "end_of_service": false,
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("X-Auth-Token"); got != token {
			t.Errorf("token %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		reply := func(code int, v any) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(v)
		}
		p := r.URL.Path
		switch {
		case strings.Contains(p, "/marketplace/") && strings.Contains(p, "local-images"):
			reply(200, map[string]any{"local_images": []any{map[string]any{"id": imageID, "arch": "x86_64", "zone": "fr-par-1", "compatible_commercial_types": []string{"DEV1-S"}, "label": "debian_trixie"}}, "total_count": 1})
		case strings.Contains(p, "/marketplace/") && strings.Contains(p, "images"):
			reply(200, map[string]any{"images": []any{}, "total_count": 0})
		case strings.HasSuffix(p, "/private-network-interfaces"):
			reply(200, map[string]any{"private_nics": []any{}, "total_count": 0})
		case p == "/instance/v1/zones/fr-par-1/products/servers":
			reply(200, map[string]any{"servers": map[string]any{"DEV1-S": map[string]any{
				"monthly_price": 8.0, "hourly_price": 0.01, "alt_names": []string{}, "per_volume_constraint": map[string]any{"l_ssd": map[string]any{"min_size": 0, "max_size": 800000000000}},
				"volumes_constraint": map[string]any{"min_size": 0, "max_size": 800000000000}, "ncpus": 2, "gpu": nil, "ram": 2000000000, "arch": "x86_64",
				"baremetal": false, "network": map[string]any{"interfaces": []any{}, "sum_internal_bandwidth": 200000000, "sum_internet_bandwidth": 200000000, "ipv6_support": true}, "capabilities": map[string]any{"block_storage": true, "boot_types": []string{"local"}, "max_file_systems": 0}, "end_of_service": false,
			}}})
		case p == "/instance/v1/zones/fr-par-1/ips" && r.Method == http.MethodPost:
			reply(200, map[string]any{"ip": ip()})
		case strings.HasPrefix(p, "/instance/v1/zones/fr-par-1/ips/"):
			if r.Method == http.MethodDelete {
				reply(204, map[string]any{})
				return
			}
			reply(200, map[string]any{"ip": ip()})
		case p == "/instance/v1/zones/fr-par-1/servers" && r.Method == http.MethodPost:
			state = "stopped"
			reply(201, map[string]any{"server": server()})
		case strings.HasSuffix(p, "/user_data/cloud-init"):
			if r.Method == http.MethodPatch {
				b, _ := io.ReadAll(r.Body)
				userData = string(b)
				reply(204, map[string]any{})
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(userData))
		case strings.HasSuffix(p, "/user_data"):
			reply(200, map[string]any{"user_data": []string{"cloud-init"}})
		case strings.HasSuffix(p, "/action") && r.Method == http.MethodPost:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body["action"] {
			case "poweron":
				state = "running"
			case "terminate", "poweroff":
				state = "absent"
			}
			reply(202, map[string]any{"task": map[string]any{"id": "t1", "description": body["action"], "status": "success", "progress": 100}})
		case p == "/instance/v1/zones/fr-par-1/servers/"+srvID:
			if state == "absent" || r.Method == http.MethodDelete {
				if r.Method == http.MethodDelete {
					state = "absent"
					reply(204, map[string]any{})
					return
				}
				reply(404, map[string]any{"type": "not_found", "message": "resource is not found", "resource": "instance_server"})
				return
			}
			reply(200, map[string]any{"server": server()})
		case strings.HasPrefix(p, "/instance/v1/zones/fr-par-1/volumes/") || strings.HasPrefix(p, "/block/"):
			reply(200, map[string]any{"volume": volume()})
		case strings.Contains(p, "/servers/"+srvID+"/") || strings.HasSuffix(p, "/servers"):
			reply(200, map[string]any{"servers": []any{}, "total_count": 0})
		default:
			unknown = append(unknown, r.Method+" "+r.URL.String())
			reply(404, map[string]any{"type": "not_found", "message": "route inconnue", "resource": "x"})
		}
	}))
	defer srv.Close()

	client := Client{HTTP: srv.Client(), ScalewayBase: srv.URL, TofuDir: t.TempDir(), TofuBin: bin}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	machine, err := client.Create(ctx, "scaleway", catalog.Creds{Token: token, ProjectID: project}, Spec{
		Name: "tofu-sw", Location: "fr-par-1", OfferID: "DEV1-S", UserData: "#!/bin/sh\necho hello", RootGB: 20,
	})
	if err != nil || machine.ID != "fr-par-1/"+srvID || machine.IPv4 != "203.0.113.88" {
		t.Fatalf("machine %+v %v\nappels %v\ninconnus %v", machine, err, calls, unknown)
	}
	t.Logf("création via tofu : %s", time.Since(start).Round(time.Millisecond))
	if !strings.Contains(userData, "echo hello") {
		t.Fatalf("cloud-init %q", userData)
	}
	if err := client.Destroy(ctx, "scaleway", catalog.Creds{Token: token, ProjectID: project}, machine.ID); err != nil {
		t.Fatalf("destroy %v\nappels %v\ninconnus %v", err, calls, unknown)
	}
	if state != "absent" {
		t.Fatalf("la machine n'a pas été supprimée (%s)", state)
	}
	t.Logf("appels API : %d, routes inconnues : %v", len(calls), unknown)
}
