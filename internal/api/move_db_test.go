package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"myinfra/internal/box"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

// A move is refused before any SSH session when the machine is not ready,
// or when leaving would drop the cluster's data or its control plane.
func TestMoveRefusesBeforeSSH(t *testing.T) {
	a, h := tokenFixture(t)
	key := make([]byte, 32)
	b, err := box.New(key)
	if err != nil {
		t.Fatal(err)
	}
	a.Box = b
	rec := call(t, h, http.MethodPost, "/api/secrets/ssh-keys", "", "sess-owner", map[string]string{"name": "clé"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var secret secretView
	if err := json.Unmarshal(rec.Body.Bytes(), &secret); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, cluster := range []struct{ id, name string }{{"sw", "SW"}, {"home", "Maison"}} {
		if err := a.Store.CreateCluster(ctx, "owner", cluster.id, cluster.name); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Store.ImportCluster(ctx, "owner", "old", "Ancien", []byte("c"), []byte("n"), []byte("w")); err != nil {
		t.Fatal(err)
	}
	ready := func(id, name, token, cluster string, plane bool, withKey bool) {
		t.Helper()
		var keys []string
		if withKey {
			keys = []string{secret.ID}
		}
		if err := a.Store.CreateServer(ctx, "owner", id, name, token, "", cluster, "linked", nil, nil, keys, plane); err != nil {
			t.Fatal(err)
		}
		if err := a.Store.SetJoinAddress(ctx, token, "203.0.113.10"); err != nil {
			t.Fatal(err)
		}
	}
	ready("plane", "plan", "tok-plane-123", "sw", true, true)
	ready("worker", "ouvrier", "tok-worker-123", "sw", false, true)
	ready("alone", "seul", "tok-alone-123", "", false, true)
	ready("nokey", "sans-cle", "tok-nokey-123", "", false, false)
	ready("imported", "importe", "tok-import-123", "old", true, true)
	if err := a.Store.CreateServer(ctx, "owner", "pending", "attente", "tok-pending-123", "", "", "pending", nil, nil, []string{secret.ID}, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateServer(ctx, "owner", "noaddr", "sans-adresse", "tok-noaddr-123", "", "", "linked", nil, nil, []string{secret.ID}, false); err != nil {
		t.Fatal(err)
	}
	app := func(id, name, kind, serverID, clusterID string, node string) {
		t.Helper()
		in := store.NewApplication{
			ID: id, OwnerID: "owner", Name: name, SourceKind: "image", Source: "nginx:1",
			DestinationKind: kind, ServerID: serverID, ClusterID: clusterID, Replicas: 1,
			DefaultID: "d-" + id, DefaultHost: id + ".apps.test", Variables: map[string]string{},
		}
		if node != "" {
			in.Services = []store.Service{{
				ID: "svc-" + id, Name: "db", Image: "postgres:16",
				Spec: spec.Service{Volumes: []spec.Volume{{Path: "/data", Disk: "data", Node: node}}},
			}}
		}
		if err := a.Store.CreateApplication(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	app("local", "locale", "server", "alone", "", "")
	app("disk", "disque", "cluster", "", "sw", "worker")
	app("kept", "reste", "cluster", "", "home", "")
	ready("last", "dernier", "tok-last-123", "home", true, true)

	post := func(id, cluster string) *httptestResponse {
		t.Helper()
		rec := call(t, h, http.MethodPost, "/api/servers/"+id+"/cluster", "", "sess-owner", map[string]string{"clusterId": cluster})
		return &httptestResponse{rec.Code, rec.Body.String()}
	}
	// Keep the helper local so a success cannot hide inside a shared client.
	check := func(id, cluster, snippet string, status int) {
		t.Helper()
		got := post(id, cluster)
		if got.code != status || !strings.Contains(got.body, snippet) {
			t.Fatalf("%s -> %s : %d %s", id, cluster, got.code, got.body)
		}
	}
	check("plane", "sw", `"status":"linked"`, http.StatusOK)
	if strings.Contains(post("plane", "sw").body, `"id":"leave"`) {
		t.Fatal("même cluster : un déplacement a démarré")
	}
	check("pending", "sw", "prête", http.StatusConflict)
	check("noaddr", "sw", "adresse", http.StatusConflict)
	check("nokey", "sw", "clé privée", http.StatusConflict)
	check("alone", "sw", "applications", http.StatusConflict)
	check("worker", "", "disque partagé", http.StatusConflict)
	check("plane", "", "seul plan de contrôle", http.StatusConflict)
	check("last", "", "seule machine", http.StatusConflict)
	check("imported", "", "importé", http.StatusConflict)
	ready("free", "libre", "tok-free-123", "", false, true)
	check("free", "old", "importé", http.StatusConflict)
	check("free", "missing", "Choisissez un cluster", http.StatusBadRequest)

	a.Replica = true
	check("free", "home", "réplique", http.StatusConflict)
	a.Replica = false

	row, err := a.Store.Server(ctx, "owner", "free")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "linked" || row.ClusterID != "" {
		t.Fatalf("la machine a bougé : %+v", row)
	}
}

type httptestResponse struct {
	code int
	body string
}
