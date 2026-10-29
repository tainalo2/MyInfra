package agent

import (
	"os/exec"
	"strings"
	"testing"

	"myinfra/internal/spec"
)

const specApp = "7d49736b-58e7-4386-b947-720823531792"

func TestSyncRunsTheSpec(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{
		{ID: specApp, Service: "web", Image: "ghcr.io/acme/app:1", Published: true, Env: map[string]string{"MODE": "app"}, Spec: spec.Service{
			Command:   []string{"/bin/app", "serve"},
			Volumes:   []spec.Volume{{Path: "/cache", SizeGi: 2}},
			Files:     []spec.File{{Path: "/etc/app.ini", Content: "a = 'b'\n$(reboot)\n"}},
			Env:       map[string]string{"MODE": "spec", "LEVEL": "debug"},
			Resources: &spec.Resources{CPU: "0.5", Memory: "512Mi"},
			DependsOn: []string{"db"},
		}},
		{ID: specApp, Service: "db", Image: "postgres:16", Listen: 5432, Disk: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"--entrypoint '/bin/app' ",
		" -v 'myinfra-data-7d49736b58e74386b947720823531792-web-v1:/cache'",
		" -v '/var/lib/myinfra/files/7d49736b58e74386b947720823531792/web/f0:/etc/app.ini:ro'",
		" -e 'LEVEL=debug'",
		" --cpus '0.500' --memory '512m' 'ghcr.io/acme/app:1' 'serve'",
		"| base64 -d > '/var/lib/myinfra/files/7d49736b58e74386b947720823531792/web/f0'",
	} {
		if !strings.Contains(script, part) {
			t.Fatalf("missing %s\n%s", part, script)
		}
	}
	if strings.Contains(script, "MODE=spec") || strings.Contains(script, "$(reboot)") {
		t.Fatal(script)
	}
	if strings.Index(script, "--network-alias 'db'") > strings.Index(script, "--network-alias 'web'") {
		t.Fatal("db must start before web")
	}
	gpu, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{
		{ID: specApp, Service: "web", Image: "ghcr.io/acme/app:1", Spec: spec.Service{Resources: &spec.Resources{GPU: 1}}},
	})
	if err != nil || !strings.Contains(gpu, " --gpus 1 ") {
		t.Fatal(gpu, err)
	}
	if err := exec.Command("sh", "-n", "-c", script).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncKeepsTheLabelOfAServiceWithoutSpec(t *testing.T) {
	app := Workload{ID: specApp, Service: "web", Image: "nginx:alpine", Published: true}
	if strings.Count(specOf(app), "|") != 6 {
		t.Fatal(specOf(app))
	}
	app.Spec.Env = map[string]string{"A": "b"}
	if strings.Count(specOf(app), "|") != 7 {
		t.Fatal(specOf(app))
	}
}

func TestSyncRefusesABrokenSpec(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{
		{ID: specApp, Service: "web", Image: "nginx:alpine", Spec: spec.Service{Command: []string{"sh", "-c", "echo 'x'"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "report '"+specApp+"' failed") || strings.Contains(script, "echo 'x'") {
		t.Fatal(script)
	}
}
