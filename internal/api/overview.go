package api

import (
	"net/http"
	"strconv"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
	"myinfra/internal/transfer"
	"myinfra/internal/usage"
)

type overviewServer struct {
	serverView
	Usage *serverUse `json:"usage,omitempty"`
}

type overviewPod struct {
	Name     string `json:"name"`
	AppID    string `json:"appId"`
	ServerID string `json:"serverId,omitempty"`
	Service  string `json:"service,omitempty"`
	Phase    string `json:"phase"`
	Reason   string `json:"reason,omitempty"`
	Ready    bool   `json:"ready"`
}

type overviewView struct {
	Servers []overviewServer `json:"servers"`
	Apps    []appView        `json:"apps"`
	Pods    []overviewPod    `json:"pods"`
}

// overview gathers servers, apps and pods in one answer for the home page: one
// sample per cluster, and no SSH, so the page stays fast when a machine hangs.
func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := userID(ctx)
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	apps, samples, err := a.applications(ctx, owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	for _, server := range servers {
		if server.ClusterID == "" || a.clusterLost(server.ClusterID) {
			continue
		}
		if _, ok := samples[server.ClusterID]; !ok {
			samples[server.ClusterID], _ = a.clusterSample(ctx, owner, server.ClusterID)
		}
	}
	view := overviewView{Servers: make([]overviewServer, 0, len(servers)), Apps: apps}
	for _, server := range servers {
		row := overviewServer{serverView: a.toServer(server), Usage: nodeUse(server, samples[server.ClusterID])}
		view.Servers = append(view.Servers, row)
	}
	view.Pods = overviewPods(servers, apps, samples)
	writeJSON(w, http.StatusOK, view)
}

func nodeUse(server store.Server, sample usage.Sample) *serverUse {
	if server.ClusterID == "" {
		return nil
	}
	for _, node := range sample.Nodes {
		if node.Name != deploy.NodeName(server.Name) || !node.Measured {
			continue
		}
		return &serverUse{
			ID:       server.ID,
			CPU:      gaugeView{Used: node.CPU.Used, Total: node.CPU.Total},
			Memory:   gaugeView{Used: node.Memory.Used, Total: node.Memory.Total},
			Disk:     gaugeView{Used: node.Disk.Used, Total: node.Disk.Total},
			Measured: true,
		}
	}
	return nil
}

// overviewPods places every pod on its server. A cluster app's pods come from
// the cluster sample; a machine alone runs one container per service copy,
// whose state is the app's own.
func overviewPods(servers []store.Server, apps []appView, samples map[string]usage.Sample) []overviewPod {
	pods := []overviewPod{}
	for _, app := range apps {
		if app.ClusterID == "" {
			pods = append(pods, alonePods(app)...)
			continue
		}
		ns := manifest.Namespace(app.ID)
		if app.ID == selfAppID {
			ns = transfer.PodNamespace
		}
		for _, pod := range samples[app.ClusterID].Pods {
			if pod.Namespace != ns {
				continue
			}
			pods = append(pods, overviewPod{
				Name: pod.Name, AppID: app.ID, ServerID: nodeServer(servers, app.ClusterID, pod.Node),
				Service: pod.Service, Phase: pod.Phase, Reason: pod.Reason, Ready: pod.Ready,
			})
		}
	}
	return pods
}

func nodeServer(servers []store.Server, clusterID, node string) string {
	if node == "" {
		return ""
	}
	for _, server := range servers {
		if server.ClusterID == clusterID && deploy.NodeName(server.Name) == node {
			return server.ID
		}
	}
	return ""
}

func alonePods(app appView) []overviewPod {
	if app.ServerID == "" || app.Paused {
		return nil
	}
	phase, ready := "Pending", false
	switch app.RunStatus {
	case "running":
		phase, ready = "Running", true
	case "failed":
		phase = "Failed"
	}
	pods := []overviewPod{}
	for _, svc := range app.Services {
		copies := svc.Replicas
		if copies < 1 {
			copies = 1
		}
		for i := 0; i < copies; i++ {
			name := app.Name + "-" + svc.Name
			if copies > 1 {
				name += "-" + strconv.Itoa(i+1)
			}
			pods = append(pods, overviewPod{Name: name, AppID: app.ID, ServerID: app.ServerID, Service: svc.Name, Phase: phase, Ready: ready})
		}
	}
	return pods
}
