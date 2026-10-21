package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A copy behind the default certificate of Traefik answers, but not over a
// trusted connection: the transfer says it waits for the certificate.
func TestWaitHealthTellsAMissingCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	var mu sync.Mutex
	var states []string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitHealth(ctx, srv.URL, nil, func(state string) {
		mu.Lock()
		states = append(states, state)
		mu.Unlock()
	})
	if err == nil || !strings.Contains(err.Error(), "certificat") {
		t.Fatalf("%v", err)
	}
	if len(states) == 0 || states[0] != "certificate" {
		t.Fatalf("%v", states)
	}
}

func TestZoneOfURL(t *testing.T) {
	if got := zoneOfURL("https://myinfra.devisgenerator.fr"); got != "devisgenerator.fr" {
		t.Fatalf("%q", got)
	}
	if got := zoneOfURL("not a url"); got != "" {
		t.Fatalf("%q", got)
	}
}

// Only a copy the new one can reach is given to it as a peer: a laptop syncs
// by itself.
func TestPeersForNewCopy(t *testing.T) {
	for _, c := range []struct {
		url  string
		keep bool
		want int
	}{
		{"http://127.0.0.1:8080", true, 0},
		{"http://192.168.1.10:8080", true, 0},
		{"https://myinfra.example.com", true, 1},
		{"https://myinfra.example.com", false, 0},
	} {
		a := &API{PublicURL: c.url}
		a.setTransferKeep(c.keep)
		if got := a.peersForNewCopy(); len(got) != c.want {
			t.Errorf("%s keep=%v : %v", c.url, c.keep, got)
		}
	}
}

func TestPodProgress(t *testing.T) {
	raw := `{"items":[
	 {"metadata":{"name":"database-1","labels":{"cnpg.io/cluster":"database"}},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
	 {"metadata":{"name":"database-2","labels":{"cnpg.io/cluster":"database"}},"status":{"phase":"Pending","conditions":[{"type":"PodScheduled","status":"False","message":"0/1 nodes are available"}]}},
	 {"metadata":{"name":"myinfra-abc","labels":{}},"status":{"phase":"Pending","initContainerStatuses":[{"name":"database","state":{"running":{}}}]}},
	 {"metadata":{"name":"myinfra-def","labels":{}},"status":{"phase":"Pending","containerStatuses":[{"name":"myinfra","state":{"waiting":{"reason":"ImagePullBackOff","message":"not found"}}}]}}
	]}`
	got := map[string]podState{}
	for _, pod := range podProgress(raw) {
		got[pod.Name] = pod
	}
	want := map[string][2]string{
		"database-1":  {"ready", "ok"},
		"myinfra-abc": {"database", "wait"},
		"myinfra-def": {"ImagePullBackOff (not found)", "bad"},
	}
	for name, w := range want {
		if got[name].State != w[0] || got[name].Tone != w[1] {
			t.Fatalf("%s : %+v", name, got[name])
		}
	}
	if !strings.HasPrefix(got["database-2"].State, "unschedulable") || got["database-2"].Role != "database" {
		t.Fatalf("%+v", got["database-2"])
	}
	if pods := podProgress(`{"items":[]}`); pods == nil || len(pods) != 0 {
		t.Fatal(pods)
	}
}

// The copy is reached at its cluster's addresses, whatever the machine's
// resolver answers for its name.
func TestCopyClientDialsTheClusterAddresses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "" && !strings.HasPrefix(r.Host, "myinfra.invalid") {
			t.Errorf("host %q", r.Host)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	base := "https://myinfra.invalid:" + port
	client := copyClient(base, []string{"127.0.0.1"}, true, 5*time.Second)
	res, err := client.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode)
	}
}
