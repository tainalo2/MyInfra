package agent

import (
	"os/exec"
	"strings"
	"testing"

	"myinfra/internal/spec"
)

func TestBootstrapInstallsRuntime(t *testing.T) {
	script, err := Bootstrap("http://127.0.0.1:8080", "tok_en")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"https://get.docker.com",
		"traefik:v3.6.2",
		"http://127.0.0.1:8080/agent/sync",
		"Authorization: Bearer tok_en",
		"/api/join",
		"LLMNR=no",
		"tcp dport { 22, 80, 443 } accept",
		"if [ ! -x /usr/local/bin/k3s ]",
	} {
		if !strings.Contains(script, part) {
			t.Fatalf("missing %s\n%s", part, script)
		}
	}
}

func TestSyncKeepsUnpublishedNameOffTheProxy(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{
		ID:    "7d49736b-58e7-4386-b947-720823531792",
		Image: "nginx:alpine",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "docker pull 'nginx:alpine'") {
		t.Fatal(script)
	}
	if !strings.Contains(script, "traefik.enable=false") {
		t.Fatal(script)
	}
	if strings.Contains(script, "essai.exemple.fr") || strings.Contains(script, "loadbalancer") || strings.Contains(script, "6443") {
		t.Fatal(script)
	}
}

func TestSyncRoutesReadyName(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{
		ID:    "7d49736b-58e7-4386-b947-720823531792",
		Image: "nginx:alpine",
		Hosts: []string{"essai.exemple.fr"},
		Env:   map[string]string{"COLOR": "blue"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "Host(`essai.exemple.fr`)") {
		t.Fatal(script)
	}
	if !strings.Contains(script, "traefik.enable=true") || !strings.Contains(script, "entrypoints=websecure") || !strings.Contains(script, "tls.certresolver=exemple-fr") || !strings.Contains(script, "certif@exemple.fr") || !strings.Contains(script, "redirectscheme.permanent=true") || !strings.Contains(script, "-e 'COLOR=blue'") {
		t.Fatal(script)
	}
	if strings.Contains(script, "traefik.enable=false") {
		t.Fatal(script)
	}
}

func TestSyncIsolatesInternalServices(t *testing.T) {
	id := "7d49736b-58e7-4386-b947-720823531792"
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{
		{ID: id, Service: "front", Image: "nginx:alpine", Published: true, Hosts: []string{"app.exemple.fr"}, Replicas: 1},
		{ID: id, Service: "postgres", Image: "postgres:16", Disk: true, Replicas: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "docker network create 'net"+strings.ReplaceAll(id, "-", "")+"'") {
		t.Fatal(script)
	}
	if !strings.Contains(script, "traefik.enable=true") || !strings.Contains(script, "docker network connect myinfra") {
		t.Fatal(script)
	}
	if !strings.Contains(script, "traefik.enable=false") || !strings.Contains(script, ":/data") || !strings.Contains(script, "PGDATA=/data") {
		t.Fatal(script)
	}
	compact := strings.ReplaceAll(id, "-", "")
	if !strings.Contains(script, "postgres-2") || !strings.Contains(script, "myinfra-data-"+compact+"-postgres-1") || !strings.Contains(script, "myinfra-data-"+compact+"-postgres-2") {
		t.Fatal(script)
	}
	if strings.Contains(script, "argocd") {
		t.Fatal(script)
	}
}

func TestSyncRejectsInjection(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{
		ID:    "7d49736b-58e7-4386-b947-720823531792",
		Image: "nginx:alpine;rm -rf /",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "docker pull") || strings.Contains(script, "rm -rf") {
		t.Fatal(script)
	}
	if !strings.Contains(script, "report '7d49736b-58e7-4386-b947-720823531792' failed") {
		t.Fatal(script)
	}
}

func TestSyncPauseStopsTheContainer(t *testing.T) {
	id := "7d49736b-58e7-4386-b947-720823531792"
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{
		ID: id, Service: "web", Image: "nginx:alpine", Disk: true, Replicas: 1, Paused: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "docker stop") || !strings.Contains(script, "report '"+id+"' paused") || strings.Contains(script, "docker pull") || strings.Contains(script, "docker volume rm") {
		t.Fatal(script)
	}
}

func TestSyncRejectsQuote(t *testing.T) {
	if _, err := Sync("http://127.0.0.1:8080", "a'b", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestProxyLabelIsQuoted(t *testing.T) {
	script := ensureProxy([]string{"exemple.fr"})
	if !strings.Contains(script, "--label 'myinfra.proxy="+proxyImage+"|exemple-fr'") {
		t.Fatal(script)
	}
	cmd := exec.Command("sh", "-n", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

func TestSyncStartsAgainAContainerStoppedByAPause(t *testing.T) {
	id := "7d49736b-58e7-4386-b947-720823531792"
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{
		ID: id, Service: "web", Image: "nginx:alpine", Disk: true, Replicas: 1, Published: true,
		Hosts: []string{"a.exemple.fr"}, Zones: map[string]string{"a.exemple.fr": "exemple.fr"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "docker start 'myinfra-7d49736b58e74386b947720823531792-web'") {
		t.Fatal(script)
	}
}

func TestSyncNeverPullsALocalImage(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{ID: "7d49736b-58e7-4386-b947-720823531792", Image: "myinfra.local/demo:1.0", Listen: 80}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "docker pull") {
		t.Fatalf("une image chargée par MyInfra est tirée d'un registre :\n%s", script)
	}
	if !strings.Contains(script, "'myinfra.local/demo:1.0'") {
		t.Fatal("l'image n'est pas lancée")
	}
}

func TestSyncLogsInBeforeAPrivatePull(t *testing.T) {
	login := &Login{Host: "ghcr.io", User: "me", Password: "tok"}
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{{ID: "7d49736b-58e7-4386-b947-720823531792", Image: "ghcr.io/me/app:2", Listen: 80, Login: login}})
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(script, "docker login 'ghcr.io' -u 'me' --password-stdin")
	pull := strings.Index(script, "docker pull 'ghcr.io/me/app:2'")
	if at < 0 || pull < 0 || at > pull {
		t.Fatalf("connexion absente ou après le pull :\n%s", script)
	}
	// A password that would break the quoting is never written.
	login.Password = "x'; rm -rf / #"
	script, err = Sync("http://127.0.0.1:8080", "tok_en", []Workload{{ID: "7d49736b-58e7-4386-b947-720823531792", Image: "ghcr.io/me/app:2", Listen: 80, Login: login}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "rm -rf") {
		t.Fatal("un mot de passe dangereux a été écrit dans le script")
	}
}

func TestSyncPublishesExposedPorts(t *testing.T) {
	app := Workload{
		ID:      "7d49736b-58e7-4386-b947-720823531792",
		Service: "hbbs",
		Image:   "rustdesk/rustdesk-server:1.1.14",
		Listen:  21115,
		Spec: spec.Service{Ports: []spec.Port{
			{Port: 21115, Expose: 21115},
			{Port: 21116, Protocol: "udp", Expose: 21116},
		}},
	}
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{app})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"-p '21115:21115/tcp'",
		"-p '21116:21116/udp'",
		"tcp dport { 21115 } accept",
		"udp dport { 21116 } accept",
	} {
		if !strings.Contains(script, part) {
			t.Fatalf("missing %s\n%s", part, script)
		}
	}
	// The port has one holder: no new container beside the old one.
	if strings.Contains(script, "-next") {
		t.Fatal(script)
	}
	app.Replicas = 2
	if safeWorkload(app) {
		t.Fatal("two copies share a port of the machine")
	}
}
