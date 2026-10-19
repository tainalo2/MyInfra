package api

import (
	"testing"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
	"myinfra/internal/usage"
)

func TestOverviewPodsPlacesPodsOnTheirServers(t *testing.T) {
	servers := []store.Server{
		{ID: "s1", Name: "SW1", ClusterID: "c1"},
		{ID: "s2", Name: "SW2", ClusterID: "c1"},
		{ID: "s3", Name: "SW1", ClusterID: "c2"},
		{ID: "m1", Name: "alone"},
	}
	apps := []appView{
		{ID: "a1", Name: "metube", ClusterID: "c1"},
		{ID: "a2", Name: "wiki", ServerID: "m1", RunStatus: "running", Services: []serviceView{{Name: "web", Replicas: 2}, {Name: "db"}}},
		{ID: "a3", Name: "off", ServerID: "m1", Paused: true, Services: []serviceView{{Name: "web"}}},
	}
	ns := manifest.Namespace("a1")
	samples := map[string]usage.Sample{"c1": {Pods: []usage.Pod{
		{Namespace: ns, Name: "p1", Node: deploy.NodeName("SW1"), Phase: "Running", Ready: true},
		{Namespace: ns, Name: "p2", Node: deploy.NodeName("SW2"), Phase: "Running"},
		{Namespace: ns, Name: "p3", Node: "gone", Phase: "Pending"},
		{Namespace: "other", Name: "x", Node: deploy.NodeName("SW1")},
	}}}
	pods := overviewPods(servers, apps, samples)
	got := map[string]overviewPod{}
	for _, pod := range pods {
		got[pod.Name] = pod
	}
	if len(pods) != 6 {
		t.Fatalf("pods: %+v", pods)
	}
	if got["p1"].ServerID != "s1" || !got["p1"].Ready || got["p2"].ServerID != "s2" {
		t.Fatalf("cluster pods: %+v", pods)
	}
	if got["p3"].ServerID != "" {
		t.Fatalf("unknown node must have no server: %+v", got["p3"])
	}
	for _, name := range []string{"wiki-web-1", "wiki-web-2", "wiki-db"} {
		pod, ok := got[name]
		if !ok || pod.ServerID != "m1" || pod.Phase != "Running" || !pod.Ready {
			t.Fatalf("%s: %+v", name, pods)
		}
	}
}

func TestAlonePodsFollowAppStatus(t *testing.T) {
	app := appView{ID: "a", Name: "a", ServerID: "m", RunStatus: "failed", Services: []serviceView{{Name: "web"}}}
	if pods := alonePods(app); len(pods) != 1 || pods[0].Phase != "Failed" || pods[0].Ready {
		t.Fatalf("%+v", pods)
	}
	app.RunStatus = "preparing"
	if pods := alonePods(app); pods[0].Phase != "Pending" {
		t.Fatalf("%+v", pods)
	}
}
