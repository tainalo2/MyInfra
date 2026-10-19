package blueprint

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNextWaitsForTheFirstServer(t *testing.T) {
	run := Build(Body{
		ClusterName:  "demo",
		Servers:      []Server{{Name: "un"}, {Name: "deux"}},
		Applications: []App{{Name: "site"}},
	}, "")
	run.Steps[0].Status = "done"
	run.Steps[0].ResourceID = "cluster"
	first := Next(run, false, nil)
	if first.Kind != "server" || run.Steps[first.Index].Name != "un" {
		t.Fatal(first, run.Steps[first.Index])
	}
	run.Steps[first.Index].ResourceID = "srv-1"
	run.Steps[first.Index].Status = "running"
	next := Next(run, false, map[string]string{"srv-1": "deploying"})
	if next.Kind != "server" || run.Steps[next.Index].Name != "deux" {
		t.Fatal(next)
	}
	run.Steps[next.Index].ResourceID = "srv-2"
	run.Steps[next.Index].Status = "running"
	if waited := Next(run, false, map[string]string{"srv-1": "deploying", "srv-2": "deploying"}); waited.Kind != "wait" {
		t.Fatal(waited)
	}
	run.Steps[1].Status = "done"
	run.Steps[2].ResourceID = "srv-2"
	run.Steps[2].Status = "done"
	if next := Next(run, true, map[string]string{"srv-1": "linked", "srv-2": "linked"}); next.Kind != "application" {
		t.Fatal(next)
	}
}

func TestNextStartsAWorkerWhileTheControlPlaneIsStillPending(t *testing.T) {
	run := Build(Body{
		ClusterName: "demo",
		Servers:     []Server{{Name: "worker"}, {Name: "plane", ControlPlane: true}},
	}, "")
	run.Steps[0].Status = "done"
	first := Next(run, false, nil)
	if first.Kind != "server" || run.Steps[first.Index].Name != "worker" {
		t.Fatal(first, run.Steps[first.Index].Name)
	}
	run.Steps[first.Index].ResourceID = "srv-worker"
	run.Steps[first.Index].Status = "running"
	next := Next(run, false, map[string]string{"srv-worker": "deploying"})
	if next.Kind != "server" || run.Steps[next.Index].Name != "plane" {
		t.Fatal(next, run.Steps[next.Index].Name)
	}
}

func TestNextStartsTheOtherControlPlaneWhileTheFirstDeploys(t *testing.T) {
	run := Build(Body{
		ClusterName: "demo",
		Servers:     []Server{{Name: "un", ControlPlane: true}, {Name: "deux", ControlPlane: true}},
	}, "")
	run.Steps[0].Status = "done"
	run.Steps[1].ResourceID = "srv-1"
	run.Steps[1].Status = "running"
	next := Next(run, false, map[string]string{"srv-1": "deploying"})
	if next.Kind != "server" || run.Steps[next.Index].Name != "deux" {
		t.Fatal(next, run.Steps[next.Index].Name)
	}
}

func TestNextCreatesEveryServerWhenALeaderExists(t *testing.T) {
	run := Build(Body{Servers: []Server{{Name: "un"}, {Name: "deux"}}}, "cluster")
	run.Steps[1].ResourceID = "srv-1"
	run.Steps[1].Status = "running"
	next := Next(run, true, map[string]string{"srv-1": "deploying"})
	if next.Kind != "server" || run.Steps[next.Index].Name != "deux" {
		t.Fatal(next, run.Steps[next.Index].Name)
	}
}

func TestNextStopsWhenAServerFails(t *testing.T) {
	run := Build(Body{Servers: []Server{{Name: "un"}}, Applications: []App{{Name: "site"}}}, "cluster")
	run.Steps[1].ResourceID = "srv-1"
	run.Steps[1].Status = "running"
	run.Steps[1].Detail = "Kubernetes"
	if next := Next(run, false, map[string]string{"srv-1": "failed"}); next.Kind != "failed" {
		t.Fatal(next)
	}
}

func TestAppsOnlyStartWhenTheClusterIsReady(t *testing.T) {
	run := Build(Body{Applications: []App{{Name: "site"}}}, "cluster")
	if next := Next(run, true, nil); next.Kind != "application" || run.Steps[next.Index].Name != "site" {
		t.Fatal(next)
	}
}

func TestExportDropsSecretIdentifiers(t *testing.T) {
	doc := Export("démo", Body{
		ClusterName: "demo",
		Servers: []Server{{
			Name: "un", ProviderID: "secret-provider", Provider: "scaleway", ProviderName: "Scaleway",
			Location: "fr-par-1", OfferID: "DEV1-S", Period: "hour", KeyIDs: []string{"secret-key"}, KeyNames: []string{"Paris"},
		}},
		Applications: []App{{Name: "site", SourceKind: "image", Source: "nginx", Names: []Name{{Subdomain: "site", Zone: "exemple.fr"}}}},
	})
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "secret-provider") || strings.Contains(text, "secret-key") || strings.Contains(text, "providerId") || strings.Contains(text, "keyIds") {
		t.Fatal(text)
	}
	if doc.Type != "myinfra-template" || doc.Version != 1 || doc.Servers[0].ProviderAccount != "Scaleway" || doc.Servers[0].Keys[0] != "Paris" {
		t.Fatal(doc)
	}
}

func TestBindMatchesNamesAndClearsUnknownZones(t *testing.T) {
	body, err := Bind(Document{
		Type: "myinfra-template", Version: 1, Name: "démo",
		Servers: []ExportServer{{
			Name: "un", Provider: "scaleway", ProviderAccount: "Scaleway", Location: "fr-par-1", OfferID: "DEV1-S", Period: "hour", Keys: []string{"Paris", "absente"},
		}},
		Applications: []App{{Name: "site", Names: []Name{{Subdomain: "site", Zone: "inconnu.fr"}, {Subdomain: "www", Zone: "exemple.fr"}}}},
	}, []Account{{ID: "prov", Name: "Scaleway", Provider: "scaleway"}}, []Account{{ID: "key", Name: "Paris"}}, []string{"exemple.fr"})
	if err != "" {
		t.Fatal(err)
	}
	if body.Servers[0].ProviderID != "prov" || len(body.Servers[0].KeyIDs) != 1 || body.Servers[0].KeyIDs[0] != "key" {
		t.Fatal(body.Servers[0])
	}
	if body.Applications[0].Names[0].Zone != "" || body.Applications[0].Names[1].Zone != "exemple.fr" {
		t.Fatal(body.Applications[0].Names)
	}
	if ServerReady(body.Servers[0]) {
		t.Fatal("missing key should leave the server incomplete")
	}
}

func TestValidateWantsAnOddNumberOfControlPlanes(t *testing.T) {
	body := Body{ClusterName: "c", Servers: []Server{{Name: "a", ControlPlane: true}, {Name: "b", ControlPlane: true}}}
	if Validate(body) == "" {
		t.Fatal("deux control planes")
	}
	body.Servers = append(body.Servers, Server{Name: "c", ControlPlane: true})
	if msg := Validate(body); msg != "" {
		t.Fatal(msg)
	}
	body.Servers = []Server{{Name: "a", ControlPlane: true}, {Name: "b"}}
	if msg := Validate(body); msg != "" {
		t.Fatal(msg)
	}
}

func TestNextWaitsForStartingApplications(t *testing.T) {
	run := Run{Steps: []Step{{Kind: "application", Name: "a", Status: "running", ResourceID: "x"}, {Kind: "application", Name: "b", Status: "pending"}}}
	if d := Next(run, true, map[string]string{}); d.Kind != "application" || d.Index != 1 {
		t.Fatalf("%+v", d)
	}
	run.Steps[1].ResourceID = "y"
	run.Steps[1].Status = "running"
	if d := Next(run, true, map[string]string{}); d.Kind != "wait" {
		t.Fatalf("%+v", d)
	}
	run.Steps[0].Status, run.Steps[1].Status = "done", "done"
	if d := Next(run, true, map[string]string{}); d.Kind != "done" {
		t.Fatalf("%+v", d)
	}
}

func TestNextDeploysTheAppsWhenOneServerFails(t *testing.T) {
	run := Build(Body{
		Servers:      []Server{{Name: "plane", ControlPlane: true}, {Name: "essai"}, {Name: "worker"}},
		Applications: []App{{Name: "site"}, {Name: "outil"}},
	}, "cluster")
	run.Steps[1].ResourceID, run.Steps[1].Status = "srv-1", "running"
	run.Steps[2].Status, run.Steps[2].Detail = "failed", "Compte en essai."
	run.Steps[3].ResourceID, run.Steps[3].Status = "srv-3", "running"
	if d := Next(run, true, map[string]string{"srv-1": "linked", "srv-3": "deploying"}); d.Kind != "wait" {
		t.Fatalf("attend le dernier serveur : %+v", d)
	}
	run.Steps[1].Status, run.Steps[3].Status = "done", "done"
	d := Next(run, true, map[string]string{"srv-1": "linked", "srv-3": "linked"})
	if d.Kind != "application" || run.Steps[d.Index].Name != "site" {
		t.Fatalf("les applications doivent partir : %+v", d)
	}
	run.Steps[4].ResourceID, run.Steps[4].Status = "app-1", "failed"
	d = Next(run, true, nil)
	if d.Kind != "application" || run.Steps[d.Index].Name != "outil" {
		t.Fatalf("une application en échec ne bloque pas l'autre : %+v", d)
	}
	run.Steps[5].ResourceID, run.Steps[5].Status = "app-2", "done"
	d = Next(run, true, nil)
	if d.Kind != "failed" || d.Index != 2 || d.Detail != "Compte en essai." {
		t.Fatalf("le run finit en échec sur la première étape ratée : %+v", d)
	}
}

func TestNextKeepsCreatingServersAfterAFailure(t *testing.T) {
	run := Build(Body{Servers: []Server{{Name: "essai"}, {Name: "deux"}}}, "cluster")
	run.Steps[1].ResourceID, run.Steps[1].Status = "srv-1", "running"
	d := Next(run, true, map[string]string{"srv-1": "failed"})
	if d.Kind != "server" || run.Steps[d.Index].Name != "deux" {
		t.Fatalf("%+v", d)
	}
}

func TestNextFailsWithoutAControlPlane(t *testing.T) {
	run := Build(Body{Servers: []Server{{Name: "plane", ControlPlane: true}, {Name: "worker"}}, Applications: []App{{Name: "site"}}}, "cluster")
	run.Steps[1].Status = "failed"
	run.Steps[2].ResourceID, run.Steps[2].Status = "srv-2", "done"
	if d := Next(run, false, map[string]string{"srv-2": "linked"}); d.Kind != "failed" || d.Index != 1 {
		t.Fatalf("%+v", d)
	}
}
