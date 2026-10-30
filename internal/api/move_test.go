package api

import (
	"strings"
	"testing"

	"myinfra/internal/deploy"
	"myinfra/internal/store"
)

func TestMoveCanTakeTheClusterFirewall(t *testing.T) {
	if !firewallWhileDeploying(deploy.MoveSteps("", "home", false)) {
		t.Fatal("un déplacement attend les clés qu'il ne refait pas")
	}
	if firewallWhileDeploying(deploy.Steps(true)) {
		t.Fatal("le premier déploiement n'a pas encore déposé ses clés")
	}
	if !firewallWhileDeploying(deploy.Mark(deploy.Steps(true), "keys", "done", "")) {
		t.Fatal("les clés sont en place")
	}
}

func TestLeaveAloneOpensForwarding(t *testing.T) {
	if !strings.Contains(leaveAloneScript, "iptables -P FORWARD ACCEPT") {
		t.Fatal(leaveAloneScript)
	}
}

func TestRefuseMove(t *testing.T) {
	server := store.Server{ID: "s", Name: "maison", Status: "linked", Address: "203.0.113.10", ClusterID: "sw", ControlPlane: true}
	worker := store.Server{ID: "w", Status: "linked", ClusterID: "sw"}
	otherPlane := store.Server{ID: "p", Status: "linked", ClusterID: "sw", ControlPlane: true}
	alone := server
	alone.ClusterID = ""
	alone.ControlPlane = false

	cases := []struct {
		name  string
		facts moveFacts
		want  string
	}{
		{name: "seul vers cluster", facts: moveFacts{Server: alone, TargetID: "sw", TargetFound: true, TargetOrigin: "created"}, want: ""},
		{name: "applications locales", facts: moveFacts{Server: alone, TargetID: "sw", TargetFound: true, TargetOrigin: "created", ServerApps: 1}, want: moveHasApps},
		{name: "cluster inconnu", facts: moveFacts{Server: alone, TargetID: "nope"}, want: movePick},
		{name: "cible importée", facts: moveFacts{Server: alone, TargetID: "old", TargetFound: true, TargetOrigin: "imported"}, want: moveImported},
		{name: "source importée", facts: moveFacts{Server: server, TargetID: "", SourceOrigin: "imported"}, want: moveImported},
		{name: "disque", facts: moveFacts{Server: worker, TargetID: "", SourceOrigin: "created", Servers: []store.Server{worker, otherPlane}, HoldsDisk: true}, want: moveDisk},
		{name: "seul plan", facts: moveFacts{Server: server, TargetID: "", SourceOrigin: "created", Servers: []store.Server{server, worker}}, want: moveOnlyPlane},
		{name: "autre plan", facts: moveFacts{Server: server, TargetID: "b", TargetFound: true, TargetOrigin: "created", SourceOrigin: "created", Servers: []store.Server{server, otherPlane}}, want: ""},
		{name: "seule machine avec applications", facts: moveFacts{Server: server, TargetID: "", SourceOrigin: "created", Servers: []store.Server{server}, ClusterApps: 2}, want: moveOnlyNode},
		{name: "seule machine qui porte MyInfra", facts: moveFacts{Server: server, TargetID: "b", TargetFound: true, TargetOrigin: "created", SourceOrigin: "created", Servers: []store.Server{server}, HostsSelf: true}, want: moveOnlyNode},
		{name: "cluster vide", facts: moveFacts{Server: server, TargetID: "", SourceOrigin: "created", Servers: []store.Server{server}}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := refuseMove(tc.facts); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
