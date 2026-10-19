package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"myinfra/internal/store"
)

func TestClusterDestructionKeepsTheControlPlaneLast(t *testing.T) {
	steps := clusterSteps([]store.Server{
		{ID: "recent", Name: "recent", ProviderID: "p", ProviderServerID: "machine"},
		{ID: "ancien", Name: "ancien"},
	}, []store.Name{{Hostname: "app.exemple.fr", Zone: "exemple.fr", SecretID: "secret-dns"}}, "clu")
	if len(steps) != 4 {
		t.Fatalf("étapes %d", len(steps))
	}
	if steps[0].Action != "server" || steps[0].Label != "Détruire recent" || steps[0].MachineID != "machine" {
		t.Fatalf("première étape %+v", steps[0])
	}
	if steps[1].Label != "Retirer ancien" || steps[1].MachineID != "" {
		t.Fatalf("deuxième étape %+v", steps[1])
	}
	if steps[2].Action != "dns" || steps[2].Hostname != "app.exemple.fr" {
		t.Fatalf("dns %+v", steps[2])
	}
	if steps[3].Action != "cluster" || steps[3].ClusterID != "clu" {
		t.Fatalf("cluster %+v", steps[3])
	}
}

func TestClusterDestructionRemovesWorkersBeforeControlPlanes(t *testing.T) {
	now := time.Now()
	steps := clusterSteps([]store.Server{
		{ID: "cp-new", Name: "cp-new", ControlPlane: true, CreatedAt: now},
		{ID: "worker", Name: "worker", CreatedAt: now.Add(time.Minute), ProviderID: "p", ProviderServerID: "m"},
		{ID: "cp-old", Name: "cp-old", ControlPlane: true, CreatedAt: now.Add(-time.Minute)},
	}, nil, "clu")
	if steps[0].ServerID != "worker" || steps[1].ServerID != "cp-new" || steps[2].ServerID != "cp-old" {
		t.Fatalf("%+v %+v %+v", steps[0], steps[1], steps[2])
	}
}

func TestServerDestructionDropsNamesBeforeTheMachine(t *testing.T) {
	steps := serverSteps(store.Server{ID: "s", Name: "une", ProviderID: "p", ProviderServerID: "m"}, []store.Name{
		{Hostname: "une.exemple.fr", Zone: "exemple.fr", SecretID: "secret-dns"},
	})
	if len(steps) != 2 || steps[0].Action != "dns" || steps[1].Action != "server" {
		t.Fatalf("%+v", steps)
	}
}

func TestApplicationDestructionEndsWithTheRecord(t *testing.T) {
	steps := appSteps(store.Application{ID: "app", Name: "site", DestinationKind: "cluster"}, []store.Name{
		{Hostname: "site.exemple.fr", Zone: "exemple.fr", SecretID: "secret-dns"},
	})
	if len(steps) != 2 || steps[0].Action != "dns" || steps[1].Action != "application" || steps[1].AppID != "app" {
		t.Fatalf("%+v", steps)
	}
}

func TestDestructionViewHidesAccountIdentifiers(t *testing.T) {
	raw, err := json.Marshal(destructionJob{Steps: []destructionStep{{
		Label: "Retirer app.exemple.fr", Action: "dns", SecretID: "secret-dns", Hostname: "app.exemple.fr", Status: "pending",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := json.Marshal(destructionViewOf(store.Destruction{
		ID: "job", Kind: "application", Name: "site", Status: "running", Body: raw,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(view), "secret-dns") || strings.Contains(string(view), "serverId") {
		t.Fatalf("vue %s", view)
	}
}
