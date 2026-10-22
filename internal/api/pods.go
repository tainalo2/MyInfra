package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"myinfra/internal/agent"
	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
	"myinfra/internal/usage"
)

var podNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

func (a *API) podLogs(w http.ResponseWriter, r *http.Request) {
	host, namespace, name, err := a.podTarget(r.Context(), ownerID(r.Context()), r.PathValue("id"), r.PathValue("pod"))
	if err != nil {
		a.writePodErr(w, err)
		return
	}
	ctx := r.Context()
	if namespace == "" {
		// A container on a machine alone.
		text, err := deploy.SSH(ctx, host, fmt.Sprintf("docker logs --tail 200 '%s' 2>&1 || true", name))
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if text == "" {
			text = "Aucun journal."
		}
		writeJSON(w, http.StatusOK, map[string]string{"text": text})
		return
	}
	text, err := deploy.SSH(ctx, host, fmt.Sprintf(
		"k3s kubectl logs -n '%s' '%s' --tail=200 --all-containers --prefix 2>&1; printf '\\n--- précédent ---\\n'; k3s kubectl logs -n '%s' '%s' --tail=200 --all-containers --prefix --previous 2>&1 || true; printf '\\n--- événements ---\\n'; k3s kubectl get events -n '%s' --field-selector involvedObject.name='%s' --sort-by=.lastTimestamp -o custom-columns=TIME:.lastTimestamp,REASON:.reason,MESSAGE:.message --no-headers 2>&1 || true",
		namespace, name, namespace, name, namespace, name,
	))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if text == "" {
		text = "Aucun journal."
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (a *API) resetPod(w http.ResponseWriter, r *http.Request) {
	host, namespace, name, err := a.podTarget(r.Context(), ownerID(r.Context()), r.PathValue("id"), r.PathValue("pod"))
	if err != nil {
		a.writePodErr(w, err)
		return
	}
	command := fmt.Sprintf("k3s kubectl delete pod -n '%s' '%s' --wait=false", namespace, name)
	if namespace == "" {
		command = fmt.Sprintf("docker restart '%s'", name)
	}
	if _, err := deploy.SSH(r.Context(), host, command); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) podTarget(ctx context.Context, owner, appID, name string) (deploy.Host, string, string, error) {
	if !podNamePattern.MatchString(name) {
		return deploy.Host{}, "", "", errString("Ce pod est introuvable.")
	}
	if appID == selfAppID {
		return a.selfPodTarget(ctx, owner, name)
	}
	if app, err := a.Store.Application(ctx, owner, appID); err == nil && app.ClusterID == "" && app.ServerID != "" {
		if !strings.HasPrefix(name, agent.ContainerPrefix(app.ID)) {
			return deploy.Host{}, "", "", store.ErrNotFound
		}
		host, err := a.machineHost(ctx, owner, app.ServerID)
		return host, "", name, err
	}
	app, err := a.Store.Application(ctx, owner, appID)
	if err != nil {
		return deploy.Host{}, "", "", err
	}
	if app.ClusterID == "" {
		return deploy.Host{}, "", "", errString("Cette application n'a pas de pod.")
	}
	namespace := manifest.Namespace(app.ID)
	sample, _ := a.clusterSample(ctx, owner, app.ClusterID)
	found := false
	for _, pod := range sample.Pods {
		if pod.Namespace == namespace && pod.Name == name {
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
	return host, namespace, name, nil
}

func (a *API) machineHost(ctx context.Context, owner, serverID string) (deploy.Host, error) {
	server, err := a.Store.Server(ctx, owner, serverID)
	if err != nil {
		return deploy.Host{}, err
	}
	if server.Address == "" {
		return deploy.Host{}, errString("Cette machine n'a pas encore d'adresse.")
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, server.ID)
	if err != nil {
		return deploy.Host{}, err
	}
	key := a.privateKey(ctx, owner, keyIDs)
	if key == "" {
		return deploy.Host{}, errString("Aucune clé privée pour ouvrir la session SSH.")
	}
	return deploy.Host{Address: server.Address, PrivateKey: key}, nil
}

// machineContainers lists the containers of an application on a machine alone.
func (a *API) machineContainers(ctx context.Context, owner, serverID, appID string) ([]podView, bool) {
	host, err := a.machineHost(ctx, owner, serverID)
	if err != nil {
		return []podView{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := deploy.SSH(ctx, host, fmt.Sprintf("docker ps -a --filter 'name=^%s' --format '{{.Names}}|{{.State}}|{{.Status}}'", agent.ContainerPrefix(appID)))
	if err != nil {
		return []podView{}, false
	}
	pods := []podView{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 || strings.HasSuffix(parts[0], "-next") {
			continue
		}
		phase := "Pending"
		switch parts[1] {
		case "running":
			phase = "Running"
		case "exited", "dead":
			phase = "Failed"
		}
		pods = append(pods, podView{Name: parts[0], Phase: phase, Reason: parts[2], Ready: parts[1] == "running"})
	}
	return pods, true
}

func (a *API) clusterHost(ctx context.Context, owner, clusterID string) (deploy.Host, error) {
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return deploy.Host{}, err
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
		return deploy.Host{}, errString("Aucun serveur lié.")
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, leader.ID)
	if err != nil {
		return deploy.Host{}, err
	}
	key := a.privateKey(ctx, owner, keyIDs)
	if key == "" {
		return deploy.Host{}, errString("Aucune clé privée pour ouvrir la session SSH.")
	}
	return deploy.Host{Address: leader.Address, PrivateKey: key}, nil
}

func (a *API) writePodErr(w http.ResponseWriter, err error) {
	var msg errString
	if errors.As(err, &msg) {
		writeError(w, http.StatusConflict, msg.Error())
		return
	}
	a.mapErr(w, err)
}

func (a *API) noteAppHealth(ctx context.Context, app store.Application, sample usage.Sample) string {
	if app.Paused {
		return "paused"
	}
	namespace := manifest.Namespace(app.ID)
	failed := false
	seen := false
	for _, pod := range sample.Pods {
		if pod.Namespace != namespace {
			continue
		}
		seen = true
		if usage.PodFailed(pod) {
			failed = true
		}
	}
	status := ""
	if failed {
		status = "failed"
	}
	// Ready pods alone never make an application online: the delivery does,
	// once its address answers in HTTPS with a valid certificate.
	if status != "" && status != app.RunStatus && seen {
		_ = a.Store.SetRunStatus(ctx, app.ID, status)
	}
	if status == "" {
		return app.RunStatus
	}
	return status
}
