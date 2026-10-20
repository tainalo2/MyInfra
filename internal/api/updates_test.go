package api

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
	"myinfra/internal/usage"
)

func TestParseServerCheck(t *testing.T) {
	detail, ok := parseServerCheck("noise\nmyinfra-updates 12 3 yes v1.33.5+k3s1\nmyinfra-system Debian GNU/Linux 13 (trixie)\nmyinfra-kernel 6.12.48-amd64\n")
	if !ok || detail.Packages != 12 || detail.Security != 3 || !detail.Reboot || detail.K3s != "v1.33.5+k3s1" || detail.System != "Debian GNU/Linux 13 (trixie)" || detail.Kernel != "6.12.48-amd64" {
		t.Fatalf("%+v %v", detail, ok)
	}
	detail, ok = parseServerCheck("myinfra-updates 0 0 no -\n")
	if !ok || detail.K3s != "" || detail.Reboot {
		t.Fatalf("machine seule : %+v", detail)
	}
	if _, ok := parseServerCheck("permission denied"); ok {
		t.Fatal("sortie illisible acceptée")
	}
}

func TestNextK3sGoesOneMinorAtATime(t *testing.T) {
	minor, _, _, _ := k3sParts(deploy.K3sVersion)
	channels := map[string]string{}
	for m := minor - 4; m <= minor; m++ {
		channels["v1."+itoa(m)] = "v1." + itoa(m) + ".9+k3s1"
	}
	if got := nextK3s("v1."+itoa(minor-3)+".2+k3s1", channels); got != "v1."+itoa(minor-2)+".9+k3s1" {
		t.Fatalf("une version mineure à la fois : %q", got)
	}
	if got := nextK3s("v1."+itoa(minor)+".0+k3s1", channels); got != deploy.K3sVersion {
		t.Fatalf("même ligne : %q", got)
	}
	if got := nextK3s(deploy.K3sVersion, channels); got != "" {
		t.Fatalf("à jour : %q", got)
	}
	if got := nextK3s("v1."+itoa(minor+1)+".0+k3s1", channels); got != "" {
		t.Fatalf("plus récent que l'épinglée : %q", got)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestPlanUpdateOrdersTheNodes(t *testing.T) {
	now := time.Now()
	servers := []store.Server{
		{ID: "leader", Name: "par-1", ClusterID: "c", ControlPlane: true, CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "agent", Name: "par-2", ClusterID: "c", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "plane", Name: "fsn-1", ClusterID: "c", ControlPlane: true, CreatedAt: now.Add(-time.Hour)},
	}
	details := map[string]serverUpdateDetail{"leader": {K3sNext: "v1.36.5+k3s1"}, "agent": {K3sNext: "v1.36.5+k3s1"}, "plane": {}}
	order := func(steps []updateStep) string {
		var ids []string
		for _, step := range steps {
			if len(ids) == 0 || ids[len(ids)-1] != step.ServerID {
				ids = append(ids, step.ServerID)
			}
		}
		return strings.Join(ids, ",")
	}
	packages := planUpdate(servers, details, false)
	if got := order(packages); got != "agent,plane,leader" {
		t.Fatalf("paquets : %s", got)
	}
	withK3s := planUpdate(servers, details, true)
	if got := order(withK3s); got != "plane,leader,agent" {
		t.Fatalf("k3s : %s", got)
	}
	var actions []string
	for _, step := range withK3s {
		if step.ServerID == "leader" {
			actions = append(actions, step.Action)
		}
	}
	if strings.Join(actions, ",") != "check,drain,packages,k3s,reboot,ready,uncordon,settle" {
		t.Fatalf("étapes : %v", actions)
	}
	alone := planUpdate([]store.Server{{ID: "m", Name: "solo"}}, map[string]serverUpdateDetail{}, true)
	actions = nil
	for _, step := range alone {
		actions = append(actions, step.Action)
	}
	if strings.Join(actions, ",") != "packages,reboot,ready" {
		t.Fatalf("machine seule : %v", actions)
	}
}

func TestRewindStartsTheFailedServerAgain(t *testing.T) {
	steps := []updateStep{
		{ServerID: "a", Action: "packages", Status: "done"},
		{ServerID: "b", Action: "check", Status: "done"},
		{ServerID: "b", Action: "drain", Status: "done"},
		{ServerID: "b", Action: "packages", Status: "failed", Detail: "dpkg"},
		{ServerID: "b", Action: "ready", Status: "pending"},
		{ServerID: "c", Action: "packages", Status: "pending"},
	}
	rewind(steps)
	want := []string{"done", "pending", "pending", "pending", "pending", "pending"}
	for i, step := range steps {
		if step.Status != want[i] || step.Detail != "" {
			t.Fatalf("%d : %+v", i, step)
		}
	}
}

func TestImageImpact(t *testing.T) {
	stateless := store.Application{ID: "a", Name: "web", Services: []store.Service{{Name: "web", Replicas: 1}}}
	if got := imageImpact(stateless); got.Effect != effectNone {
		t.Fatalf("sans disque : %+v", got)
	}
	litestream := store.Application{ID: "b", Name: "kuma", ClusterID: "c", Services: []store.Service{{Name: "kuma", Disk: true, Backup: "ready", Replicas: 1}}}
	if got := imageImpact(litestream); got.Effect != effectShort || got.Reason != "failover" {
		t.Fatalf("litestream : %+v", got)
	}
	disk := store.Application{ID: "c", Name: "wiki", Services: []store.Service{{Name: "wiki", Disk: true, Replicas: 1}, {Name: "front", Replicas: 2}}}
	if got := imageImpact(disk); got.Effect != effectDown || got.Reason != "disk" {
		t.Fatalf("disque : %+v", got)
	}
}

func TestNodeImpactLooksWhereCopiesRun(t *testing.T) {
	server := store.Server{ID: "s1", Name: "par-1", ClusterID: "c"}
	apps := []store.Application{
		{ID: "spread", Name: "spread", ClusterID: "c", Services: []store.Service{{Name: "web", Replicas: 2}}},
		{ID: "single", Name: "single", ClusterID: "c", Services: []store.Service{{Name: "web", Replicas: 1}}},
		{ID: "disk", Name: "disk", ClusterID: "c", Services: []store.Service{{Name: "db", Disk: true, Replicas: 1}}},
		{ID: "away", Name: "away", ClusterID: "c", Services: []store.Service{{Name: "db", Disk: true, Replicas: 1}}},
		{ID: "other", Name: "other", ClusterID: "x", Services: []store.Service{{Name: "web"}}},
	}
	node := deploy.NodeName(server.Name)
	sample := usage.Sample{Pods: []usage.Pod{
		{Namespace: manifest.Namespace("spread"), Service: "web", Node: node},
		{Namespace: manifest.Namespace("spread"), Service: "web", Node: "fsn-1"},
		{Namespace: manifest.Namespace("single"), Service: "web", Node: node},
		{Namespace: manifest.Namespace("disk"), Service: "db", Node: node},
		{Namespace: manifest.Namespace("away"), Service: "db", Node: "fsn-1"},
	}}
	got := map[string]string{}
	for _, item := range nodeImpact(server, apps, sample, true) {
		got[item.ID] = item.Effect
	}
	want := map[string]string{"spread": effectNone, "single": effectShort, "disk": effectDown, "away": effectNone}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for id, effect := range want {
		if got[id] != effect {
			t.Fatalf("%s : %s, attendu %s", id, got[id], effect)
		}
	}
	if view := impactOf(nodeImpact(server, apps, sample, true), "reboot"); view.Effect != effectDown || view.Notes[0] != "reboot" {
		t.Fatalf("%+v", view)
	}
}
