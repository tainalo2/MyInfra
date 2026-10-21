package manifest

import (
	"strings"
	"testing"
)

func TestRenderPublishesOnlyThePublicService(t *testing.T) {
	raw := Render(App{
		ID:    "7d49736b-58e7-4386-b947-720823531792",
		Hosts: []string{"app.exemple.fr"},
		Env:   map[string]string{"POSTGRES_PASSWORD": "secret"},
		Services: []Service{
			{Name: "front", Image: "nginx:alpine", Published: true, Replicas: 2},
			{Name: "back", Image: "ghcr.io/acme/api:1", Replicas: 2},
			{Name: "postgres", Image: "postgres:16", Disk: true, Replicas: 3},
		},
	})
	if !strings.Contains(raw, "kind: IngressRoute") || !strings.Contains(raw, "Host(`app.exemple.fr`)") {
		t.Fatal(raw)
	}
	if strings.Count(raw, "kind: IngressRoute") != 2 || !strings.Contains(raw, "entryPoints: [websecure]") || !strings.Contains(raw, "certResolver: exemple-fr") || !strings.Contains(raw, "redirectScheme") || !strings.Contains(raw, "!PathPrefix(`/.well-known/acme-challenge/`)") {
		t.Fatal(raw)
	}
	proxy := Proxy([]string{"exemple.fr"})
	if !strings.Contains(proxy, "tlsChallenge: true") || !strings.Contains(proxy, "fsGroup: 65532") || !strings.Contains(proxy, `email: "certif@exemple.fr"`) || strings.Contains(proxy, "redirections.entryPoint") {
		t.Fatal(proxy)
	}
	if !strings.Contains(raw, "name: postgres") || !strings.Contains(raw, "kind: StatefulSet") || !strings.Contains(raw, "volumeClaimTemplates") || !strings.Contains(raw, "replicas: 3") {
		t.Fatal(raw)
	}
	if strings.Contains(raw, "kind: PersistentVolumeClaim") || strings.Contains(raw, "type: Recreate") {
		t.Fatal(raw)
	}
	paused := Render(App{
		ID: "7d49736b-58e7-4386-b947-720823531792", Paused: true,
		Services: []Service{
			{Name: "web", Image: "nginx:alpine", Published: true, Disk: true, Replicas: 1},
			{Name: "postgres", Image: "postgres:16", Disk: true, Replicas: 3},
		},
	})
	if !strings.Contains(paused, "kind: PersistentVolumeClaim") || !strings.Contains(paused, "kind: StatefulSet") || strings.Contains(paused, "replicas: 1") || strings.Contains(paused, "replicas: 3") || !strings.Contains(paused, "replicas: 0") {
		t.Fatal(paused)
	}
	if strings.Contains(raw, "kind: Application") || strings.Contains(raw, "argoproj") {
		t.Fatal(raw)
	}
}

func TestRenderUsesTheDeclaredPortAndDisk(t *testing.T) {
	raw := Render(App{
		ID: "7d49736b-58e7-4386-b947-720823531792",
		Services: []Service{
			{Name: "web", Image: "wordpress:6-apache", Published: true, Listen: 80, Disk: true, DiskPath: "/var/www/html", DiskGi: 8},
			{Name: "mariadb", Image: "mariadb:11", Listen: 3306, Disk: true, DiskPath: "/var/lib/mysql", DiskGi: 8},
		},
	})
	if !strings.Contains(raw, "containerPort: 3306") || !strings.Contains(raw, "port: 3306") || !strings.Contains(raw, "mountPath: /var/lib/mysql") || !strings.Contains(raw, "storage: 8Gi") || !strings.Contains(raw, "mountPath: /var/www/html") {
		t.Fatal(raw)
	}
}

func TestRenderKeepsASingleDiskOnOneVolume(t *testing.T) {
	raw := Render(App{
		ID: "7d49736b-58e7-4386-b947-720823531792",
		Services: []Service{
			{Name: "postgres", Image: "postgres:16", Published: true, Disk: true, Replicas: 1},
		},
	})
	if !strings.Contains(raw, "kind: Deployment") || !strings.Contains(raw, "kind: PersistentVolumeClaim") || !strings.Contains(raw, "replicas: 1") || !strings.Contains(raw, "type: Recreate") {
		t.Fatal(raw)
	}
	if strings.Contains(raw, "kind: StatefulSet") || strings.Contains(raw, "volumeClaimTemplates") {
		t.Fatal(raw)
	}
}

func TestProxySeedsCertificatesBeforeTraefik(t *testing.T) {
	raw := Proxy([]string{"devisgenerator.fr"})
	for _, want := range []string{"initContainers:", "secretName: " + CertificateSecret, "optional: true", "mountPath: /seed", "name: data", "tolerationSeconds: 30"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("manque %q\n%s", want, raw)
		}
	}
	secret := CertificateFiles(map[string][]byte{"acme-devisgenerator-fr.json": []byte("{}")})
	if !strings.Contains(secret, "acme-devisgenerator-fr.json: e30=") || !strings.Contains(secret, "namespace: kube-system") {
		t.Fatal(secret)
	}
	if !strings.Contains(CertificateFiles(nil), "data: {}") {
		t.Fatal("un Secret vide doit rester valide")
	}
}

func TestBackedUpServiceRestartsAnywhere(t *testing.T) {
	svc := Service{Name: "uptime-kuma", Image: "louislam/uptime-kuma:2.5.6", Published: true, Disk: true, DiskPath: "/app/data", Listen: 3001, Replicas: 1,
		Backup: &Backup{Databases: []string{"/app/data/kuma.db"}, Bucket: "myinfra-x", KeyID: "GK0123456789abcdef01234567", Secret: strings.Repeat("ab", 32)}}
	app := App{ID: "app-1", Services: []Service{svc}}
	if !Deployable(app) {
		t.Fatal("refusé")
	}
	outside := svc
	outside.Backup = &Backup{Databases: []string{"/etc/passwd"}, Bucket: "myinfra-x", KeyID: svc.Backup.KeyID, Secret: svc.Backup.Secret}
	if Deployable(App{ID: "app-1", Services: []Service{outside}}) {
		t.Fatal("un fichier hors du disque ne doit pas être répliqué")
	}
	raw := Render(app)
	for _, want := range []string{"emptyDir: {}", "restore, -if-db-not-exists, -if-replica-exists", "args: [replicate", "type: Recreate", "tolerationSeconds: 30", "path: uptime-kuma/kuma.db", "bucket: myinfra-x"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("manque %q\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "PersistentVolumeClaim") {
		t.Fatal("prête, la copie ne doit plus dépendre d'un disque de nœud")
	}
	offsite := svc
	offsite.Backup = &Backup{
		Databases: svc.Backup.Databases, Bucket: svc.Backup.Bucket, KeyID: svc.Backup.KeyID, Secret: svc.Backup.Secret,
		Offsite: &S3Replica{Endpoint: "https://s3.example.com", Bucket: "offsite", Region: "eu", Path: "clusters/c1/apps/a1", KeyID: "AKIAOFFSITE", Secret: strings.Repeat("cd", 32)},
	}
	rawOff := Render(App{ID: "app-1", Services: []Service{offsite}})
	for _, want := range []string{"LITESTREAM_OFFSITE_ACCESS_KEY_ID", "bucket: offsite", "path: clusters/c1/apps/a1/uptime-kuma/kuma.db", "endpoint: https://s3.example.com"} {
		if !strings.Contains(rawOff, want) {
			t.Fatalf("hors cluster : manque %q\n%s", want, rawOff)
		}
	}
	svc.Backup.Copying = true
	raw = Render(App{ID: "app-1", Services: []Service{svc}})
	if !strings.Contains(raw, "claimName: uptime-kuma") || strings.Contains(raw, "restore-0") {
		t.Fatalf("pendant la première copie, le disque du nœud reste la source\n%s", raw)
	}
}

func TestStorageIsNotAnApplicationNamespace(t *testing.T) {
	raw := Storage(strings.Repeat("ab", 32))
	if strings.Contains(raw, "myinfra.io/managed") {
		t.Fatal("la livraison supprimerait le stockage")
	}
	if !strings.Contains(raw, "replication_factor = 3") || !strings.Contains(raw, "kubernetes_discovery") {
		t.Fatal(raw)
	}
}

func TestDatabaseServiceAndMigration(t *testing.T) {
	svc := Service{Name: "vikunja", Image: "vikunja/vikunja:latest", Published: true, Listen: 3456, Replicas: 3,
		Env:       map[string]string{"VIKUNJA_DATABASE_TYPE": "postgres", "VIKUNJA_DATABASE_HOST": "vikunja-db-rw"},
		SecretEnv: map[string]SecretKey{"VIKUNJA_DATABASE_PASSWORD": {Secret: "vikunja-db-app", Key: "password"}}}
	app := App{ID: "app-2", Env: map[string]string{"VIKUNJA_DATABASE_TYPE": "sqlite"}, Services: []Service{svc},
		Databases: []Database{{Name: "vikunja-db", Database: "vikunja", Instances: 2}},
		Secrets:   []SharedSecret{{Name: "vikunja-files", Data: map[string]string{"accessKey": "GK1"}}}}
	if !Deployable(app) {
		t.Fatal("refusé")
	}
	raw := Render(app)
	for _, want := range []string{"kind: Cluster", "instances: 2", "podAntiAffinityType: required", "tolerationSeconds: 30", "replicas: 3", "secretKeyRef:", "name: vikunja-db-app", "name: vikunja-files"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("manque %q\n%s", want, raw)
		}
	}
	disk := svc
	disk.Disk, disk.DiskPath, disk.Env, disk.SecretEnv = true, "/app/vikunja/files", nil, nil
	job := MigrationJob(app, Migration{Service: disk,
		Dump:    []string{"/app/vikunja/vikunja", "dump", "-p", "{dir}", "-f", "dump.zip"},
		Restore: []string{"/app/vikunja/vikunja", "restore", "--preserve-config", "{dir}/dump.zip"},
		Tables:  []string{"users", "tasks"}, Database: "vikunja-db", RestoreInput: "Yes, I understand\n", ToolEnv: map[string]string{"VIKUNJA_SERVICE_ROOTPATH": "/app/vikunja"},
		Env: svc.Env, SecretEnv: svc.SecretEnv})
	for _, want := range []string{"kind: Job", `/image/app/vikunja/vikunja`, `'/app/vikunja/files/myinfra-migration/dump.zip'`, "reference: vikunja/vikunja:latest", "Yes, I understand", "VIKUNJA_SERVICE_ROOTPATH", "claimName: vikunja", "name: restore", "backoffLimit: 0",
		"name: check-dump", "name: verify", "&& printf %s", "name: vikunja-db-app", "key: uri", "chmod 777", "for t in users tasks"} {
		if !strings.Contains(job, want) {
			t.Fatalf("manque %q\n%s", want, job)
		}
	}
}

func TestRenderGivesPrivateRegistryLoginToPods(t *testing.T) {
	app := App{ID: "7d49736b-58e7-4386-b947-720823531792", Pull: `{"auths":{"ghcr.io":{"auth":"bWU6dG9r"}}}`,
		Services: []Service{{Name: "web", Image: "ghcr.io/me/app:2", Published: true, Replicas: 1, Listen: 80}}}
	raw := Render(app)
	for _, want := range []string{"type: kubernetes.io/dockerconfigjson", "name: " + PullSecret, "kind: ServiceAccount", "imagePullSecrets:"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("absent : %q\n%s", want, raw)
		}
	}
	app.Pull = ""
	if strings.Contains(Render(app), PullSecret) {
		t.Fatal("secret de connexion rendu sans registre privé")
	}
}

func TestRenderKeepsCopiesAnsweringDuringAnUpdate(t *testing.T) {
	raw := Render(App{
		ID: "7d49736b-58e7-4386-b947-720823531792",
		Services: []Service{
			{Name: "front", Image: "nginx:1.27@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Published: true, Replicas: 2},
			{Name: "worker", Image: "ghcr.io/acme/worker:1", Replicas: 1},
			{Name: "postgres", Image: "postgres:16", Listen: 5432, Disk: true, Replicas: 3},
		},
	})
	for _, want := range []string{
		"maxUnavailable: 0\n      maxSurge: 1",
		"kind: PodDisruptionBudget\nmetadata:\n  name: front",
		"kind: PodDisruptionBudget\nmetadata:\n  name: postgres",
		"tcpSocket:\n              port: 80",
		"tcpSocket:\n              port: 5432",
		"tolerationSeconds: 30",
		"image: nginx:1.27@sha256:",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("%q absent :\n%s", want, raw)
		}
	}
	// A worker without a port is not probed, and one copy has no budget.
	if strings.Contains(raw, "name: worker\n  namespace: myinfra-7d49736b-58e7-4386-b947-720823531792\nspec:\n  maxUnavailable") || strings.Count(raw, "readinessProbe") != 2 {
		t.Fatal(raw)
	}
	if strings.Count(raw, "kind: PodDisruptionBudget") != 2 {
		t.Fatal(raw)
	}
}

func TestRenderRestartsCopiesWhenVariablesChange(t *testing.T) {
	app := App{ID: "7d49736b-58e7-4386-b947-720823531792", Env: map[string]string{"MODE": "a"},
		Services: []Service{{Name: "web", Image: "nginx:1.27", Published: true, Replicas: 2}}}
	first := Render(app)
	app.Env = map[string]string{"MODE": "b"}
	second := Render(app)
	if !strings.Contains(first, "myinfra.io/config:") || first == second {
		t.Fatal(first)
	}
	if Render(app) != second {
		t.Fatal("le rendu change sans changement de variables")
	}
}
