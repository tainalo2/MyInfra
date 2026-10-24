package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/store"
)

type destructionJob struct {
	Steps []destructionStep `json:"steps"`
}

type destructionStep struct {
	Label      string `json:"label"`
	Action     string `json:"action"`
	ServerID   string `json:"serverId,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	MachineID  string `json:"machineId,omitempty"`
	SecretID   string `json:"secretId,omitempty"`
	Zone       string `json:"zone,omitempty"`
	Hostname   string `json:"hostname,omitempty"`
	ClusterID  string `json:"clusterId,omitempty"`
	AppID      string `json:"appId,omitempty"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
}

func (a *API) Destructions(ctx context.Context) {
	a.resumeDestructions()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.resumeDestructions()
		}
	}
}

func (a *API) resumeDestructions() {
	rows, err := a.Store.RunningDestructions(context.Background())
	if err != nil {
		return
	}
	for _, row := range rows {
		go a.kickDestruction(row.ID, row.OwnerID)
	}
}

func (a *API) listDestructions(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListRunningDestructions(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]destructionItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, destructionItemOf(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getDestruction(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.Destruction(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, destructionViewOf(row))
}

func (a *API) resumeDestruction(w http.ResponseWriter, r *http.Request) {
	if a.alone(r.Context()) {
		writeError(w, http.StatusConflict, replicaAlone)
		return
	}
	owner := ownerID(r.Context())
	row, err := a.Store.Destruction(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if row.Status != "failed" {
		writeError(w, http.StatusConflict, "Cette destruction n'est pas en échec.")
		return
	}
	var job destructionJob
	if err := json.Unmarshal(row.Body, &job); err != nil {
		a.mapErr(w, err)
		return
	}
	for i := range job.Steps {
		if job.Steps[i].Status == "failed" || job.Steps[i].Status == "running" {
			job.Steps[i].Status = "pending"
			job.Steps[i].Detail = ""
		}
	}
	raw, err := json.Marshal(job)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.UpdateDestruction(r.Context(), owner, row.ID, "running", raw); err != nil {
		a.mapErr(w, err)
		return
	}
	go a.kickDestruction(row.ID, owner)
	row.Status = "running"
	row.Body = raw
	writeJSON(w, http.StatusOK, destructionViewOf(row))
}

func (a *API) deleteApplication(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == selfAppID {
		a.deleteSelf(w, r)
		return
	}
	owner := ownerID(r.Context())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if !confirmedName(w, r, app.Name) {
		return
	}
	names, err := a.Store.ApplicationNames(r.Context(), owner, app.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	steps := appSteps(app, names)
	if app.DestinationKind == "cluster" {
		origin, _, _, _, err := a.Store.ClusterKube(r.Context(), owner, app.ClusterID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if origin == "created" {
			steps = append(steps, destructionStep{
				Label: "Retirer les charges du cluster", Action: "workload", ClusterID: app.ClusterID, Status: "pending",
			})
		}
	}
	a.respondDestruction(w, r, owner, "application", app.ID, app.Name, steps)
}

func (a *API) respondDestruction(w http.ResponseWriter, r *http.Request, owner, kind, target, name string, steps []destructionStep) {
	if a.alone(r.Context()) {
		writeError(w, http.StatusConflict, replicaAlone)
		return
	}
	row, err := a.startDestruction(r.Context(), owner, kind, target, name, steps)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, destructionViewOf(row))
}

func (a *API) startDestruction(ctx context.Context, owner, kind, target, name string, steps []destructionStep) (store.Destruction, error) {
	if existing, err := a.Store.RunningDestruction(ctx, owner, kind, target); err == nil {
		return existing, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Destruction{}, err
	}
	for i := range steps {
		if steps[i].Status == "" {
			steps[i].Status = "pending"
		}
	}
	raw, err := json.Marshal(destructionJob{Steps: steps})
	if err != nil {
		return store.Destruction{}, err
	}
	id := newID()
	if err := a.Store.CreateDestruction(ctx, owner, id, kind, target, name, raw); err != nil {
		return store.Destruction{}, err
	}
	_ = a.Store.HoldNames(ctx, owner, kind, target)
	if kind == "server" {
		a.stopUpdateJobs(ctx, owner, target, "")
		a.noteServer(ctx, owner, target, "server.delete", "warn", "Suppression demandée.")
	} else if kind == "cluster" {
		a.stopUpdateJobs(ctx, owner, "", target)
		a.note(ctx, owner, newID(), "cluster.delete", "warn", "Suppression demandée.", "", target)
	} else if kind == "application" {
		a.note(ctx, owner, newID(), "app.delete", "warn", "Suppression de l'application "+name+" demandée.", "", "")
	}
	go a.kickDestruction(id, owner)
	return a.Store.Destruction(ctx, owner, id)
}

func (a *API) kickDestruction(id, owner string) {
	if a.Replica {
		return
	}
	a.destructionMu.Lock()
	if a.destructionBusy == nil {
		a.destructionBusy = map[string]bool{}
	}
	if a.destructionBusy[id] {
		a.destructionMu.Unlock()
		return
	}
	a.destructionBusy[id] = true
	a.destructionMu.Unlock()
	defer func() {
		a.destructionMu.Lock()
		delete(a.destructionBusy, id)
		a.destructionMu.Unlock()
	}()
	a.runDestruction(owner, id)
}

func (a *API) runDestruction(owner, id string) {
	for {
		row, err := a.Store.Destruction(context.Background(), owner, id)
		if err != nil || row.Status != "running" {
			return
		}
		var job destructionJob
		if err := json.Unmarshal(row.Body, &job); err != nil {
			return
		}
		index := -1
		for i := range job.Steps {
			switch job.Steps[i].Status {
			case "failed":
				_ = a.persistDestruction(owner, id, "failed", job)
				return
			case "pending", "running":
				index = i
			}
			if index >= 0 {
				break
			}
		}
		if index < 0 {
			_ = a.persistDestruction(owner, id, "done", job)
			return
		}
		job.Steps[index].Status = "running"
		job.Steps[index].Detail = ""
		if err := a.persistDestruction(owner, id, "running", job); err != nil {
			return
		}
		detail, err := a.performDestruction(owner, &job.Steps[index], wholeCluster(job))
		if err != nil {
			job.Steps[index].Status = "failed"
			job.Steps[index].Detail = err.Error()
			_ = a.persistDestruction(owner, id, "failed", job)
			return
		}
		job.Steps[index].Status = "done"
		job.Steps[index].Detail = detail
		if err := a.persistDestruction(owner, id, "running", job); err != nil {
			return
		}
	}
}

func (a *API) persistDestruction(owner, id, status string, job destructionJob) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return a.Store.UpdateDestruction(context.Background(), owner, id, status, raw)
}

// wholeCluster tells whether a destruction removes a cluster with all its
// servers: draining a node, or closing the firewall behind it, is then of no
// use, and would wait on servers that may be gone already.
func wholeCluster(job destructionJob) bool {
	for _, step := range job.Steps {
		if step.Action == "cluster" {
			return true
		}
	}
	return false
}

func (a *API) performDestruction(owner string, step *destructionStep, whole bool) (string, error) {
	switch step.Action {
	case "server":
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		// A deployment still running on this machine would keep its SSH
		// session, and the deployment lock, on a machine that no longer exists.
		a.abortDeploy(step.ServerID)
		// A machine its provider no longer has is only removed from MyInfra:
		// nothing would answer the steps that act on it.
		gone, provider := a.machineGone(ctx, owner, step.ProviderID, step.MachineID)
		clusterID := ""
		if srv, err := a.Store.Server(ctx, owner, step.ServerID); err == nil {
			clusterID = srv.ClusterID
			if clusterID != "" && !gone && !whole {
				a.removeNode(ctx, owner, srv)
			}
		}
		if gone {
			provisionClient(a).ForgetStack(provider, step.MachineID)
		} else if err := a.destroyMachine(ctx, owner, step.ProviderID, step.MachineID); err != nil {
			slog.Info("destruction machine", "server", step.ServerID, "err", err)
			return "", errors.New("La machine n'a pas pu être détruite : " + err.Error())
		}
		if err := a.Store.DeleteServer(ctx, owner, step.ServerID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", errors.New("Le serveur n'a pas pu être retiré.")
		}
		if clusterID != "" && !whole {
			if err := a.reconcileFirewall(ctx, owner, clusterID); err != nil {
				slog.Info("pare-feu", "cluster", clusterID, "err", err)
			}
		}
	case "dns":
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := a.dropName(ctx, owner, store.Name{Hostname: step.Hostname, Zone: step.Zone, SecretID: step.SecretID})
		if err != nil {
			slog.Info("destruction dns", "host", step.Hostname, "err", err)
			return "", errors.New("Le nom " + step.Hostname + " n'a pas pu être retiré.")
		}
	case "cluster":
		if err := a.Store.DeleteCluster(context.Background(), owner, step.ClusterID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", errors.New("Le cluster n'a pas pu être retiré.")
		}
	case "application":
		if err := a.Store.DeleteApplication(context.Background(), owner, step.AppID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", errors.New("L'application n'a pas pu être retirée.")
		}
	case "workload":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		leaders, err := a.Store.ClusterLeaders(ctx)
		if err != nil {
			return "", errors.New("Les charges n'ont pas pu être retirées.")
		}
		for _, leader := range leaders {
			if leader.ClusterID == step.ClusterID && leader.OwnerID == owner {
				a.deliverCluster(ctx, leader)
				return "", nil
			}
		}
		return "Aucun serveur lié.", nil
	default:
		return "", errors.New("Étape inconnue.")
	}
	return "", nil
}

func confirmedName(w http.ResponseWriter, r *http.Request, expected string) bool {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return false
	}
	got := strings.TrimSpace(body.Name)
	if got != expected && !(expected == selfDestroyPhrase && selfPhraseOK(got)) {
		writeError(w, http.StatusBadRequest, "La confirmation ne correspond pas.")
		return false
	}
	return true
}

func clusterSteps(members []store.Server, names []store.Name, clusterID string) []destructionStep {
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].ControlPlane != members[j].ControlPlane {
			return !members[i].ControlPlane
		}
		return members[i].CreatedAt.After(members[j].CreatedAt)
	})
	steps := make([]destructionStep, 0, len(members)+len(names)+1)
	for _, srv := range members {
		label := "Retirer " + srv.Name
		if srv.ProviderServerID != "" {
			label = "Détruire " + srv.Name
		}
		steps = append(steps, destructionStep{
			Label: label, Action: "server", ServerID: srv.ID,
			ProviderID: srv.ProviderID, MachineID: srv.ProviderServerID, Status: "pending",
		})
	}
	for _, name := range names {
		steps = append(steps, dnsStep(name))
	}
	steps = append(steps, destructionStep{Label: "Retirer le cluster", Action: "cluster", ClusterID: clusterID, Status: "pending"})
	return steps
}

func serverSteps(srv store.Server, names []store.Name) []destructionStep {
	steps := make([]destructionStep, 0, len(names)+1)
	for _, name := range names {
		steps = append(steps, dnsStep(name))
	}
	label := "Retirer " + srv.Name
	if srv.ProviderServerID != "" {
		label = "Détruire " + srv.Name
	}
	steps = append(steps, destructionStep{
		Label: label, Action: "server", ServerID: srv.ID,
		ProviderID: srv.ProviderID, MachineID: srv.ProviderServerID, Status: "pending",
	})
	return steps
}

func appSteps(app store.Application, names []store.Name) []destructionStep {
	steps := make([]destructionStep, 0, len(names)+1)
	for _, name := range names {
		steps = append(steps, dnsStep(name))
	}
	steps = append(steps, destructionStep{Label: "Retirer l'application", Action: "application", AppID: app.ID, Status: "pending"})
	return steps
}

func dnsStep(name store.Name) destructionStep {
	return destructionStep{
		Label: "Retirer " + name.Hostname, Action: "dns",
		Hostname: name.Hostname, Zone: name.Zone, SecretID: name.SecretID, Status: "pending",
	}
}

func destructionViewOf(row store.Destruction) destructionView {
	var job destructionJob
	_ = json.Unmarshal(row.Body, &job)
	view := destructionView{
		ID: row.ID, Kind: row.Kind, TargetID: row.TargetID, Name: row.Name, Status: row.Status, CreatedAt: row.CreatedAt,
		Steps: make([]destructionStepView, 0, len(job.Steps)),
	}
	for _, step := range job.Steps {
		view.Steps = append(view.Steps, destructionStepView{Label: step.Label, Status: step.Status, Detail: step.Detail})
	}
	return view
}

func destructionItemOf(row store.Destruction) destructionItem {
	return destructionItem{ID: row.ID, Kind: row.Kind, TargetID: row.TargetID, Name: row.Name, Status: row.Status, CreatedAt: row.CreatedAt}
}

type destructionItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	TargetID  string    `json:"targetId"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type destructionView struct {
	ID        string                `json:"id"`
	Kind      string                `json:"kind"`
	TargetID  string                `json:"targetId,omitempty"`
	Name      string                `json:"name"`
	Status    string                `json:"status"`
	Steps     []destructionStepView `json:"steps"`
	CreatedAt time.Time             `json:"createdAt"`
}

type destructionStepView struct {
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// removeNode moves the work of a cluster node to the others, then takes the
// node out of Kubernetes, before its machine is destroyed. Without it, the
// pods stay bound to a machine that no longer exists.
func (a *API) removeNode(ctx context.Context, owner string, srv store.Server) {
	host, ok := a.otherPlane(ctx, owner, srv)
	if !ok {
		return
	}
	node := deploy.NodeName(srv.Name)
	command := fmt.Sprintf("k3s kubectl drain %q --ignore-daemonsets --delete-emptydir-data --force --timeout=120s >/dev/null 2>&1; k3s kubectl delete node %q --wait=false >/dev/null 2>&1 || true", node, node)
	if _, err := deploy.SSH(ctx, host, command); err != nil {
		slog.Info("retrait du nœud", "node", node, "err", err)
	}
}

// otherPlane is a linked control plane of the cluster other than srv.
func (a *API) otherPlane(ctx context.Context, owner string, srv store.Server) (deploy.Host, bool) {
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return deploy.Host{}, false
	}
	for _, row := range rows {
		if row.ID == srv.ID || row.ClusterID != srv.ClusterID || !row.ControlPlane || row.Status != "linked" || row.Address == "" {
			continue
		}
		if state, _ := a.reachOf(row.ID); state != "" {
			continue
		}
		keyIDs, err := a.Store.ServerKeyIDs(ctx, row.ID)
		if err != nil {
			continue
		}
		if key := a.privateKey(ctx, owner, keyIDs); key != "" {
			return deploy.Host{Address: row.Address, PrivateKey: key}, true
		}
	}
	return deploy.Host{}, false
}
