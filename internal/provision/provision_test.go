package provision

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"myinfra/internal/catalog"
)

func TestCreateHetzner(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/servers" {
			body, _ := io.ReadAll(r.Body)
			got = string(body)
			w.Write([]byte(`{"server":{"id":42,"public_net":{"ipv4":{"ip":"203.0.113.10"}}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	machine, err := (Client{HTTP: srv.Client(), HetznerBase: srv.URL}).createHetzner(context.Background(), "tok", Spec{
		Name: "myinfra-ab", Location: "fsn1", OfferID: "cx23", UserData: "#cloud-config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if machine.ID != "42" || machine.IPv4 != "203.0.113.10" || !strings.Contains(got, `"server_type":"cx23"`) || !strings.Contains(got, "debian-13") {
		t.Fatalf("machine %+v body %s", machine, got)
	}
}

func TestCreateDigitalOcean(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/droplets" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			got = string(body)
			w.Write([]byte(`{"droplet":{"id":7,"networks":{"v4":[{"ip_address":"203.0.113.20","type":"public"}]}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	machine, err := (Client{HTTP: srv.Client(), DigitalOceanBase: srv.URL}).createDigitalOcean(context.Background(), "tok", Spec{
		Name: "myinfra-ab", Location: "ams3", OfferID: "s-2vcpu-2gb", UserData: "#cloud-config",
	})
	if err != nil || machine.IPv4 != "203.0.113.20" || machine.ID != "7" || !strings.Contains(got, `"image":"debian-13-x64"`) {
		t.Fatalf("machine %+v %v body %s", machine, err, got)
	}
}

func TestOVHRegionPicksSite(t *testing.T) {
	body := []byte(`["BHS","BHS5","DE","DE1","GRA","GRA11","RBX","RBX-A","SBG","SBG5","UK","UK1","WAW","WAW1"]`)
	if got := ovhRegion(body, "WAW"); got != "WAW1" {
		t.Fatalf("varsovie %s", got)
	}
	if got := ovhRegion(body, "GRA"); got != "GRA11" {
		t.Fatalf("gravelines %s", got)
	}
	if got := ovhRegion(body, "RBX"); got != "RBX-A" {
		t.Fatalf("roubaix %s", got)
	}
	archived := []byte(`["RBX","RBX-ARCHIVE"]`)
	if got := ovhRegion(archived, "RBX"); got != "RBX" {
		t.Fatalf("archive %s", got)
	}
}

func TestMatchFlavorUsesThePlanCode(t *testing.T) {
	body := []byte(`[{"id":"old","name":"s1-8","available":true,"planCodes":{"hourly":"s1-8.consumption"}},{"id":"new","name":"b3-8","available":true,"osType":"linux","planCodes":{"hourly":"b3-8.consumption"}}]`)
	if got := matchFlavor(body, "b3-8.consumption", "b3-8"); got != "new" {
		t.Fatalf("code %s", got)
	}
	if got := matchFlavor(body, "s1-8.consumption", "s1-8"); got != "old" {
		t.Fatalf("ancien %s", got)
	}
	if got := matchFlavor(body, "absent.consumption", "absent"); got != "" {
		t.Fatal(got)
	}
	sold := soldFlavors(body)
	if !sold["b3-8.consumption"] || !sold["s1-8"] {
		t.Fatal(sold)
	}
}

func TestDebianImageSkipsBaremetal(t *testing.T) {
	body := []byte(`[
		{"id":"bm","name":"Debian 13","type":"baremetal-linux"},
		{"id":"old","name":"Debian 12","type":"linux"},
		{"id":"vm","name":"Debian 13","type":"linux"}
	]`)
	if got := debianImage(body); got != "vm" {
		t.Fatal(got)
	}
	metal := []byte(`[{"id":"metal","name":"bm-8","type":"baremetal","available":true,"osType":"linux","planCodes":{"hourly":"bm-8.consumption"}}]`)
	if got := matchFlavor(metal, "bm-8.consumption", "bm-8"); got != "" {
		t.Fatal(got)
	}
}

func TestDestroyScalewayDeletesBlockVolumes(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/action"):
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/servers/"):
			w.Write([]byte(`{"server":{"id":"srv","state":"stopped","security_group":{"id":"sg-test"},"volumes":{"0":{"id":"vol-sbs","volume_type":"sbs_volume"},"1":{"id":"vol-local","volume_type":"l_ssd"}}}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/security_groups/"):
			w.Write([]byte(`{"security_group":{"id":"sg-test","servers":[]}}`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/servers/"):
			deleted = append(deleted, r.URL.RequestURI())
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/block/"):
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/security_groups/"):
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("appel inattendu %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	err := (Client{HTTP: srv.Client(), ScalewayBase: srv.URL}).Destroy(context.Background(), "scaleway", catalog.Creds{Token: "tok"}, "fr-par-1/srv")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(deleted, " ")
	if !strings.Contains(got, "delete_volumes=true") ||
		!strings.Contains(got, "/block/v1/zones/fr-par-1/volumes/vol-sbs") ||
		!strings.Contains(got, "/instance/v1/zones/fr-par-1/security_groups/sg-test") ||
		strings.Contains(got, "vol-local") {
		t.Fatalf("suppressions %s", got)
	}
}

func TestScalewayRegistersTheProjectSSHKey(t *testing.T) {
	var created string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/iam/v1alpha1/ssh-keys":
			w.Write([]byte(`{"ssh_keys":[{"public_key":"ssh-ed25519 AAAAlready"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/iam/v1alpha1/ssh-keys":
			body, _ := io.ReadAll(r.Body)
			created = string(body)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("appel inattendu %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	err := (Client{HTTP: srv.Client()}).ensureScalewaySSHKeys(context.Background(), srv.URL, "project", scalewayAuth("tok"), []string{
		"ssh-ed25519 AAAAlready",
		"ssh-ed25519 AAANew",
	})
	if err != nil || !strings.Contains(created, "AAANew") || strings.Contains(created, "AAAAlready") {
		t.Fatalf("err %v body %s", err, created)
	}
}

func TestCreateUpCloud(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %s", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/1.3/plan":
			w.Write([]byte(`{"plans":{"plan":[{"name":"2xCPU-4GB","storage_size":80,"storage_tier":"maxiops"}]}}`))
		case "/1.3/storage/template":
			w.Write([]byte(`{"storages":{"storage":[{"uuid":"debian-12","title":"Debian GNU/Linux 12"},{"uuid":"debian-13","title":"Debian GNU/Linux 13 (Trixie)"}]}}`))
		case "/1.3/server":
			body, _ := io.ReadAll(r.Body)
			got = string(body)
			w.Write([]byte(`{"server":{"uuid":"srv-1","ip_addresses":{"ip_address":[{"access":"public","family":"IPv4","address":"203.0.113.30"}]}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	machine, err := (Client{HTTP: srv.Client(), UpCloudBase: srv.URL}).createUpCloud(context.Background(), "tok", Spec{
		Name: "Test 1", Location: "de-fra1", OfferID: "2xCPU-4GB", UserData: "#!/bin/sh\necho ok", SSHKeys: []string{"ssh-ed25519 AAA"},
	})
	if err != nil || machine.ID != "srv-1" || machine.IPv4 != "203.0.113.30" {
		t.Fatalf("machine %+v %v", machine, err)
	}
	if !strings.Contains(got, `"storage":"debian-13"`) || !strings.Contains(got, "echo ok") || !strings.Contains(got, "ssh-ed25519 AAA") || !strings.Contains(got, `"size":80`) {
		t.Fatalf("body %s", got)
	}
	if !strings.Contains(got, `"firewall":"off"`) {
		t.Fatalf("le pare-feu UpCloud doit être désactivé, body %s", got)
	}
}

func TestDestroyUpCloudStopsFirst(t *testing.T) {
	state := "started"
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet:
			w.Write([]byte(`{"server":{"state":"` + state + `"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"):
			state = "stopped"
			w.Write([]byte(`{"server":{"state":"maintenance"}}`))
		case r.Method == http.MethodDelete:
			if state != "stopped" {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	fast(t)
	if err := (Client{HTTP: srv.Client(), UpCloudBase: srv.URL}).Destroy(context.Background(), "upcloud", catalog.Creds{Token: "tok"}, "srv-1"); err != nil {
		t.Fatal(err)
	}
	if last := calls[len(calls)-1]; last != "DELETE /1.3/server/srv-1" {
		t.Fatalf("appels %v", calls)
	}
}

func TestRefusedExplainsQuota(t *testing.T) {
	err := refused(403, []byte(`{"error":{"error_code":"SERVER_CORE_LIMIT_REACHED","error_message":"Trial CPU core quota of 2 exceeded"}}`))
	if !strings.Contains(err.Error(), "quota") {
		t.Fatalf("message %q", err)
	}
}

func TestRefusedExplainsReason(t *testing.T) {
	err := refused(400, []byte(`{"errors": [{"reason": "Resource creation in this region is currently restricted. Please select another region.", "field": "region"}]}`))
	if !strings.Contains(err.Error(), "ce lieu est restreint") {
		t.Fatalf("message %q", err)
	}
	if err := refused(500, []byte(`oops`)); err.Error() != "Le provider a refusé la machine." {
		t.Fatalf("message %q", err)
	}
}

func TestCreateVultr(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/os" {
			w.Write([]byte(`{"os":[{"id":12,"name":"Debian 12 x64","arch":"x64","family":"debian"},{"id":13,"name":"Debian 13 x64","arch":"x64","family":"debian"}]}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v2/instances" {
			body, _ := io.ReadAll(r.Body)
			got = string(body)
			w.Write([]byte(`{"instance":{"id":"inst-1","main_ip":"203.0.113.40"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	machine, err := (Client{HTTP: srv.Client(), VultrBase: srv.URL}).createVultr(context.Background(), "tok", Spec{
		Name: "myinfra-ab", Location: "ams", OfferID: "vc2-1c-1gb", UserData: "#!/bin/sh\necho ok",
	})
	if err != nil || machine.ID != "inst-1" || machine.IPv4 != "203.0.113.40" || !strings.Contains(got, `"os_id":13`) {
		t.Fatalf("machine %+v %v body %s", machine, err, got)
	}
	raw, err := base64.StdEncoding.DecodeString(userDataOf(got))
	if err != nil || !strings.Contains(string(raw), "echo ok") {
		t.Fatalf("user data %s %v", got, err)
	}
}

func TestCreateAkamai(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v4/images/") {
			if !strings.HasSuffix(r.URL.Path, "linode/debian13") {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`{"id":"linode/debian13"}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v4/linode/instances" {
			body, _ := io.ReadAll(r.Body)
			got = string(body)
			w.Write([]byte(`{"id":99,"ipv4":["203.0.113.50"]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	machine, err := (Client{HTTP: srv.Client(), AkamaiBase: srv.URL}).createAkamai(context.Background(), "tok", Spec{
		Name: "myinfra-ab", Location: "fr-par", OfferID: "g6-nanode-1", UserData: "#!/bin/sh\necho ok", SSHKeys: []string{"ssh-ed25519 AAA"},
	})
	if err != nil || machine.ID != "99" || machine.IPv4 != "203.0.113.50" || !strings.Contains(got, `"image":"linode/debian13"`) || !strings.Contains(got, "ssh-ed25519 AAA") {
		t.Fatalf("machine %+v %v body %s", machine, err, got)
	}
	encoded := userDataOf(got)
	outer, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !strings.Contains(string(outer), "#cloud-config") {
		t.Fatalf("config %s %v", got, err)
	}
}

func userDataOf(body string) string {
	const key = `"user_data":"`
	start := strings.Index(body, key)
	if start < 0 {
		return ""
	}
	rest := body[start+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func TestDigitalOceanGPUUsesUbuntu(t *testing.T) {
	if digitalOceanImage(0) != "debian-13-x64" || digitalOceanImage(1) != "ubuntu-24-04-x64" {
		t.Fatal(digitalOceanImage(0), digitalOceanImage(1))
	}
}

func TestScalewayPicksADebianImageForTheGPUType(t *testing.T) {
	body := []byte(`{"local_images":[{"id":"old","label":"debian_bookworm","compatible_commercial_types":["L4-1-24G"]},{"id":"new","label":"debian_trixie","compatible_commercial_types":["L4-1-24G"]},{"id":"ubuntu","label":"ubuntu_noble","compatible_commercial_types":["L4-1-24G"]}]}`)
	if scalewayCompatible(body, "L4-1-24G") != "new" {
		t.Fatal(scalewayCompatible(body, "L4-1-24G"))
	}
	if scalewayCompatible(body, "DEV1-S") != "" {
		t.Fatal("un type CPU ne prend pas l'image GPU")
	}
}

func TestRefusedNamesUnauthorizedIP(t *testing.T) {
	err := refused(401, []byte(`{"error":"Unauthorized IP address: 2001:db8::1","status":401}`))
	if err == nil || !strings.Contains(err.Error(), "2001:db8::1") {
		t.Fatal(err)
	}
	if unauthorizedIP("quota") != "" {
		t.Fatal("faux positif")
	}
}
