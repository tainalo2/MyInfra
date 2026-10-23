package manifest

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"myinfra/internal/spec"
)

const specApp = "7d49736b-58e7-4386-b947-720823531792"

// documents parses each document of a rendering, by kind and name.
func documents(t *testing.T, raw string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, part := range strings.Split(raw, "\n---\n") {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(part), &doc); err != nil {
			t.Fatalf("%v\n%s", err, part)
		}
		meta, _ := doc["metadata"].(map[string]any)
		out[doc["kind"].(string)+"/"+meta["name"].(string)] = doc
	}
	return out
}

func dig(doc any, path ...any) any {
	for _, step := range path {
		switch key := step.(type) {
		case string:
			m, _ := doc.(map[string]any)
			doc = m[key]
		case int:
			list, _ := doc.([]any)
			if key >= len(list) {
				return nil
			}
			doc = list[key]
		}
	}
	return doc
}

func TestRenderKeepsTheBudgetOutOfTheContainer(t *testing.T) {
	raw := Render(App{
		ID:  specApp,
		Env: map[string]string{"A": "b"},
		Services: []Service{
			{Name: "front", Image: "nginx:alpine", Published: true, Replicas: 2, Args: []string{"x"}},
		},
	})
	docs := documents(t, raw)
	container := dig(docs["Deployment/front"], "spec", "template", "spec", "containers", 0)
	if dig(container, "args", 0) != "x" || dig(container, "envFrom", 0, "secretRef", "name") != "env" {
		t.Fatal(raw)
	}
	if dig(docs["PodDisruptionBudget/front"], "spec", "maxUnavailable") != 1 || strings.Contains(raw, "              myinfra.io/service: front\n          args:") {
		t.Fatal(raw)
	}
}

func TestRenderWritesTheSpec(t *testing.T) {
	raw := Render(App{
		ID: specApp,
		Services: []Service{
			{Name: "web", Image: "ghcr.io/acme/app:1", Published: true, Listen: 8080, Disk: true, DiskPath: "/data", Replicas: 1, Spec: spec.Service{
				Command:   []string{"/bin/app", "serve"},
				Ports:     []spec.Port{{Name: "metrics", Port: 9100}, {Port: 9200}},
				Volumes:   []spec.Volume{{Path: "/cache", SizeGi: 4}},
				Files:     []spec.File{{Path: "/etc/app/config.ini", Content: "[main]\nkey = \"value\"\n"}},
				Env:       map[string]string{"MODE": "prod"},
				Resources: &spec.Resources{CPU: "500m", Memory: "512Mi"},
				Health:    &spec.Health{HTTP: "/health"},
				DependsOn: []string{"db"},
			}},
			{Name: "db", Image: "postgres:16", Listen: 5432, Disk: true, Replicas: 1},
		},
	})
	docs := documents(t, raw)
	pod := dig(docs["Deployment/web"], "spec", "template", "spec")
	container := dig(pod, "containers", 0)
	if dig(container, "command", 1) != "serve" || dig(container, "ports", 1, "name") != "metrics" || dig(container, "ports", 2, "name") != "p9200" {
		t.Fatal(raw)
	}
	if dig(container, "readinessProbe", "httpGet", "path") != "/health" || dig(container, "livenessProbe", "httpGet", "port") != 8080 {
		t.Fatal(raw)
	}
	if dig(container, "resources", "requests", "cpu") != "500m" || dig(container, "resources", "limits", "memory") != "512Mi" {
		t.Fatal(raw)
	}
	if dig(container, "env", 0, "name") != "MODE" || dig(container, "volumeMounts", 1, "mountPath") != "/cache" || dig(container, "volumeMounts", 2, "subPath") != "f0" {
		t.Fatal(raw)
	}
	if dig(pod, "volumes", 1, "persistentVolumeClaim", "claimName") != "web.v1" || dig(pod, "volumes", 2, "configMap", "name") != "web.files" {
		t.Fatal(raw)
	}
	wait := dig(pod, "initContainers", 0, "command", 2)
	if s, _ := wait.(string); !strings.Contains(s, "nc -z -w 2 db 5432") {
		t.Fatal(raw)
	}
	if dig(docs["PersistentVolumeClaim/web.v1"], "spec", "resources", "requests", "storage") != "4Gi" {
		t.Fatal(raw)
	}
	if dig(docs["ConfigMap/web.files"], "data", "f0") != "[main]\nkey = \"value\"\n" {
		t.Fatal(raw)
	}
	ports := dig(docs["Service/web"], "spec", "ports")
	if dig(ports, 0, "name") != "main" || dig(ports, 1, "port") != 9100 || dig(ports, 2, "name") != "p9200" {
		t.Fatal(raw)
	}
	if dig(docs["Deployment/web"], "spec", "strategy", "type") != "Recreate" {
		t.Fatal(raw)
	}
}

func TestRenderWritesTheSpecOfStatefulAndBackedUpServices(t *testing.T) {
	stateful := Render(App{
		ID:     specApp,
		Paused: true,
		Services: []Service{
			{Name: "db", Image: "postgres:16", Published: true, Disk: true, Replicas: 3, Spec: spec.Service{
				Volumes: []spec.Volume{{Path: "/wal", SizeGi: 2}},
				Files:   []spec.File{{Path: "/etc/pg.conf", Content: "x"}},
			}},
		},
	})
	docs := documents(t, stateful)
	set := docs["StatefulSet/db"]
	if dig(set, "spec", "volumeClaimTemplates", 1, "metadata", "name") != "v1" || dig(set, "spec", "template", "spec", "volumes", 0, "configMap", "name") != "db.files" {
		t.Fatal(stateful)
	}
	if _, ok := docs["PersistentVolumeClaim/db.v1"]; ok {
		t.Fatal(stateful)
	}
	backed := Render(App{
		ID: specApp,
		Services: []Service{
			{Name: "web", Image: "app:1", Published: true, Disk: true, Spec: spec.Service{
				Volumes:   []spec.Volume{{Path: "/cache"}},
				DependsOn: []string{"cache"},
				Env:       map[string]string{"MODE": "prod"},
			}, Backup: &Backup{Databases: []string{"/data/db.sqlite"}, Bucket: "bucket", KeyID: "GK000000000000000000000000", Secret: strings.Repeat("0", 64), Copying: true, Env: map[string]string{"X": "y"}}},
			{Name: "cache", Image: "redis:7", Listen: 6379},
		},
	})
	docs = documents(t, backed)
	pod := dig(docs["Deployment/web"], "spec", "template", "spec")
	if dig(pod, "initContainers", 0, "name") != "wait-cache" || dig(pod, "containers", 0, "env", 0, "name") != "MODE" || dig(pod, "containers", 0, "env", 1, "name") != "X" {
		t.Fatal(backed)
	}
	if dig(pod, "containers", 0, "volumeMounts", 1, "mountPath") != "/cache" || dig(pod, "volumes", 2, "persistentVolumeClaim", "claimName") != "web.v1" {
		t.Fatal(backed)
	}
	if _, ok := docs["PersistentVolumeClaim/web.v1"]; !ok {
		t.Fatal(backed)
	}
}

func TestDeployableChecksTheSpec(t *testing.T) {
	base := func(s spec.Service) App {
		return App{ID: specApp, Services: []Service{
			{Name: "web", Image: "app:1", Published: true, Listen: 8080, Disk: true, DiskPath: "/data", Spec: s},
			{Name: "db", Image: "postgres:16"},
		}}
	}
	if !Deployable(base(spec.Service{DependsOn: []string{"db"}, Ports: []spec.Port{{Port: 9100}}})) {
		t.Fatal("a sound spec is refused")
	}
	for _, bad := range []spec.Service{
		{DependsOn: []string{"web"}},
		{DependsOn: []string{"cache"}},
		{Ports: []spec.Port{{Port: 8080}}},
		{Ports: []spec.Port{{Name: "main", Port: 9000}}},
		{Volumes: []spec.Volume{{Path: "/data"}}},
		{Volumes: []spec.Volume{{Path: "/a/../etc"}}},
		{Files: []spec.File{{Path: "/etc/x", Content: "a"}, {Path: "/etc/x", Content: "b"}}},
		{Command: []string{"sh", "-c", "echo 'x'"}},
		{Env: map[string]string{"A": "line\nbreak"}},
		{Resources: &spec.Resources{Memory: "1G"}},
		{Health: &spec.Health{HTTP: "health"}},
	} {
		if Deployable(base(bad)) {
			t.Fatalf("%+v is accepted", bad)
		}
	}
}
