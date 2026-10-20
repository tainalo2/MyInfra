package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/join"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
)

// k3sVersionCommand prints the k3s of a node, such as v1.36.5+k3s1.
const k3sVersionCommand = "k3s --version 2>/dev/null | awk 'NR==1 {print $3}'"

// serverUpdateDetail is what a server can update, as its check found.
type serverUpdateDetail struct {
	Packages int    `json:"packages"`
	Security int    `json:"security"`
	Reboot   bool   `json:"reboot"`
	System   string `json:"system,omitempty"`
	Kernel   string `json:"kernel,omitempty"`
	K3s      string `json:"k3s,omitempty"`
	K3sNext  string `json:"k3sNext,omitempty"`
}

// serverCheckScript reads, without changing anything but the package lists,
// what a server can update.
const serverCheckScript = `export DEBIAN_FRONTEND=noninteractive
apt-get update -qq -o DPkg::Lock::Timeout=60 >/dev/null 2>&1 || true
list=$(apt list --upgradable 2>/dev/null | grep 'upgradable from' || true)
total=$(printf '%s\n' "$list" | grep -c 'upgradable from' || true)
security=$(printf '%s\n' "$list" | grep -c -- '-security' || true)
reboot=no
[ -f /var/run/reboot-required ] && reboot=yes
k3s=$(` + k3sVersionCommand + `)
. /etc/os-release 2>/dev/null || PRETTY_NAME=''
echo "myinfra-updates $total $security $reboot ${k3s:--}"
echo "myinfra-system $PRETTY_NAME"
echo "myinfra-kernel $(uname -r)"
`

func parseServerCheck(out string) (serverUpdateDetail, bool) {
	var detail serverUpdateDetail
	found := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "myinfra-updates "):
			fields := strings.Fields(line)
			if len(fields) != 5 {
				return detail, false
			}
			detail.Packages, _ = strconv.Atoi(fields[1])
			detail.Security, _ = strconv.Atoi(fields[2])
			detail.Reboot = fields[3] == "yes"
			if deploy.ValidK3sVersion(fields[4]) {
				detail.K3s = fields[4]
			}
			found = true
		case strings.HasPrefix(line, "myinfra-system "):
			detail.System = strings.TrimSpace(strings.TrimPrefix(line, "myinfra-system "))
		case strings.HasPrefix(line, "myinfra-kernel "):
			detail.Kernel = strings.TrimSpace(strings.TrimPrefix(line, "myinfra-kernel "))
		}
	}
	return detail, found
}

// k3sChannels gives the latest version of each minor k3s line, read from the
// k3s update service.
var k3sChannels = func(ctx context.Context) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://update.k3s.io/v1-release/channels", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("k3s répond %d", resp.StatusCode)
	}
	var listed struct {
		Data []struct {
			Name   string `json:"name"`
			Latest string `json:"latest"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&listed); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, item := range listed.Data {
		if deploy.ValidK3sVersion(item.Latest) {
			out[item.Name] = item.Latest
		}
	}
	return out, nil
}

// k3sParts reads v1.35.9+k3s1 as 35, 9 and 1.
func k3sParts(version string) (minor, patch, build int, ok bool) {
	if !deploy.ValidK3sVersion(version) {
		return 0, 0, 0, false
	}
	core, k3s, _ := strings.Cut(strings.TrimPrefix(version, "v1."), "+k3s")
	minorText, patchText, _ := strings.Cut(core, ".")
	minor, _ = strconv.Atoi(minorText)
	patch, _ = strconv.Atoi(patchText)
	build, _ = strconv.Atoi(k3s)
	return minor, patch, build, true
}

// nextK3s is the version a node goes to next: one minor line at a time, as
// Kubernetes requires, up to the version MyInfra pins.
func nextK3s(current string, channels map[string]string) string {
	minor, patch, build, ok := k3sParts(current)
	target, tpatch, tbuild, tok := k3sParts(deploy.K3sVersion)
	if !ok || !tok {
		return ""
	}
	switch {
	case minor < target:
		if next := channels[fmt.Sprintf("v1.%d", minor+1)]; next != "" {
			return next
		}
		if minor+1 == target {
			return deploy.K3sVersion
		}
		return ""
	case minor == target && (patch < tpatch || (patch == tpatch && build < tbuild)):
		return deploy.K3sVersion
	}
	return ""
}

func (a *API) checkServers(ctx context.Context) error {
	owner, _, err := a.Store.User(ctx)
	if err != nil {
		return nil
	}
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return err
	}
	channels, _ := k3sChannels(ctx)
	clusters := map[string]bool{}
	if rows, err := a.Store.ListClusters(ctx, owner); err == nil {
		for _, row := range rows {
			clusters[row.ID] = row.Origin != "imported"
		}
	}
	var (
		mu      sync.Mutex
		found   []store.Update
		checked []string
		group   sync.WaitGroup
		slots   = make(chan struct{}, 4)
	)
	for _, server := range servers {
		if server.Status != "linked" || server.Address == "" {
			continue
		}
		group.Add(1)
		go func(server store.Server) {
			defer group.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			host, err := a.serverHost(ctx, owner, server.ID)
			if err != nil {
				return
			}
			checkCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			out, err := deploy.SSH(checkCtx, host, serverCheckScript)
			if err != nil {
				return
			}
			detail, ok := parseServerCheck(out)
			if !ok {
				return
			}
			if server.ClusterID != "" && clusters[server.ClusterID] {
				detail.K3sNext = nextK3s(detail.K3s, channels)
			}
			item, has := serverUpdate(owner, server.ID, detail)
			mu.Lock()
			defer mu.Unlock()
			checked = append(checked, server.ID)
			if has {
				found = append(found, item)
			}
		}(server)
	}
	group.Wait()
	if checked == nil {
		checked = []string{}
	}
	return a.Store.SaveUpdates(ctx, "server", found, checked)
}

// serverUpdate turns a check into an update to offer, if there is one.
func serverUpdate(owner, id string, detail serverUpdateDetail) (store.Update, bool) {
	level := ""
	switch {
	case detail.Security > 0:
		level = "security"
	case detail.Reboot:
		level = "reboot"
	case detail.Packages > 0:
		level = "packages"
	case detail.K3sNext != "":
		level = "minor"
	default:
		return store.Update{}, false
	}
	raw, _ := json.Marshal(detail)
	return store.Update{TargetID: id, OwnerID: owner, Current: detail.K3s, Available: detail.K3sNext, Level: level, Detail: raw}, true
}

// updateStep is one step of an update job. The steps of a server follow each
// other: check, drain, packages, k3s, reboot, ready, uncordon, settle.
type updateStep struct {
	Action   string `json:"action"`
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	K3s      string `json:"k3s,omitempty"`
	BootID   string `json:"bootId,omitempty"`
	// Waiting are the services not ready before the node was emptied, on its
	// check step: the update does not wait for them afterwards.
	Waiting []string `json:"waiting,omitempty"`
	Status  string   `json:"status"`
	Detail  string   `json:"detail,omitempty"`
}

// planUpdate orders the servers of a job and lists their steps. With k3s,
// control planes go first, as Kubernetes requires; otherwise the other nodes
// go first and the oldest control plane, which leads, last.
func planUpdate(servers []store.Server, details map[string]serverUpdateDetail, withK3s bool) []updateStep {
	sorted := append([]store.Server(nil), servers...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ControlPlane != sorted[j].ControlPlane {
			return sorted[i].ControlPlane == withK3s
		}
		// The leader is the oldest control plane: it goes last.
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	var steps []updateStep
	for _, server := range sorted {
		detail := details[server.ID]
		add := func(action, k3s string) {
			steps = append(steps, updateStep{Action: action, ServerID: server.ID, Name: server.Name, K3s: k3s, Status: "pending"})
		}
		inCluster := server.ClusterID != ""
		if inCluster {
			add("check", "")
			add("drain", "")
		}
		add("packages", "")
		if withK3s && inCluster && detail.K3sNext != "" {
			add("k3s", detail.K3sNext)
		}
		add("reboot", "")
		add("ready", "")
		if inCluster {
			add("uncordon", "")
			add("settle", "")
		}
	}
	return steps
}

var (
	updateJobMu     sync.Mutex
	updateJobBusy   = map[string]bool{}
	updateJobCancel = map[string]bool{}
	updateJobStop   = map[string]context.CancelFunc{}
)

type updateJobView struct {
	ID         string       `json:"id"`
	ClusterID  string       `json:"clusterId,omitempty"`
	ServerID   string       `json:"serverId,omitempty"`
	Name       string       `json:"name"`
	Status     string       `json:"status"`
	Steps      []updateStep `json:"steps"`
	CreatedAt  time.Time    `json:"createdAt"`
	FinishedAt *time.Time   `json:"finishedAt,omitempty"`
}

func (a *API) jobView(ctx context.Context, job store.UpdateJob) updateJobView {
	view := updateJobView{ID: job.ID, ClusterID: job.ClusterID, ServerID: job.ServerID, Status: job.Status, CreatedAt: job.CreatedAt, FinishedAt: job.FinishedAt}
	_ = json.Unmarshal(job.Steps, &view.Steps)
	if view.Steps == nil {
		view.Steps = []updateStep{}
	}
	if job.ClusterID != "" {
		if cluster, err := a.Store.Cluster(ctx, job.OwnerID, job.ClusterID); err == nil {
			view.Name = cluster.Name
		}
	} else if server, err := a.Store.Server(ctx, job.OwnerID, job.ServerID); err == nil {
		view.Name = server.Name
	}
	return view
}

func (a *API) listUpdateJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.Store.RecentUpdateJobs(r.Context(), userID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]updateJobView, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, a.jobView(r.Context(), job))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getUpdateJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.Store.UpdateJob(r.Context(), userID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.jobView(r.Context(), job))
}

// targetServers are the servers an update request covers, with what each can
// update.
func (a *API) targetServers(ctx context.Context, owner, clusterID, serverID string) ([]store.Server, map[string]serverUpdateDetail, error) {
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	details := map[string]serverUpdateDetail{}
	var out []store.Server
	for _, server := range servers {
		if (serverID != "" && server.ID != serverID) || (clusterID != "" && server.ClusterID != clusterID) {
			continue
		}
		if server.Status != "linked" || server.Address == "" {
			if serverID != "" {
				return nil, nil, errString("Ce serveur n'est pas prêt : il ne peut pas être mis à jour maintenant.")
			}
			continue
		}
		var detail serverUpdateDetail
		if found, err := a.Store.UpdateOf(ctx, "server", server.ID); err == nil {
			_ = json.Unmarshal(found.Detail, &detail)
		}
		details[server.ID] = detail
		out = append(out, server)
	}
	if len(out) == 0 {
		return nil, nil, store.ErrNotFound
	}
	return out, details, nil
}

func (a *API) serverUpdateImpact(w http.ResponseWriter, r *http.Request) {
	a.writeUpdateImpact(w, r, "", r.PathValue("id"))
}

func (a *API) clusterUpdateImpact(w http.ResponseWriter, r *http.Request) {
	a.writeUpdateImpact(w, r, r.PathValue("id"), "")
}

func (a *API) writeUpdateImpact(w http.ResponseWriter, r *http.Request, clusterID, serverID string) {
	owner := userID(r.Context())
	servers, details, err := a.targetServers(r.Context(), owner, clusterID, serverID)
	if err != nil {
		a.writeJobErr(w, err)
		return
	}
	apps, err := a.detailedApps(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	worst := map[string]impactApp{}
	var order []string
	keep := func(items []impactApp) {
		for _, item := range items {
			current, seen := worst[item.ID]
			if !seen {
				order = append(order, item.ID)
				worst[item.ID] = item
				continue
			}
			if effectRank[item.Effect] > effectRank[current.Effect] {
				worst[item.ID] = item
			}
		}
	}
	notes := []string{}
	reboot, k3s := false, false
	for _, server := range servers {
		detail := details[server.ID]
		reboot = reboot || detail.Reboot
		k3s = k3s || detail.K3sNext != ""
		if server.ClusterID == "" {
			keep(machineImpact(server, apps))
			continue
		}
		sample, measured := a.clusterSample(r.Context(), owner, server.ClusterID)
		keep(nodeImpact(server, apps, sample, measured))
	}
	if planes := controlPlanes(servers); len(servers) > 0 && servers[0].ClusterID != "" {
		all, _ := a.Store.ListServers(r.Context(), owner)
		if len(controlPlanes(filterCluster(all, servers[0].ClusterID))) == 1 && len(planes) == 1 {
			notes = append(notes, "single-plane")
		}
	}
	if reboot {
		notes = append(notes, "reboot")
	}
	if k3s {
		notes = append(notes, "k3s")
	}
	items := make([]impactApp, 0, len(order))
	for _, id := range order {
		items = append(items, worst[id])
	}
	writeJSON(w, http.StatusOK, impactOf(items, notes...))
}

func controlPlanes(servers []store.Server) []store.Server {
	var out []store.Server
	for _, server := range servers {
		if server.ControlPlane {
			out = append(out, server)
		}
	}
	return out
}

func filterCluster(servers []store.Server, clusterID string) []store.Server {
	var out []store.Server
	for _, server := range servers {
		if server.ClusterID == clusterID && server.Status != "failed" {
			out = append(out, server)
		}
	}
	return out
}

func (a *API) updateServer(w http.ResponseWriter, r *http.Request) {
	a.startUpdateJob(w, r, "", r.PathValue("id"))
}

func (a *API) updateCluster(w http.ResponseWriter, r *http.Request) {
	a.startUpdateJob(w, r, r.PathValue("id"), "")
}

func (a *API) startUpdateJob(w http.ResponseWriter, r *http.Request, clusterID, serverID string) {
	var body struct {
		K3s bool `json:"k3s"`
	}
	if err := readJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := userID(r.Context())
	servers, details, err := a.targetServers(r.Context(), owner, clusterID, serverID)
	if err != nil {
		a.writeJobErr(w, err)
		return
	}
	if serverID != "" && servers[0].ClusterID != "" {
		// A node is updated alone, but the cluster keeps one update at a time.
		clusterID = servers[0].ClusterID
	}
	raw, err := json.Marshal(planUpdate(servers, details, body.K3s))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	job := store.UpdateJob{ID: newID(), OwnerID: owner, ClusterID: clusterID, ServerID: serverID, Steps: raw}
	if err := a.Store.StartUpdateJob(r.Context(), job); err != nil {
		a.writeJobErr(w, err)
		return
	}
	name := servers[0].Name
	if clusterID != "" && serverID == "" {
		name = servers[0].ClusterName
	}
	a.note(r.Context(), owner, newID(), "server.update", "info", "Mise à jour de "+name+" lancée.", serverID, clusterID)
	go a.runUpdateJob(job.ID, owner)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": job.ID})
}

func (a *API) writeJobErr(w http.ResponseWriter, err error) {
	var msg errString
	switch {
	case errors.As(err, &msg):
		writeError(w, http.StatusConflict, string(msg))
	case errors.Is(err, store.ErrUpdateRunning):
		writeError(w, http.StatusConflict, "Une mise à jour est déjà en cours sur ces serveurs.")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Aucun serveur prêt à mettre à jour.")
	default:
		a.mapErr(w, err)
	}
}

func (a *API) resumeUpdateJob(w http.ResponseWriter, r *http.Request) {
	owner := userID(r.Context())
	job, err := a.Store.UpdateJob(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if job.Status != "failed" {
		writeError(w, http.StatusConflict, "Cette mise à jour n'a pas échoué.")
		return
	}
	var steps []updateStep
	if err := json.Unmarshal(job.Steps, &steps); err != nil {
		a.mapErr(w, err)
		return
	}
	rewind(steps)
	raw, _ := json.Marshal(steps)
	if err := a.Store.SaveUpdateJob(r.Context(), job.ID, "running", raw); err != nil {
		a.mapErr(w, err)
		return
	}
	go a.runUpdateJob(job.ID, owner)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": job.ID})
}

// rewind starts the failed server again from its first step: its node was
// handed back to the cluster when the step failed.
func rewind(steps []updateStep) {
	for i, step := range steps {
		if step.Status != "failed" {
			continue
		}
		// Once given back, the node needs no new emptying: only the wait goes on.
		from := step.Action == "settle"
		for j := range steps {
			if (steps[j].ServerID == step.ServerID && (!from || j >= i)) || j > i {
				steps[j].Status, steps[j].Detail = "pending", ""
			}
		}
		return
	}
}

func (a *API) cancelUpdateJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.Store.UpdateJob(r.Context(), userID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if job.Status != "running" {
		writeError(w, http.StatusConflict, "Cette mise à jour est terminée.")
		return
	}
	updateJobMu.Lock()
	updateJobCancel[job.ID] = true
	busy := updateJobBusy[job.ID]
	updateJobMu.Unlock()
	if !busy {
		var steps []updateStep
		_ = json.Unmarshal(job.Steps, &steps)
		skipRest(steps, 0)
		raw, _ := json.Marshal(steps)
		_ = a.Store.SaveUpdateJob(r.Context(), job.ID, "cancelled", raw)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
}

func skipRest(steps []updateStep, from int) {
	for i := from; i < len(steps); i++ {
		if steps[i].Status == "pending" || steps[i].Status == "running" {
			steps[i].Status = "skipped"
		}
	}
}

// UpdateJobs resumes the server updates MyInfra was running when it stopped.
func (a *API) resumeUpdateJobs() {
	jobs, err := a.Store.RunningUpdateJobs(context.Background())
	if err != nil {
		return
	}
	for _, job := range jobs {
		go a.runUpdateJob(job.ID, job.OwnerID)
	}
}

// runUpdateJob runs the steps of a job one after the other, saving each.
func (a *API) runUpdateJob(id, owner string) {
	jobCtx, stop := context.WithCancel(context.Background())
	defer stop()
	updateJobMu.Lock()
	if updateJobBusy[id] {
		updateJobMu.Unlock()
		return
	}
	updateJobBusy[id] = true
	updateJobStop[id] = stop
	updateJobMu.Unlock()
	defer func() {
		updateJobMu.Lock()
		delete(updateJobBusy, id)
		delete(updateJobCancel, id)
		delete(updateJobStop, id)
		updateJobMu.Unlock()
	}()
	ctx := context.Background()
	job, err := a.Store.UpdateJob(ctx, owner, id)
	if err != nil || job.Status != "running" {
		return
	}
	var steps []updateStep
	if err := json.Unmarshal(job.Steps, &steps); err != nil {
		return
	}
	// save returns false once the job was closed elsewhere: its server or
	// its cluster is being removed. The job then stops where it is.
	save := func(status string) bool {
		raw, _ := json.Marshal(steps)
		return !errors.Is(a.Store.SaveUpdateJob(ctx, id, status, raw), store.ErrUpdateStopped)
	}
	for i := range steps {
		if steps[i].Status == "done" || steps[i].Status == "skipped" {
			continue
		}
		if jobCtx.Err() != nil {
			return
		}
		// A server removed since the job began has nothing left to update.
		if _, err := a.Store.Server(ctx, owner, steps[i].ServerID); errors.Is(err, store.ErrNotFound) {
			skipServer(steps, i, "Serveur supprimé.")
			if !save("running") {
				return
			}
			continue
		}
		updateJobMu.Lock()
		cancelled := updateJobCancel[id]
		updateJobMu.Unlock()
		// A cancel waits for the server in progress to be handed back.
		if cancelled && (steps[i].Action == "check" || steps[i].Action == "packages" && (i == 0 || steps[i-1].ServerID != steps[i].ServerID)) {
			skipRest(steps, i)
			save("cancelled")
			a.note(ctx, owner, newID(), "server.update", "warn", "Mise à jour arrêtée à la demande.", job.ServerID, job.ClusterID)
			return
		}
		steps[i].Status = "running"
		if !save("running") {
			return
		}
		stepCtx, cancel := context.WithTimeout(jobCtx, stepTimeout(steps[i].Action))
		err := a.runUpdateStep(stepCtx, owner, steps, i, func() { save("running") })
		cancel()
		if jobCtx.Err() != nil {
			return
		}
		if err != nil {
			if _, gone := a.Store.Server(ctx, owner, steps[i].ServerID); errors.Is(gone, store.ErrNotFound) {
				skipServer(steps, i, "Serveur supprimé.")
				if !save("running") {
					return
				}
				continue
			}
			steps[i].Status = "failed"
			steps[i].Detail = clip(err.Error(), 300)
			a.handBack(ctx, owner, steps[i])
			save("failed")
			a.note(ctx, owner, newID(), "server.update", "bad", "Mise à jour de "+steps[i].Name+" interrompue : "+steps[i].Detail, steps[i].ServerID, job.ClusterID)
			return
		}
		if steps[i].Status == "running" {
			steps[i].Status = "done"
		}
		if !save("running") {
			return
		}
		if last := i == len(steps)-1 || steps[i+1].ServerID != steps[i].ServerID; last {
			_ = a.Store.ForgetUpdate(ctx, "server", steps[i].ServerID)
		}
	}
	save("done")
	a.note(ctx, owner, newID(), "server.update", "ok", "Mise à jour terminée.", job.ServerID, job.ClusterID)
	go a.checkDue(context.Background(), false)
}

// skipServer passes the remaining steps of the server of step from.
func skipServer(steps []updateStep, from int, detail string) {
	for j := from; j < len(steps) && steps[j].ServerID == steps[from].ServerID; j++ {
		if steps[j].Status != "done" {
			steps[j].Status, steps[j].Detail = "skipped", ""
		}
	}
	steps[from].Detail = detail
}

// stopUpdateJobs ends the updates of a server or a cluster about to be
// removed, the running step included.
func (a *API) stopUpdateJobs(ctx context.Context, owner, serverID, clusterID string) {
	ids, err := a.Store.CloseUpdateJobs(ctx, owner, serverID, clusterID)
	if err != nil {
		return
	}
	updateJobMu.Lock()
	defer updateJobMu.Unlock()
	for _, id := range ids {
		if stop := updateJobStop[id]; stop != nil {
			stop()
		}
	}
}

func stepTimeout(action string) time.Duration {
	switch action {
	case "drain", "reboot", "k3s":
		return 12 * time.Minute
	case "packages":
		return 30 * time.Minute
	case "settle", "ready":
		return 12 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// handBack gives a node back to its cluster after a failed step, when it
// still answers: a node left cordoned holds no copy.
func (a *API) handBack(ctx context.Context, owner string, step updateStep) {
	server, err := a.Store.Server(ctx, owner, step.ServerID)
	if err != nil || server.Status != "updating" {
		return
	}
	if server.ClusterID != "" {
		if host, err := a.kubeHost(ctx, owner, server); err == nil {
			_, _ = deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl uncordon %q >/dev/null 2>&1 || true", deploy.NodeName(server.Name)))
		}
	}
	_ = a.Store.SetServerStatus(ctx, server.ID, "updating", "linked")
}

// kubeHost is where to run kubectl about a node: another control plane when
// there is one, since the node itself restarts.
func (a *API) kubeHost(ctx context.Context, owner string, server store.Server) (deploy.Host, error) {
	if host, ok := a.otherPlane(ctx, owner, server); ok {
		return host, nil
	}
	if server.ControlPlane {
		return a.serverHost(ctx, owner, server.ID)
	}
	return a.clusterHost(ctx, owner, server.ClusterID)
}

func (a *API) runUpdateStep(ctx context.Context, owner string, steps []updateStep, index int, report func()) error {
	step := &steps[index]
	server, err := a.Store.Server(ctx, owner, step.ServerID)
	if err != nil {
		return errString("Ce serveur n'existe plus.")
	}
	node := deploy.NodeName(server.Name)
	switch step.Action {
	case "check":
		if err := a.clusterReady(ctx, owner, server); err != nil {
			return err
		}
		if host, err := a.kubeHost(ctx, owner, server); err == nil {
			if waiting, err := notReadyServices(ctx, host); err == nil {
				step.Waiting = waiting
			}
		}
		return nil
	case "drain":
		if err := a.Store.SetServerStatus(ctx, server.ID, "linked", "updating"); err != nil {
			return err
		}
		host, err := a.kubeHost(ctx, owner, server)
		if err != nil {
			return err
		}
		if _, err := deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl cordon %q", node)); err != nil {
			return errString("Le nœud n'a pas pu être mis à l'écart.")
		}
		a.movePrimaries(ctx, host, node)
		// Without --force: copies leave one at a time, as their budgets allow.
		if _, err := deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl drain %q --ignore-daemonsets --delete-emptydir-data --timeout=600s", node)); err != nil {
			return errString("Le nœud ne s'est pas vidé en 10 minutes : une copie ne trouve pas d'autre serveur.")
		}
		return nil
	case "packages":
		if server.ClusterID == "" {
			if err := a.Store.SetServerStatus(ctx, server.ID, "linked", "updating"); err != nil {
				return err
			}
		}
		host, err := a.serverHost(ctx, owner, server.ID)
		if err != nil {
			return err
		}
		keys, err := a.serverKeysScript(ctx, owner, server.ID)
		if err != nil {
			return err
		}
		if _, err := a.nodeSSH(ctx, owner, server, host, packagesScript(keys)); err != nil {
			return errString("La mise à jour des paquets a échoué : " + commandTail(err))
		}
		return nil
	case "k3s":
		host, err := a.serverHost(ctx, owner, server.ID)
		if err != nil {
			return err
		}
		if !deploy.ValidK3sVersion(step.K3s) {
			return errString("Version de k3s illisible.")
		}
		if _, err := a.nodeSSH(ctx, owner, server, host, k3sUpgradeScript(step.K3s)); err != nil {
			return errString("La nouvelle version de k3s n'a pas pu être installée : " + commandTail(err))
		}
		return nil
	case "reboot":
		host, err := a.serverHost(ctx, owner, server.ID)
		if err != nil {
			return err
		}
		out, err := a.nodeSSH(ctx, owner, server, host, "if [ -f /var/run/reboot-required ]; then cat /proc/sys/kernel/random/boot_id; fi")
		if err != nil {
			return err
		}
		if step.BootID = strings.TrimSpace(out); step.BootID == "" {
			step.Status = "skipped"
			return nil
		}
		_, _ = deploy.SSH(ctx, host, "nohup sh -c 'sleep 2; systemctl reboot' >/dev/null 2>&1 &")
		return nil
	case "ready":
		return a.waitBack(ctx, owner, server, node)
	case "uncordon":
		host, err := a.kubeHost(ctx, owner, server)
		if err != nil {
			return err
		}
		if _, err := deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl uncordon %q", node)); err != nil {
			return errString("Le nœud n'a pas pu être rendu au cluster.")
		}
		return a.Store.SetServerStatus(ctx, server.ID, "updating", "linked")
	case "settle":
		var before []string
		for j := index; j >= 0 && steps[j].ServerID == step.ServerID; j-- {
			if steps[j].Action == "check" {
				before = steps[j].Waiting
			}
		}
		return a.waitSettled(ctx, owner, server, before, func(detail string) {
			if detail != step.Detail {
				step.Detail = detail
				report()
			}
		})
	}
	return errString("Étape inconnue.")
}

// packagesScript upgrades the packages without asking anything: the files a
// person changed stay theirs, new dependencies such as a new kernel are added.
// OpenSSH may be upgraded on the way: its penalties are softened before and
// after, and the service restarted at once, so that the old daemon does not
// start the new session program — a mismatch OpenSSH counts as a crash.
// Open sessions, this one included, survive the restart. MyInfra's keys are
// put back after: an agent of the provider may write authorized_keys again
// when its package is upgraded.
func packagesScript(keys string) string {
	return join.SSHPenalties + keys + `set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq -o DPkg::Lock::Timeout=300
apt-get -y -qq -o DPkg::Lock::Timeout=300 -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade --with-new-pkgs >/dev/null
systemctl try-restart ssh >/dev/null 2>&1 || systemctl try-restart sshd >/dev/null 2>&1 || true
set +e
` + join.SSHPenalties + keys
}

// k3sUpgradeScript replaces the k3s program of a node by the given version,
// checked against the sums of its release, then restarts it.
func k3sUpgradeScript(version string) string {
	return fmt.Sprintf(`set -e
case "$(uname -m)" in
  x86_64) file=k3s; sums=sha256sum-amd64.txt ;;
  aarch64) file=k3s-arm64; sums=sha256sum-arm64.txt ;;
  *) echo "architecture inconnue"; exit 1 ;;
esac
base="https://github.com/k3s-io/k3s/releases/download/$(printf '%%s' '%s' | sed 's/+/%%2B/')"
tmp=$(mktemp -d)
curl -sfL -o "$tmp/$file" "$base/$file"
curl -sfL -o "$tmp/sums" "$base/$sums"
want=$(awk -v f="$file" '$2 == f {print $1}' "$tmp/sums")
[ -n "$want" ]
echo "$want  $tmp/$file" | sha256sum -c - >/dev/null
chmod 755 "$tmp/$file"
mv "$tmp/$file" /usr/local/bin/k3s
rm -rf "$tmp"
if systemctl is-enabled --quiet k3s 2>/dev/null; then systemctl restart k3s; else systemctl restart k3s-agent; fi
`, version)
}

// patientSSH runs a command on a server, waiting through a refusal that
// lasts: a restart, or OpenSSH shutting an address out for a while.
func patientSSH(ctx context.Context, host deploy.Host, command string) (string, error) {
	for {
		out, err := deploy.SSH(ctx, host, command)
		if err == nil {
			return out, nil
		}
		var cmd *deploy.CommandError
		if errors.As(err, &cmd) {
			return "", err
		}
		// Asking again with a refused key only gets the address shut out.
		if keyRefused(err) {
			return "", errKeyRefused
		}
		select {
		case <-ctx.Done():
			if strings.Contains(strings.ToLower(err.Error()), "not allowed at this time") {
				return "", errString("Le serveur refuse pour l'instant les connexions SSH de cette adresse : OpenSSH la bloque après des connexions interrompues. Reprenez dans une dizaine de minutes.")
			}
			return "", errString("Le serveur ne répond pas en SSH.")
		case <-time.After(20 * time.Second):
		}
	}
}

var errKeyRefused = errString("Le serveur refuse la clé SSH de MyInfra.")

func keyRefused(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unable to authenticate") || strings.Contains(text, "no supported methods remain")
}

// serverKeysScript puts back the public keys MyInfra opens a server with.
func (a *API) serverKeysScript(ctx context.Context, owner, serverID string) (string, error) {
	keyIDs, err := a.Store.ServerKeyIDs(ctx, serverID)
	if err != nil {
		return "", err
	}
	keys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
	if err != nil {
		return "", err
	}
	return join.KeysFile(keys)
}

// nodeSSH runs a command on a server like patientSSH, and when a cluster
// node keeps refusing, repairs its SSH service through Kubernetes first.
func (a *API) nodeSSH(ctx context.Context, owner string, server store.Server, host deploy.Host, command string) (string, error) {
	first, cancel := context.WithTimeout(ctx, 3*time.Minute)
	out, err := patientSSH(first, host, command)
	cancel()
	if errors.Is(err, errKeyRefused) && server.ClusterID == "" {
		return "", errString("Le serveur refuse la clé SSH de MyInfra : elle a été retirée de /root/.ssh/authorized_keys. Remettez-la depuis la console de l'hébergeur.")
	}
	if err == nil || server.ClusterID == "" || ctx.Err() != nil {
		return out, err
	}
	var cmd *deploy.CommandError
	if errors.As(err, &cmd) {
		return "", err
	}
	if repairErr := a.repairSSH(ctx, owner, server); repairErr != nil {
		return "", err
	}
	return patientSSH(ctx, host, command)
}

// sshRepairPod is a pod that runs script in the namespaces of the node's
// first process, as root: on the node itself. JSON keeps the script intact.
func sshRepairPod(name, node, script string) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "namespace": "kube-system"},
		"spec": map[string]any{
			"nodeName":      node,
			"hostPID":       true,
			"restartPolicy": "Never",
			"tolerations":   []any{map[string]any{"operator": "Exists"}},
			"containers": []any{map[string]any{
				"name":            "repair",
				"image":           "rancher/mirrored-library-busybox:1.37.0",
				"securityContext": map[string]any{"privileged": true},
				"command":         []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "sh", "-c", script},
			}},
		},
	})
	return string(raw), err
}

// repairSSH puts MyInfra's keys back on a cluster node and restarts its SSH
// service, from another node, through a privileged pod on it. After an upgrade of OpenSSH, a daemon left
// running starts a session program of another version: every connection
// dies, and OpenSSH shuts the address out on top of it.
func (a *API) repairSSH(ctx context.Context, owner string, server store.Server) error {
	host, ok := a.otherPlane(ctx, owner, server)
	if !ok {
		return errString("Aucun autre pilote du cluster ne peut réparer ce serveur.")
	}
	keys, err := a.serverKeysScript(ctx, owner, server.ID)
	if err != nil {
		return err
	}
	name := "myinfra-ssh-repair-" + randomHex(4)
	script := join.SSHPenalties + keys + "systemctl restart ssh 2>/dev/null || systemctl restart sshd\nsystemctl is-active ssh 2>/dev/null || systemctl is-active sshd\n"
	manifest, err := sshRepairPod(name, deploy.NodeName(server.Name), script)
	if err != nil {
		return err
	}
	if _, err := deploy.SSH(ctx, host, deploy.KubectlApply(manifest)); err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = deploy.SSH(clean, host, "k3s kubectl -n kube-system delete pod "+name+" --wait=false --ignore-not-found")
	}()
	if _, err := deploy.SSH(ctx, host, "k3s kubectl -n kube-system wait --for=jsonpath='{.status.phase}'=Succeeded pod/"+name+" --timeout=120s"); err != nil {
		logs, _ := deploy.SSH(ctx, host, "k3s kubectl -n kube-system logs "+name+" --tail=20")
		return errString("Le service SSH n'a pas pu être redémarré : " + clip(logs, 200))
	}
	return nil
}

func commandTail(err error) string {
	var cmd *deploy.CommandError
	if errors.As(err, &cmd) && strings.TrimSpace(cmd.Text) != "" {
		return clip(cmd.Text, 200)
	}
	return clip(err.Error(), 200)
}

// waitBack waits for a server to answer again after its restart, then, in a
// cluster, for its node to be ready.
func (a *API) waitBack(ctx context.Context, owner string, server store.Server, node string) error {
	host, err := a.serverHost(ctx, owner, server.ID)
	if err != nil {
		return err
	}
	for {
		if a.isBack(ctx, owner, server, host, node) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errString("Le serveur n'est pas revenu après son redémarrage.")
		case <-time.After(10 * time.Second):
		}
	}
}

func (a *API) isBack(ctx context.Context, owner string, server store.Server, host deploy.Host, node string) bool {
	out, err := deploy.SSH(ctx, host, "systemctl is-system-running 2>/dev/null || true; if command -v docker >/dev/null 2>&1; then docker info >/dev/null 2>&1 && echo docker-ok; fi")
	if err != nil || strings.Contains(out, "starting") {
		return false
	}
	if server.ClusterID == "" {
		return strings.Contains(out, "docker-ok")
	}
	kube, err := a.kubeHost(ctx, owner, server)
	if err != nil {
		return false
	}
	ready, _ := deploy.SSH(ctx, kube, fmt.Sprintf("k3s kubectl get node %q -o jsonpath='{.status.conditions[?(@.type==\"Ready\")].status}'", node))
	return strings.TrimSpace(ready) == "True"
}

// clusterReady refuses to empty a node while the cluster could not take its
// copies: a node not ready, the object store missing a node, a replicated
// database missing an instance.
func (a *API) clusterReady(ctx context.Context, owner string, server store.Server) error {
	host, err := a.kubeHost(ctx, owner, server)
	if err != nil {
		return err
	}
	out, err := deploy.SSH(ctx, host, "k3s kubectl get nodes -o json")
	if err != nil {
		return errString("Le cluster ne répond pas.")
	}
	var nodes struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Unschedulable bool `json:"unschedulable"`
			} `json:"spec"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		return errString("Le cluster ne répond pas.")
	}
	self := deploy.NodeName(server.Name)
	for _, item := range nodes.Items {
		ready := false
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
			}
		}
		if !ready {
			return errString(fmt.Sprintf("Le nœud %s n'est pas prêt : la mise à jour attend que le cluster soit complet.", item.Metadata.Name))
		}
		if item.Spec.Unschedulable && item.Metadata.Name != self {
			return errString(fmt.Sprintf("Le nœud %s est déjà mis à l'écart.", item.Metadata.Name))
		}
	}
	if ok, err := a.storageComplete(ctx, host); err == nil && !ok {
		return errString("Le stockage du cluster n'a pas toutes ses copies : la mise à jour attend qu'il soit complet.")
	}
	if missing := databasesMissing(ctx, host); missing != "" {
		return errString("La base répliquée " + missing + " n'a pas toutes ses instances : la mise à jour attend qu'elle soit complète.")
	}
	return nil
}

// storageComplete is true when every node of the object store answers, or
// when the cluster has no object store.
func (a *API) storageComplete(ctx context.Context, host deploy.Host) (bool, error) {
	missing, err := a.storageMissing(ctx, host)
	return missing == "", err
}

// storageMissing names a node of the object store that does not answer,
// empty when the store is complete or the cluster has none. A node whose
// server left the cluster does not count: the store drops it after an hour.
func (a *API) storageMissing(ctx context.Context, host deploy.Host) (string, error) {
	out, err := deploy.SSH(ctx, host, "k3s kubectl get ns "+manifest.StorageNamespace+" -o name --ignore-not-found; echo ---; k3s kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{\"\\n\"}{end}'")
	if err != nil {
		return "", err
	}
	storage, nodeList, _ := strings.Cut(out, "---")
	if strings.TrimSpace(storage) == "" {
		return "", nil
	}
	nodes := map[string]bool{}
	for _, name := range strings.Fields(nodeList) {
		nodes[name] = true
	}
	var status garageStatus
	if err := a.garage(ctx, host, "GetClusterStatus", nil, &status); err != nil {
		return "?", nil
	}
	for _, node := range status.Nodes {
		if node.Role == nil || node.IsUp || !nodes[node.Role.Zone] {
			continue
		}
		return node.Role.Zone, nil
	}
	return "", nil
}

// databasesMissing names a replicated database that lacks an instance.
func databasesMissing(ctx context.Context, host deploy.Host) string {
	out, err := deploy.SSH(ctx, host, "k3s kubectl get clusters.postgresql.cnpg.io -A -o json 2>/dev/null || true")
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	var listed struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Instances int `json:"instances"`
			} `json:"spec"`
			Status struct {
				ReadyInstances int `json:"readyInstances"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(out), &listed) != nil {
		return ""
	}
	for _, item := range listed.Items {
		if item.Status.ReadyInstances < item.Spec.Instances {
			return item.Metadata.Namespace + "/" + item.Metadata.Name
		}
	}
	return ""
}

// movePrimaries hands the primary of each replicated database on the node to
// an instance elsewhere, before the node is emptied: the operator keeps its
// primary from being evicted.
func (a *API) movePrimaries(ctx context.Context, host deploy.Host, node string) {
	out, err := deploy.SSH(ctx, host, "k3s kubectl get pods -A -l cnpg.io/cluster -o json 2>/dev/null || true")
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name      string            `json:"name"`
				Namespace string            `json:"namespace"`
				Labels    map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				NodeName string `json:"nodeName"`
			} `json:"spec"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(out), &pods) != nil {
		return
	}
	type key struct{ ns, cluster string }
	primaries := map[key]bool{}
	standby := map[key]string{}
	for _, pod := range pods.Items {
		k := key{pod.Metadata.Namespace, pod.Metadata.Labels["cnpg.io/cluster"]}
		role := pod.Metadata.Labels["cnpg.io/instanceRole"]
		if role == "" {
			role = pod.Metadata.Labels["role"]
		}
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
			}
		}
		switch {
		case role == "primary" && pod.Spec.NodeName == node:
			primaries[k] = true
		case role != "primary" && pod.Spec.NodeName != node && ready:
			standby[k] = pod.Metadata.Name
		}
	}
	for k := range primaries {
		target := standby[k]
		if target == "" || !safeShell(target) || !safeShell(k.ns) || !safeShell(k.cluster) {
			continue
		}
		patch := fmt.Sprintf(`{"status":{"targetPrimary":"%s"}}`, target)
		_, _ = deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl -n %s patch clusters.postgresql.cnpg.io %s --type merge --subresource status -p '%s'", k.ns, k.cluster, patch))
		for i := 0; i < 24; i++ {
			current, _ := deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl -n %s get clusters.postgresql.cnpg.io %s -o jsonpath='{.status.currentPrimary}'", k.ns, k.cluster))
			if strings.TrimSpace(current) == target {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// notReadyServices lists the services of the cluster with a copy that is not
// ready, as namespace/service.
func notReadyServices(ctx context.Context, host deploy.Host) ([]string, error) {
	out, err := deploy.SSH(ctx, host, "k3s kubectl get pods -A -l myinfra.io/service -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.labels.myinfra\\.io/service} {.status.phase} {.status.containerStatuses[*].ready}{\"\\n\"}{end}'")
	if err != nil {
		return nil, err
	}
	return parseNotReady(out), nil
}

func parseNotReady(out string) []string {
	seen := map[string]bool{}
	var waiting []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] == "Succeeded" {
			continue
		}
		ready := fields[1] == "Running" && len(fields) > 2 && !strings.Contains(line, "false")
		if !ready && !seen[fields[0]] {
			seen[fields[0]] = true
			waiting = append(waiting, fields[0])
		}
	}
	sort.Strings(waiting)
	return waiting
}

// waitSettled waits, before the next node, for the services that were ready
// before the node was emptied to be ready again, and for the object store to
// be complete. It reports what it still waits for.
func (a *API) waitSettled(ctx context.Context, owner string, server store.Server, before []string, report func(string)) error {
	host, err := a.kubeHost(ctx, owner, server)
	if err != nil {
		return err
	}
	last := ""
	for {
		waiting, err := notReadyServices(ctx, host)
		if err == nil {
			var still []string
			for _, name := range waiting {
				if !slices.Contains(before, name) {
					still = append(still, name)
				}
			}
			missing, _ := a.storageMissing(ctx, host)
			switch {
			case len(still) == 0 && missing == "":
				report("")
				return nil
			case len(still) > 0:
				last = "Copies pas encore prêtes : " + strings.Join(still, ", ") + "."
			default:
				last = "Le stockage attend le nœud " + missing + "."
			}
			report(last)
		}
		select {
		case <-ctx.Done():
			if last == "" {
				last = "Le cluster ne répond pas."
			}
			return errString("Les copies ne sont pas revenues après la mise à jour du nœud : la suite attend. " + last)
		case <-time.After(10 * time.Second):
		}
	}
}
