package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
	"myinfra/internal/transfer"
	"myinfra/internal/usage"
)

const statsScript = `set +e
echo '---NODES---'
k3s kubectl get nodes -o json
echo '---PODS---'
k3s kubectl get pods -A -o json
k3s kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | while IFS= read -r n; do
  case "$n" in
    ''|*[!a-zA-Z0-9.-]*) continue ;;
  esac
  echo "---NODE $n---"
  if body=$(timeout 20 k3s kubectl get --raw "/api/v1/nodes/$n/proxy/stats/summary" 2>/tmp/mi-stats.err); then
    printf '%s\n' "$body"
  else
    echo '{}'
    printf '---ERR %s %s---\n' "$n" "$(tr '\n' ' ' </tmp/mi-stats.err | head -c 180)"
  fi
done
`

type gaugeView struct {
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

type serverUse struct {
	ID       string    `json:"id"`
	CPU      gaugeView `json:"cpu"`
	Memory   gaugeView `json:"memory"`
	Disk     gaugeView `json:"disk"`
	Measured bool      `json:"measured"`
}

type clusterUse struct {
	Measured bool        `json:"measured"`
	CPU      gaugeView   `json:"cpu"`
	Memory   gaugeView   `json:"memory"`
	Disk     gaugeView   `json:"disk"`
	Servers  []serverUse `json:"servers"`
}

type podView struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Phase  string `json:"phase"`
	Reason string `json:"reason"`
	Ready  bool   `json:"ready"`
	CPU    uint64 `json:"cpu"`
	Memory uint64 `json:"memory"`
	Disk   uint64 `json:"disk"`
}

type runtimeView struct {
	Measured bool      `json:"measured"`
	CPU      gaugeView `json:"cpu"`
	Memory   gaugeView `json:"memory"`
	Disk     gaugeView `json:"disk"`
	Pods     []podView `json:"pods"`
	Status   string    `json:"status,omitempty"`
}

type serverPodView struct {
	Name        string `json:"name"`
	Application string `json:"application"`
	AppID       string `json:"appId"`
	Service     string `json:"service"`
	Phase       string `json:"phase"`
	Reason      string `json:"reason"`
	Ready       bool   `json:"ready"`
	CPU         uint64 `json:"cpu"`
	Memory      uint64 `json:"memory"`
	Disk        uint64 `json:"disk"`
}

type serverRuntimeView struct {
	Measured bool            `json:"measured"`
	CPU      gaugeView       `json:"cpu"`
	Memory   gaugeView       `json:"memory"`
	Disk     gaugeView       `json:"disk"`
	Pods     []serverPodView `json:"pods"`
}

type statsHit struct {
	at       time.Time // last reading, successful or not
	readAt   time.Time // last reading that reached the cluster
	sample   usage.Sample
	measured bool
}

var (
	statsMu sync.Mutex
	stats   = map[string]statsHit{}
	// statsBusy holds the reading in flight per cluster: every page asking at
	// the same time waits on the same SSH session.
	statsBusy = map[string]chan struct{}{}
)

const (
	// sampleFresh is the age under which a sample is served without reading again.
	sampleFresh = 10 * time.Second
	// sampleFirstWait bounds how long a page waits for a cluster never read yet.
	sampleFirstWait = 3 * time.Second
	// sampleKept is how long a failed reading keeps showing the last good one.
	sampleKept = 2 * time.Minute
)

func (a *API) clusterUsage(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	id := r.PathValue("id")
	if _, ok := a.ownedCluster(r.Context(), owner, id); !ok {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	sample, measured := a.clusterSample(r.Context(), owner, id)
	writeJSON(w, http.StatusOK, a.clusterView(r.Context(), owner, id, sample, measured))
}

func (a *API) serverRuntime(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	server, err := a.Store.Server(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	view := serverRuntimeView{Pods: []serverPodView{}}
	if server.Status != "linked" || server.ClusterID == "" {
		writeJSON(w, http.StatusOK, view)
		return
	}
	sample, measured := a.clusterSample(r.Context(), owner, server.ClusterID)
	node := deploy.NodeName(server.Name)
	for _, item := range sample.Nodes {
		if item.Name != node {
			continue
		}
		view.Measured = measured && item.Measured
		view.CPU = gaugeView{Used: item.CPU.Used, Total: item.CPU.Total}
		view.Memory = gaugeView{Used: item.Memory.Used, Total: item.Memory.Total}
		view.Disk = gaugeView{Used: item.Disk.Used, Total: item.Disk.Total}
	}
	apps, err := a.Store.ListApplications(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	byNS := map[string]store.Application{}
	for _, app := range apps {
		if app.ClusterID == server.ClusterID {
			byNS[manifest.Namespace(app.ID)] = app
		}
	}
	if self, ok := a.selfRecord(r.Context()); ok && self.ClusterID == server.ClusterID {
		byNS[transfer.PodNamespace] = self
	}
	for _, pod := range sample.Pods {
		if pod.Node != node {
			continue
		}
		app, ok := byNS[pod.Namespace]
		if !ok {
			continue
		}
		view.Pods = append(view.Pods, serverPodView{
			Name: pod.Name, Application: app.Name, AppID: app.ID, Service: pod.Service,
			Phase: pod.Phase, Reason: pod.Reason, Ready: pod.Ready,
			CPU: pod.CPU, Memory: pod.Memory, Disk: pod.Disk,
		})
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) applicationRuntime(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == selfAppID {
		a.selfRuntime(w, r)
		return
	}
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	view := runtimeView{Pods: []podView{}, Status: app.RunStatus}
	if app.ClusterID == "" {
		if app.ServerID != "" {
			view.Pods, view.Measured = a.machineContainers(r.Context(), ownerID(r.Context()), app.ServerID, app.ID)
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	sample, measured := a.clusterSample(r.Context(), ownerID(r.Context()), app.ClusterID)
	view.Measured = measured
	servers, _ := a.Store.ListServers(r.Context(), ownerID(r.Context()))
	ns := manifest.Namespace(app.ID)
	nodes := map[string]bool{}
	for _, pod := range sample.Pods {
		if pod.Namespace != ns {
			continue
		}
		view.Pods = append(view.Pods, podView{
			Name: pod.Name, Server: serverName(servers, pod.Node), Phase: pod.Phase, Reason: pod.Reason,
			Ready: pod.Ready, CPU: pod.CPU, Memory: pod.Memory, Disk: pod.Disk,
		})
		view.CPU.Used += pod.CPU
		view.Memory.Used += pod.Memory
		view.Disk.Used += pod.Disk
		if pod.Node != "" {
			nodes[pod.Node] = true
		}
	}
	view.Status = a.noteAppHealth(r.Context(), app, sample)
	for _, node := range sample.Nodes {
		if !nodes[node.Name] {
			continue
		}
		view.CPU.Total += node.CPU.Total
		view.Memory.Total += node.Memory.Total
		view.Disk.Total += node.Disk.Total
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) clusterView(ctx context.Context, owner, clusterID string, sample usage.Sample, measured bool) clusterUse {
	servers, _ := a.Store.ListServers(ctx, owner)
	view := clusterUse{Measured: measured, Servers: []serverUse{}}
	for _, server := range servers {
		if server.ClusterID != clusterID {
			continue
		}
		row := serverUse{ID: server.ID}
		for _, node := range sample.Nodes {
			if node.Name != deploy.NodeName(server.Name) {
				continue
			}
			row.CPU = gaugeView{Used: node.CPU.Used, Total: node.CPU.Total}
			row.Memory = gaugeView{Used: node.Memory.Used, Total: node.Memory.Total}
			row.Disk = gaugeView{Used: node.Disk.Used, Total: node.Disk.Total}
			row.Measured = node.Measured
		}
		view.Servers = append(view.Servers, row)
	}
	for _, node := range sample.Nodes {
		if !node.Measured {
			continue
		}
		view.CPU.Used += node.CPU.Used
		view.CPU.Total += node.CPU.Total
		view.Memory.Used += node.Memory.Used
		view.Memory.Total += node.Memory.Total
		view.Disk.Used += node.Disk.Used
		view.Disk.Total += node.Disk.Total
	}
	return view
}

func (a *API) ownedCluster(ctx context.Context, owner, id string) (store.Cluster, bool) {
	rows, err := a.Store.ListClusters(ctx, owner)
	if err != nil {
		return store.Cluster{}, false
	}
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return store.Cluster{}, false
}

// clusterSample answers from the last sample at once. A stale one is read
// again in the background: a page never waits for SSH, except the first time
// a cluster is read, and then no more than sampleFirstWait.
func (a *API) clusterSample(ctx context.Context, owner, clusterID string) (usage.Sample, bool) {
	statsMu.Lock()
	hit, ok := stats[clusterID]
	if ok && time.Since(hit.at) < sampleFresh {
		statsMu.Unlock()
		return hit.sample, hit.measured
	}
	done := a.refreshSample(owner, clusterID)
	statsMu.Unlock()
	if ok {
		return hit.sample, hit.measured
	}
	timer := time.NewTimer(sampleFirstWait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		return usage.Sample{}, false
	case <-ctx.Done():
		return usage.Sample{}, false
	}
	statsMu.Lock()
	hit = stats[clusterID]
	statsMu.Unlock()
	return hit.sample, hit.measured
}

// refreshSample starts reading a cluster unless a reading is already in
// flight, and returns what to wait on. statsMu must be held.
func (a *API) refreshSample(owner, clusterID string) chan struct{} {
	if done, busy := statsBusy[clusterID]; busy {
		return done
	}
	done := make(chan struct{})
	statsBusy[clusterID] = done
	go func() {
		defer close(done)
		sample, measured := a.readSample(context.Background(), owner, clusterID)
		now := time.Now()
		statsMu.Lock()
		defer statsMu.Unlock()
		delete(statsBusy, clusterID)
		previous, had := stats[clusterID]
		if len(sample.Nodes) == 0 && len(sample.Pods) == 0 && had && now.Sub(previous.readAt) < sampleKept {
			// The cluster did not answer this time: keep showing the last
			// reading for a while instead of pods that vanish and come back.
			previous.at = now
			stats[clusterID] = previous
			return
		}
		hit := statsHit{at: now, sample: sample, measured: measured}
		if len(sample.Nodes) > 0 || len(sample.Pods) > 0 {
			hit.readAt = now
		}
		stats[clusterID] = hit
	}()
	return done
}

// WarmSamples reads every cluster once at startup, so that the first pages
// already have pods and meters to show.
func (a *API) WarmSamples(ctx context.Context) {
	leaders, err := a.Store.ClusterLeaders(ctx)
	if err != nil {
		return
	}
	statsMu.Lock()
	defer statsMu.Unlock()
	for _, leader := range leaders {
		a.refreshSample(leader.OwnerID, leader.ClusterID)
	}
}

func (a *API) readSample(ctx context.Context, owner, clusterID string) (usage.Sample, bool) {
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return usage.Sample{}, false
	}
	var leader *store.Server
	for i := range servers {
		server := &servers[i]
		if server.ClusterID != clusterID || !server.ControlPlane || server.Status != "linked" || server.Address == "" {
			continue
		}
		if leader == nil || server.CreatedAt.Before(leader.CreatedAt) {
			leader = server
		}
	}
	if leader == nil {
		return usage.Sample{}, false
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, leader.ID)
	if err != nil {
		return usage.Sample{}, false
	}
	key := a.privateKey(ctx, owner, keyIDs)
	if key == "" {
		return usage.Sample{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	raw, err := deploy.SSH(ctx, deploy.Host{Address: leader.Address, PrivateKey: key}, statsScript)
	if err != nil || raw == "" {
		slog.Info("mesures", "err", err, "len", len(raw))
		return usage.Sample{}, false
	}
	if idx := strings.Index(raw, "---ERR "); idx >= 0 {
		end := idx + 220
		if end > len(raw) {
			end = len(raw)
		}
		slog.Info("mesures", "detail", raw[idx:end])
	}
	sample, err := usage.Parse(raw)
	if err != nil {
		slog.Info("mesures", "err", err, "len", len(raw))
		return usage.Sample{}, false
	}
	measured := false
	for _, node := range sample.Nodes {
		if node.Measured {
			measured = true
			break
		}
	}
	return sample, measured
}

func serverName(servers []store.Server, node string) string {
	for _, server := range servers {
		if deploy.NodeName(server.Name) == node {
			return server.Name
		}
	}
	if node == "" {
		return "—"
	}
	return node
}
