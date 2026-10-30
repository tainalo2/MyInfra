package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/catalog"
	"myinfra/internal/deploy"
	"myinfra/internal/store"
)

const (
	moveNotReady  = "La machine doit être prête."
	moveHasApps   = "Cette machine porte des applications. Retirez-les avant de la rattacher à un cluster."
	moveDisk      = "Cette machine porte un disque partagé du cluster. Déplacez ce disque avant."
	moveOnlyPlane = "C'est le seul plan de contrôle, et d'autres machines restent dans le cluster."
	moveOnlyNode  = "C'est la seule machine du cluster, et des applications y tournent."
	moveImported  = "Ce cluster est importé."
	movePick      = "Choisissez un cluster."
)

const leaveClusterScript = `
set -eu
if [ -x /usr/local/bin/k3s-uninstall.sh ]; then /usr/local/bin/k3s-uninstall.sh; fi
if [ -x /usr/local/bin/k3s-agent-uninstall.sh ]; then /usr/local/bin/k3s-agent-uninstall.sh; fi
systemctl disable --now wg-quick@myinfra >/dev/null 2>&1 || true
rm -f /etc/wireguard/myinfra.conf
`

const leaveAloneScript = `
set -eu
systemctl disable --now myinfra-sync >/dev/null 2>&1 || true
docker rm -f myinfra-proxy >/dev/null 2>&1 || true
systemctl disable --now docker >/dev/null 2>&1 || true
# Docker leaves forwarding closed. The pods of a cluster need it open.
iptables -P FORWARD ACCEPT >/dev/null 2>&1 || true
`

// attachCluster moves a ready machine to another cluster, or out to run alone.
// clusterId empty means alone. The work continues after the answer.
func (a *API) attachCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID    string `json:"clusterId"`
		ControlPlane bool   `json:"controlPlane"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if a.Replica {
		writeError(w, http.StatusConflict, replicaRefused)
		return
	}
	ctx := r.Context()
	owner := ownerID(ctx)
	row, err := a.Store.Server(ctx, owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	target := strings.TrimSpace(body.ClusterID)
	if target == row.ClusterID {
		writeJSON(w, http.StatusOK, a.toServer(row))
		return
	}
	if target != "" && a.refuseBusyTarget(w, r, "", target) {
		return
	}
	if row.Status != "linked" {
		writeError(w, http.StatusConflict, moveNotReady)
		return
	}
	if row.Address == "" {
		writeError(w, http.StatusConflict, "Cette machine n'a pas encore d'adresse.")
		return
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, row.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if a.privateKey(ctx, owner, keyIDs) == "" {
		writeError(w, http.StatusConflict, "Aucune clé privée pour ouvrir la session SSH.")
		return
	}
	facts, err := a.moveFacts(ctx, owner, row, target)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if msg := refuseMove(facts); msg != "" {
		status := http.StatusConflict
		if msg == movePick {
			status = http.StatusBadRequest
		}
		writeError(w, status, msg)
		return
	}
	control := body.ControlPlane && target != ""
	if target != "" && !clusterHasPlane(facts.Servers, target) {
		control = true
	}
	steps := deploy.MoveSteps(row.ClusterID, target, control)
	raw, err := deploy.Encode(steps)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.ClaimDeploy(ctx, owner, row.ID, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	go a.runMove(owner, row.ID, steps)
	row, err = a.Store.Server(ctx, owner, row.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.toServer(row))
}

type moveFacts struct {
	Server       store.Server
	TargetID     string
	TargetOrigin string
	TargetFound  bool
	SourceOrigin string
	Servers      []store.Server
	ServerApps   int
	ClusterApps  int
	HoldsDisk    bool
	HostsSelf    bool
}

func (a *API) moveFacts(ctx context.Context, owner string, row store.Server, target string) (moveFacts, error) {
	facts := moveFacts{Server: row, TargetID: target, TargetFound: target == ""}
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return facts, err
	}
	facts.Servers = servers
	if target != "" {
		cluster, err := a.Store.Cluster(ctx, owner, target)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return facts, nil
			}
			return facts, err
		}
		facts.TargetFound = true
		facts.TargetOrigin = cluster.Origin
		n, err := a.Store.CountDestinationApps(ctx, owner, "server", row.ID)
		if err != nil {
			return facts, err
		}
		facts.ServerApps = n
	}
	if row.ClusterID != "" {
		cluster, err := a.Store.Cluster(ctx, owner, row.ClusterID)
		if err != nil {
			return facts, err
		}
		facts.SourceOrigin = cluster.Origin
		n, err := a.Store.CountDestinationApps(ctx, owner, "cluster", row.ClusterID)
		if err != nil {
			return facts, err
		}
		facts.ClusterApps = n
		held, err := a.Store.ServerHoldsDisk(ctx, owner, row.ClusterID, row.ID)
		if err != nil {
			return facts, err
		}
		facts.HoldsDisk = held
		if self, ok := a.selfRecord(ctx); ok && self.ClusterID == row.ClusterID {
			facts.HostsSelf = true
		}
	}
	return facts, nil
}

func createdCluster(origin string) bool {
	return origin == "" || origin == "created"
}

func clusterHasPlane(servers []store.Server, clusterID string) bool {
	for _, row := range servers {
		if row.ClusterID == clusterID && row.ControlPlane {
			return true
		}
	}
	return false
}

// refuseMove is why a move must not start. Empty means it may.
func refuseMove(f moveFacts) string {
	if f.Server.ClusterID != "" && !createdCluster(f.SourceOrigin) {
		return moveImported
	}
	if f.TargetID != "" {
		if !f.TargetFound {
			return movePick
		}
		if !createdCluster(f.TargetOrigin) {
			return moveImported
		}
		if f.ServerApps > 0 {
			return moveHasApps
		}
	}
	if f.Server.ClusterID == "" {
		return ""
	}
	if f.HoldsDisk {
		return moveDisk
	}
	others, otherLinked := 0, 0
	otherPlane := false
	for _, row := range f.Servers {
		if row.ID == f.Server.ID || row.ClusterID != f.Server.ClusterID {
			continue
		}
		others++
		if row.Status == "linked" {
			otherLinked++
		}
		if row.ControlPlane {
			otherPlane = true
		}
	}
	if f.Server.ControlPlane && others > 0 && !otherPlane {
		return moveOnlyPlane
	}
	if otherLinked == 0 && (f.ClusterApps > 0 || f.HostsSelf) {
		return moveOnlyNode
	}
	return ""
}

// restartMove takes a move up again from the steps that are not done.
func (a *API) restartMove(ctx context.Context, owner string, row store.Server, steps []deploy.Step) error {
	raw, err := deploy.Encode(steps)
	if err != nil {
		return err
	}
	if err := a.Store.ClaimDeploy(ctx, owner, row.ID, raw); err != nil {
		return err
	}
	go a.runMove(owner, row.ID, steps)
	return nil
}

// runMove leaves the current role, records the new one, then joins it.
func (a *API) runMove(owner, id string, steps []deploy.Step) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	a.trackDeploy(id, cancel)
	defer a.forgetDeploy(id)
	source, target, control, ok := deploy.MoveOf(steps)
	if !ok {
		return
	}
	a.noteServer(context.Background(), owner, id, "deploy.started", "info", "Déplacement lancé.")
	row, err := a.Store.Server(ctx, owner, id)
	if err != nil {
		return
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, id)
	if err != nil {
		a.failDeploy(owner, id, steps, "leave", row.Address, row.ProviderServerID, "Le déplacement n'a pas pu continuer.")
		return
	}
	publicKeys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
	if err != nil {
		a.failDeploy(owner, id, steps, "leave", row.Address, row.ProviderServerID, "Le déplacement n'a pas pu continuer.")
		return
	}
	privateKey := a.privateKey(ctx, owner, keyIDs)
	plan := catalog.Plan{}
	if decoded := planOf(row.Plan); decoded != nil {
		plan = *decoded
	}
	address, machineID := row.Address, row.ProviderServerID
	if !deploy.Done(steps, "leave") {
		steps = deploy.Mark(steps, "leave", "running", "Sortie du rôle actuel.")
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		switched := row.ClusterID == target && row.ControlPlane == control
		if !switched {
			if err := a.leaveMachine(ctx, owner, row, privateKey); err != nil {
				a.failDeploy(owner, id, steps, "leave", address, machineID, err.Error())
				return
			}
			if err := a.Store.PlaceServer(ctx, owner, id, target, control); err != nil {
				a.failDeploy(owner, id, steps, "leave", address, machineID, err.Error())
				return
			}
		}
		if source != "" {
			name := ""
			if cluster, err := a.Store.Cluster(ctx, owner, source); err == nil {
				name = cluster.Name
			}
			err := func() error {
				a.deployMu.Lock()
				defer a.deployMu.Unlock()
				return a.reconcileWireGuard(ctx, owner, source, "", "", privateKey, publicKeys, name)
			}()
			if err != nil {
				a.failDeploy(owner, id, steps, "leave", address, machineID, err.Error())
				return
			}
			if err := a.reconcileFirewall(ctx, owner, source); err != nil {
				a.failDeploy(owner, id, steps, "leave", address, machineID, err.Error())
				return
			}
		}
		steps = deploy.Mark(steps, "leave", "done", "L'ancien rôle est quitté.")
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		row, err = a.Store.Server(ctx, owner, id)
		if err != nil {
			return
		}
	}
	if target == "" {
		a.finishAlone(ctx, owner, id, address, machineID, privateKey, publicKeys, row.Name, steps, plan)
		return
	}
	a.continueCluster(ctx, owner, id, row.Name, target, row.ClusterName, address, machineID, privateKey, publicKeys, plan, row, steps)
}

// leaveMachine uninstalls Kubernetes or stops Docker, before the record changes.
func (a *API) leaveMachine(ctx context.Context, owner string, srv store.Server, privateKey string) error {
	host := deploy.Host{Address: srv.Address, PrivateKey: privateKey}
	script := leaveAloneScript
	if srv.ClusterID != "" {
		if err := a.removeNode(ctx, owner, srv); err != nil {
			return err
		}
		script = leaveClusterScript
	}
	_, err := deploy.SSH(ctx, host, deploy.RunScript(script))
	return err
}
