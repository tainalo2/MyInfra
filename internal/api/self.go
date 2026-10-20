package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/store"
	"myinfra/internal/transfer"
	"myinfra/internal/usage"
)

const (
	selfAppID         = "myinfra"
	selfDestroyPhrase = "détruire myinfra"
)

var selfPhrases = map[string]string{
	"fr": selfDestroyPhrase,
	"en": "destroy myinfra",
	"pt": "destruir myinfra",
	"it": "distruggere myinfra",
	"de": "myinfra zerstören",
}

func selfPhraseOK(name string) bool {
	name = strings.TrimSpace(name)
	for _, phrase := range selfPhrases {
		if name == phrase {
			return true
		}
	}
	return false
}

func phraseFor(r *http.Request) string {
	lang := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language")))
	if len(lang) >= 2 {
		if phrase, ok := selfPhrases[lang[:2]]; ok {
			return phrase
		}
	}
	return selfDestroyPhrase
}

// selfRecord is MyInfra shown as an application of the cluster it runs in:
// on the copy in the pods, and on a copy a transfer kept elsewhere.
func (a *API) selfRecord(ctx context.Context) (store.Application, bool) {
	clusterID, publicURL := "", ""
	if inst, err := a.Store.Installation(ctx); err == nil && inst.Method == "pod" && inst.ClusterID != "" {
		clusterID, publicURL = inst.ClusterID, inst.PublicURL
	} else if role, err := a.Store.CopyRole(ctx); err == nil && role.MainMethod == "pod" && role.MainClusterID != "" {
		clusterID, publicURL = role.MainClusterID, role.MainURL
	}
	if clusterID == "" {
		return store.Application{}, false
	}
	host := ""
	if parsed, err := url.Parse(publicURL); err == nil {
		host = parsed.Hostname()
	}
	return store.Application{
		ID: selfAppID, Name: "MyInfra", SourceKind: "image", Source: strings.TrimSpace(a.Image),
		DestinationKind: "cluster", ClusterID: clusterID, Hostname: host, Replicas: 2,
	}, true
}

func (a *API) selfView(ctx context.Context, owner string) (appView, bool) {
	app, ok := a.selfRecord(ctx)
	if !ok {
		return appView{}, false
	}
	if rows, err := a.Store.ListClusters(ctx, owner); err == nil {
		for _, row := range rows {
			if row.ID == app.ClusterID {
				app.ClusterName = row.Name
			}
		}
	}
	sample, _ := a.clusterSample(ctx, owner, app.ClusterID)
	app.RunStatus = selfHealth(sample)
	view := appView{
		ID: app.ID, Name: app.Name, SourceKind: app.SourceKind, Source: app.Source,
		DestinationKind: app.DestinationKind, ClusterID: app.ClusterID, ClusterName: app.ClusterName,
		Hostname: app.Hostname, Replicas: app.Replicas, RunStatus: app.RunStatus, Guarded: true,
		Domains: []domainView{},
		Services: []serviceView{
			{Name: "myinfra", Image: app.Source, Published: true, Replicas: 2},
			{Name: transfer.DatabaseName, Image: "PostgreSQL · CloudNativePG", Replicas: 2},
		},
	}
	if view.Source == "" {
		view.Source = "MyInfra"
		view.Services[0].Image = "MyInfra"
	}
	if app.Hostname != "" {
		view.Domains = []domainView{{Hostname: app.Hostname, Kind: "custom", Status: "ready"}}
	}
	return view, true
}

func selfHealth(sample usage.Sample) string {
	ready := false
	failed := false
	for _, pod := range sample.Pods {
		if pod.Namespace != transfer.PodNamespace {
			continue
		}
		if usage.PodFailed(pod) {
			failed = true
		}
		if pod.Ready {
			ready = true
		}
	}
	if failed {
		return "failed"
	}
	if ready {
		return "running"
	}
	return ""
}

func (a *API) selfRuntime(w http.ResponseWriter, r *http.Request) {
	app, ok := a.selfRecord(r.Context())
	if !ok {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	view := runtimeView{Pods: []podView{}, Status: app.RunStatus}
	sample, measured := a.clusterSample(r.Context(), userID(r.Context()), app.ClusterID)
	view.Measured = measured
	servers, _ := a.Store.ListServers(r.Context(), userID(r.Context()))
	nodes := map[string]bool{}
	for _, pod := range sample.Pods {
		if pod.Namespace != transfer.PodNamespace {
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
	view.Status = selfHealth(sample)
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

func (a *API) selfPodTarget(ctx context.Context, owner, name string) (deploy.Host, string, string, error) {
	app, ok := a.selfRecord(ctx)
	if !ok {
		return deploy.Host{}, "", "", store.ErrNotFound
	}
	sample, _ := a.clusterSample(ctx, owner, app.ClusterID)
	found := false
	for _, pod := range sample.Pods {
		if pod.Namespace == transfer.PodNamespace && pod.Name == name {
			found = true
			break
		}
	}
	if !found {
		return deploy.Host{}, "", "", store.ErrNotFound
	}
	host, err := a.clusterHost(ctx, owner, app.ClusterID)
	if err != nil {
		return deploy.Host{}, "", "", err
	}
	return host, transfer.PodNamespace, name, nil
}

func (a *API) deleteSelf(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if !selfPhraseOK(body.Name) {
		phrase := phraseFor(r)
		writeCoded(w, http.StatusBadRequest, "confirm_self", "Tapez « "+phrase+" » pour confirmer.", map[string]string{"phrase": phrase})
		return
	}
	app, ok := a.selfRecord(r.Context())
	if !ok {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	host, err := a.clusterHost(r.Context(), userID(r.Context()), app.ClusterID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err := deploy.SSH(r.Context(), host, "k3s kubectl delete ns "+transfer.PodNamespace+" --wait=false"); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Removed from a copy that followed it, the copy drives again: there is
	// no other one left. Its name and its peer go with it.
	if role, err := a.Store.CopyRole(r.Context()); err == nil && role.MainMethod == "pod" && role.MainClusterID == app.ClusterID {
		owner := userID(r.Context())
		if zone := zoneOfURL(role.MainURL); zone != "" {
			if sec, creds, err := a.dnsAccountOf(r.Context(), owner, zone); err == nil {
				_ = a.dnsClient().DropAddress(r.Context(), sec.Provider, creds, zone, hostOfURL(role.MainURL))
			}
		}
		_ = a.Store.DeletePeer(r.Context(), role.MainURL)
		if err := a.Store.SetCopyRole(r.Context(), store.CopyRole{}); err == nil {
			a.note(r.Context(), owner, newID(), "myinfra.removed", "warn", "MyInfra a été retiré du cluster : cette copie pilote de nouveau les machines.", "", app.ClusterID)
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping", "restart": "true"})
			go func() {
				time.Sleep(3 * time.Second)
				if a.Restart != nil {
					a.Restart()
				} else if a.Shutdown != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = a.Shutdown(ctx)
				}
			}()
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}
