package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"myinfra/internal/agent"
	"myinfra/internal/boot"
	"myinfra/internal/box"
	"myinfra/internal/catalog"
	"myinfra/internal/deploy"
	"myinfra/internal/provision"
	"myinfra/internal/store"
)

func (a *API) retryDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := userID(ctx)
	id := r.PathValue("id")
	row, err := a.Store.Server(ctx, owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if row.Address == "" && row.ProviderID == "" {
		writeError(w, http.StatusConflict, "Cette machine n'a pas encore d'adresse.")
		return
	}
	if row.Address == "" && row.Status == "deploying" {
		writeError(w, http.StatusConflict, "Le déploiement est déjà en cours.")
		return
	}
	steps := stepsOf(row.Deploy)
	if len(steps) == 0 {
		steps = stepsFor(row.ProviderID != "", row.ClusterID)
	}
	if row.Address != "" {
		if !deploy.Done(steps, "machine") {
			steps = deploy.Mark(steps, "machine", "done", row.Address)
		}
		steps = deploy.Replay(steps)
	} else {
		steps = deploy.Resume(steps)
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	publicKeys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	raw, err := deploy.Encode(steps)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.ClaimDeploy(ctx, owner, id, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	plan := catalog.Plan{}
	if decoded := planOf(row.Plan); decoded != nil {
		plan = *decoded
	}
	go a.runDeploy(owner, id, row.Name, row.JoinToken, row.ProviderID, row.ClusterID, row.ClusterName, keyIDs, publicKeys, a.privateKey(ctx, owner, keyIDs), plan, row.Address, row.ProviderServerID, steps)
	row, err = a.Store.Server(ctx, owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.toServer(row))
}

func (a *API) runDeploy(owner, id, name, token, providerID, clusterID, clusterName string, keyIDs []string, publicKeys []string, privateKey string, plan catalog.Plan, address, machineID string, steps []deploy.Step) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	a.trackDeploy(id, cancel)
	defer a.forgetDeploy(id)
	a.noteServer(context.Background(), owner, id, "deploy.started", "info", "Déploiement lancé.")
	self, err := a.Store.Server(ctx, owner, id)
	if err != nil {
		return
	}
	if address == "" || !deploy.Done(steps, "machine") {
		steps = deploy.Mark(steps, "machine", "running", "Commande chez le provider.")
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		var err error
		address, machineID, err = a.purchase(ctx, owner, providerID, id, name, token, keyIDs, plan, clusterID != "")
		if err != nil {
			a.failDeploy(owner, id, steps, "machine", "", "", err.Error())
			return
		}
		deploy.ForgetHostKey(address)
		_ = a.Store.RememberMachine(context.Background(), owner, id, address, machineID)
		if _, err := a.Store.Server(ctx, owner, id); err != nil {
			_ = a.destroyMachine(ctx, owner, providerID, machineID)
			return
		}
		steps = deploy.Mark(steps, "machine", "done", address)
	}
	if !deploy.Done(steps, "ssh") {
		steps = deploy.Mark(steps, "ssh", "running", "Attente du port SSH.")
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		if err := deploy.WaitSSH(ctx, address, 4*time.Minute); err != nil {
			a.failDeploy(owner, id, steps, "ssh", address, machineID, err.Error())
			return
		}
		steps = deploy.Mark(steps, "ssh", "done", address)
	}
	if clusterID == "" {
		a.finishAlone(ctx, owner, id, address, machineID, privateKey, publicKeys, name, steps)
		return
	}
	index, err := a.Store.EnsureWireGuardIndex(ctx, id)
	if err != nil {
		a.failDeploy(owner, id, steps, firstOpen(steps), address, machineID, err.Error())
		return
	}
	host := deploy.Host{
		Address: address, PrivateKey: privateKey, PublicKeys: publicKeys, Cluster: clusterName,
		NodeName: deploy.NodeName(name), Role: "agent",
		WGAddress: deploy.WireGuardIP(index) + "/24", NodeIP: deploy.WireGuardIP(index),
	}
	if self.ControlPlane {
		host.Role = "server"
	}
	var leader store.Server
	hasLeader := false
	engine := deploy.Engine()
	if engine != "k3s" {
		host.NodeIP = ""
	}
	labels := map[string]string{
		"keys":       "Dépôt des clés SSH.",
		"wireguard":  "Réseau WireGuard du cluster.",
		"kubernetes": kubernetesDetail(engine),
		"cluster":    "kubeconfig du cluster " + clusterName + ".",
	}
	for _, tag := range []string{"keys", "wireguard", "kubernetes", "cluster"} {
		if deploy.Done(steps, tag) {
			continue
		}
		steps = deploy.Mark(steps, tag, "running", labels[tag])
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		if tag == "wireguard" {
			// The nodes must reach each other on UDP 51820 before the tunnel exists.
			if err := a.reconcileFirewall(ctx, owner, clusterID); err != nil {
				a.failDeploy(owner, id, steps, tag, address, machineID, err.Error())
				return
			}
		}
		if tag == "kubernetes" {
			join, err := a.joinsCluster(ctx, owner, self)
			if err != nil {
				a.failDeploy(owner, id, steps, tag, address, machineID, "Le déploiement n'a pas pu continuer.")
				return
			}
			if join {
				leader, err = a.waitReadyLeader(ctx, owner, clusterID, id)
				if err != nil {
					a.failDeploy(owner, id, steps, tag, address, machineID, "Le control plane n'est pas encore prêt.")
					return
				}
				hasLeader = true
				host.ServerURL = "https://" + leader.Address + ":6443"
				if engine == "k3s" {
					indexes, err := a.Store.WireGuardIndexes(ctx, clusterID)
					if err != nil || indexes[leader.ID] == 0 {
						a.failDeploy(owner, id, steps, tag, address, machineID, "Le control plane n'a pas d'adresse WireGuard.")
						return
					}
					host.ServerURL = "https://" + deploy.WireGuardIP(indexes[leader.ID]) + ":6443"
				}
				if self.ControlPlane {
					out, err := deploy.SSH(ctx, deploy.Host{Address: leader.Address, PrivateKey: privateKey}, "if [ -d /var/lib/rancher/k3s/server/db/etcd ]; then echo ok; fi")
					if err != nil || !strings.Contains(out, "ok") {
						a.failDeploy(owner, id, steps, tag, address, machineID, "Ce control plane ne peut pas en accueillir un autre.")
						return
					}
				}
				k3sToken, err := deploy.SSH(ctx, deploy.Host{Address: leader.Address, PrivateKey: privateKey}, "cat /var/lib/rancher/k3s/server/node-token")
				if err != nil {
					a.failDeploy(owner, id, steps, tag, address, machineID, err.Error())
					return
				}
				host.Token = strings.TrimSpace(k3sToken)
				// The new node runs the Kubernetes of its cluster, whatever MyInfra pins.
				if out, err := deploy.SSH(ctx, deploy.Host{Address: leader.Address, PrivateKey: privateKey}, k3sVersionCommand); err == nil && deploy.ValidK3sVersion(strings.TrimSpace(out)) {
					host.K3sVersion = strings.TrimSpace(out)
				}
			}
		}
		if err := deploy.Play(ctx, host, tag); err != nil {
			a.failDeploy(owner, id, steps, tag, address, machineID, err.Error())
			return
		}
		if tag == "wireguard" {
			err := func() error {
				a.deployMu.Lock()
				defer a.deployMu.Unlock()
				return a.reconcileWireGuard(ctx, owner, clusterID, id, address, privateKey, publicKeys, clusterName)
			}()
			if err != nil {
				a.failDeploy(owner, id, steps, tag, address, machineID, err.Error())
				return
			}
		}
		if tag == "kubernetes" {
			verifyHost := host
			if hasLeader {
				verifyHost.Address = leader.Address
			}
			command := fmt.Sprintf("name=%q; i=0; while [ \"$i\" -lt 40 ]; do k3s kubectl get node/\"$name\" >/dev/null 2>&1 && break; i=$((i+1)); sleep 3; done; k3s kubectl wait --for=condition=Ready node/\"$name\" --timeout=180s", host.NodeName)
			if _, err := deploy.SSH(ctx, verifyHost, command); err != nil {
				a.failDeploy(owner, id, steps, tag, address, machineID, err.Error())
				return
			}
		}
		steps = deploy.Mark(steps, tag, "done", labels[tag])
	}
	_ = a.saveDeploy(owner, id, "linked", address, machineID, steps)
	a.noteServer(context.Background(), owner, id, "deploy.linked", "ok", "Le serveur est prêt.")
	go a.publishPending(context.Background())
}

// finishAlone prepares a machine that runs alone: keys, then Docker and the
// proxy. The applications are then delivered over SSH by the delivery loop.
func (a *API) finishAlone(ctx context.Context, owner, id, address, machineID, privateKey string, publicKeys []string, name string, steps []deploy.Step) {
	host := deploy.Host{Address: address, PrivateKey: privateKey, PublicKeys: publicKeys, NodeName: deploy.NodeName(name), Role: "server"}
	if !deploy.Done(steps, "keys") {
		steps = deploy.Mark(steps, "keys", "running", "Dépôt des clés SSH.")
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		if err := deploy.Play(ctx, host, "keys"); err != nil {
			a.failDeploy(owner, id, steps, "keys", address, machineID, err.Error())
			return
		}
		steps = deploy.Mark(steps, "keys", "done", "Dépôt des clés SSH.")
	}
	if !deploy.Done(steps, "docker") {
		steps = deploy.Mark(steps, "docker", "running", "Installation de Docker et du proxy.")
		if err := a.saveDeploy(owner, id, "deploying", address, machineID, steps); err != nil {
			return
		}
		install, err := agent.Install()
		if err == nil {
			_, err = deploy.SSH(ctx, host, deploy.RunScript("cloud-init status --wait >/dev/null 2>&1 || true\n"+install))
		}
		if err != nil {
			a.failDeploy(owner, id, steps, "docker", address, machineID, err.Error())
			return
		}
		steps = deploy.Mark(steps, "docker", "done", "Docker et proxy prêts.")
	}
	_ = a.saveDeploy(owner, id, "linked", address, machineID, steps)
	a.noteServer(context.Background(), owner, id, "deploy.linked", "ok", "Le serveur est prêt.")
	go a.publishPending(context.Background())
	go a.deliverClusters(context.Background())
}

func firstOpen(steps []deploy.Step) string {
	for _, step := range steps {
		if step.Status != "done" {
			return step.ID
		}
	}
	return "machine"
}

func (a *API) reconcileWireGuard(ctx context.Context, owner, clusterID, currentID, currentAddress, privateKey string, publicKeys []string, clusterName string) error {
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return err
	}
	indexes, err := a.Store.WireGuardIndexes(ctx, clusterID)
	if err != nil {
		return err
	}
	type wireNode struct {
		row     store.Server
		address string
		key     string
		index   int
	}
	var wire []wireNode
	for _, row := range rows {
		if row.ClusterID != clusterID || row.Address == "" || indexes[row.ID] == 0 {
			continue
		}
		if row.Status != "linked" && row.Status != "deploying" && row.ID != currentID {
			continue
		}
		address := row.Address
		if row.ID == currentID {
			address = currentAddress
		}
		publicKey, err := deploy.SSH(ctx, deploy.Host{Address: address, PrivateKey: privateKey}, "cat /etc/wireguard/myinfra.pub 2>/dev/null || true")
		if err != nil {
			if row.ID == currentID {
				return err
			}
			continue
		}
		publicKey = strings.TrimSpace(publicKey)
		if publicKey == "" {
			// This node has not reached its WireGuard step yet; it will add itself.
			if row.ID == currentID {
				return fmt.Errorf("La clé WireGuard du nœud est introuvable.")
			}
			continue
		}
		wire = append(wire, wireNode{row: row, address: address, key: publicKey, index: indexes[row.ID]})
	}
	sort.Slice(wire, func(i, j int) bool { return wire[i].index < wire[j].index })
	for i, node := range wire {
		peers := make([]deploy.WireGuardPeer, 0, len(wire)-1)
		for j, peer := range wire {
			if i == j {
				continue
			}
			peers = append(peers, deploy.WireGuardPeer{
				PublicKey: peer.key,
				Address:   deploy.WireGuardIP(peer.index),
				Endpoint:  peer.address,
			})
		}
		host := deploy.Host{
			Address: node.address, PrivateKey: privateKey, PublicKeys: publicKeys, Cluster: clusterName,
			NodeName: deploy.NodeName(node.row.Name), WGAddress: deploy.WireGuardIP(node.index) + "/24", WGPeers: peers,
		}
		if err := deploy.Play(ctx, host, "wireguard"); err != nil {
			return err
		}
	}
	return a.applyFirewall(ctx, owner, clusterID)
}

func (a *API) reconcileFirewall(ctx context.Context, owner, clusterID string) error {
	a.deployMu.Lock()
	defer a.deployMu.Unlock()
	return a.applyFirewall(ctx, owner, clusterID)
}

func (a *API) applyFirewall(ctx context.Context, owner, clusterID string) error {
	if clusterID == "" || deploy.Engine() != "k3s" {
		return nil
	}
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return err
	}
	var nodes []store.Server
	for _, row := range rows {
		if row.ClusterID != clusterID || row.Address == "" {
			continue
		}
		if row.Status != "linked" && row.Status != "deploying" {
			continue
		}
		nodes = append(nodes, row)
	}
	if len(nodes) == 0 {
		return nil
	}
	peers := make([]string, 0, len(nodes))
	var planes []string
	for _, row := range nodes {
		peers = append(peers, row.Address)
		if row.ControlPlane {
			planes = append(planes, row.Address)
		}
	}
	for _, row := range nodes {
		// A node without its keys yet cannot be reached: it applies the
		// firewall itself at its WireGuard step. Waiting for it here would
		// fail the node being deployed.
		if row.Status == "deploying" && !deploy.Done(stepsOf(row.Deploy), "keys") {
			continue
		}
		keyIDs, err := a.Store.ServerKeyIDs(ctx, row.ID)
		if err != nil {
			return err
		}
		privateKey := a.privateKey(ctx, owner, keyIDs)
		if privateKey == "" {
			return fmt.Errorf("Aucune clé privée pour ouvrir la session SSH.")
		}
		publicKeys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
		if err != nil {
			return err
		}
		host := deploy.Host{
			Address: row.Address, PrivateKey: privateKey, PublicKeys: publicKeys, Cluster: row.ClusterName,
			NodeName: deploy.NodeName(row.Name), Peers: peers, ControlPlanes: planes,
		}
		if err := deploy.Play(ctx, host, "firewall"); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) joinsCluster(ctx context.Context, owner string, self store.Server) (bool, error) {
	if !self.ControlPlane {
		return true, nil
	}
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.ID == self.ID || row.ClusterID != self.ClusterID || !row.ControlPlane {
			continue
		}
		if row.CreatedAt.Before(self.CreatedAt) || (row.CreatedAt.Equal(self.CreatedAt) && row.ID < self.ID) {
			return true, nil
		}
	}
	return false, nil
}

func (a *API) waitReadyLeader(ctx context.Context, owner, clusterID, excludeID string) (store.Server, error) {
	for {
		leader, ok := a.clusterLeader(ctx, owner, clusterID, excludeID)
		if ok && kubernetesReady(leader) {
			return leader, nil
		}
		select {
		case <-ctx.Done():
			return store.Server{}, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *API) clusterLeader(ctx context.Context, owner, clusterID, excludeID string) (store.Server, bool) {
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return store.Server{}, false
	}
	var oldest, ready *store.Server
	for i := range rows {
		row := &rows[i]
		if row.ID == excludeID || row.ClusterID != clusterID || !row.ControlPlane {
			continue
		}
		if oldest == nil || row.CreatedAt.Before(oldest.CreatedAt) {
			oldest = row
		}
		if kubernetesReady(*row) && (ready == nil || row.CreatedAt.Before(ready.CreatedAt)) {
			ready = row
		}
	}
	if ready != nil {
		return *ready, true
	}
	if oldest == nil {
		return store.Server{}, false
	}
	return *oldest, true
}

func kubernetesReady(row store.Server) bool {
	if row.Status == "linked" && row.Address != "" {
		return true
	}
	for _, step := range stepsOf(row.Deploy) {
		if step.ID == "kubernetes" && step.Status == "done" && row.Address != "" {
			return true
		}
	}
	return false
}

func kubernetesDetail(engine string) string {
	if engine == "kubespray" {
		return "Kubespray, via Ansible."
	}
	return "k3s, via Ansible."
}

func (a *API) trackDeploy(id string, cancel context.CancelFunc) {
	a.deployStopMu.Lock()
	defer a.deployStopMu.Unlock()
	if a.deployStop == nil {
		a.deployStop = map[string]context.CancelFunc{}
	}
	a.deployStop[id] = cancel
}

func (a *API) forgetDeploy(id string) {
	a.deployStopMu.Lock()
	delete(a.deployStop, id)
	a.deployStopMu.Unlock()
}

func (a *API) abortDeploy(id string) {
	a.deployStopMu.Lock()
	cancel := a.deployStop[id]
	a.deployStopMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// RecoverDeploys takes up the deployments a restart cut off. The finished
// steps are kept; the purchase finds its machine again through its stack.
// A deployment that cannot start again is marked as failed.
func (a *API) RecoverDeploys(ctx context.Context) {
	owners, ids, raws, err := a.Store.DeployingServers(ctx)
	if err != nil {
		return
	}
	for i, id := range ids {
		if err := a.restartDeploy(ctx, owners[i], id, false); err == nil {
			a.noteServer(ctx, owners[i], id, "deploy.resumed", "info", "Déploiement repris après un redémarrage de MyInfra.")
			continue
		}
		steps := markDeployInterrupted(stepsOf(raws[i]))
		raw, err := deploy.Encode(steps)
		if err != nil {
			continue
		}
		if err := a.Store.SetDeploy(ctx, owners[i], id, "failed", "", "", raw); err != nil {
			continue
		}
		a.noteServer(ctx, owners[i], id, "deploy.failed", "bad", "Déploiement interrompu par un redémarrage de MyInfra.")
	}
}

func markDeployInterrupted(steps []deploy.Step) []deploy.Step {
	for i := range steps {
		if steps[i].Status == "running" {
			steps[i].Status = "failed"
			steps[i].Detail = "Interrompu par un redémarrage de MyInfra. Relancez le déploiement."
		}
	}
	return steps
}

func markDeployCancelled(steps []deploy.Step) []deploy.Step {
	for i := range steps {
		if steps[i].Status == "pending" || steps[i].Status == "running" {
			steps[i].Status = "failed"
			steps[i].Detail = "Annulé."
		}
	}
	return steps
}

func (a *API) stopServer(ctx context.Context, owner, id string) error {
	row, err := a.Store.Server(ctx, owner, id)
	if err != nil {
		return err
	}
	if row.Status != "pending" && row.Status != "deploying" {
		return store.ErrStopped
	}
	raw, err := deploy.Encode(markDeployCancelled(stepsOf(row.Deploy)))
	if err != nil {
		return err
	}
	if err := a.Store.SetDeploy(ctx, owner, id, "failed", "", "", raw); err != nil {
		return err
	}
	a.abortDeploy(id)
	return nil
}

func (a *API) saveDeploy(owner, id, status, address, machineID string, steps []deploy.Step) error {
	raw, err := deploy.Encode(steps)
	if err != nil {
		return err
	}
	return a.Store.SetDeployActive(context.Background(), owner, id, status, address, machineID, raw)
}

func (a *API) failDeploy(owner, id string, steps []deploy.Step, step, address, machineID, detail string) {
	steps = deploy.Mark(steps, step, "failed", detail)
	_ = a.saveDeploy(owner, id, "failed", address, machineID, steps)
	a.noteServer(context.Background(), owner, id, "deploy.failed", "bad", "Échec du déploiement. "+detail)
}

func (a *API) destroyMachine(ctx context.Context, owner, providerID, machineID string) error {
	if machineID == "" || providerID == "" {
		return nil
	}
	sec, tok, err := a.openToken(ctx, owner, providerID)
	if err != nil {
		return err
	}
	return provisionClient(a).Destroy(ctx, sec.Provider, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, machineID)
}

func tofuDir() string {
	if dir := os.Getenv("MYINFRA_TOFU_DIR"); dir != "" {
		return dir
	}
	home, err := boot.Home()
	if err != nil {
		return ""
	}
	return home + "/tofu"
}

func provisionClient(a *API) provision.Client {
	return provision.Client{
		HTTP: a.Verify.HTTP, HetznerBase: a.Verify.HetznerBase, DigitalOceanBase: a.Verify.DigitalOceanBase,
		ScalewayBase: a.Verify.ScalewayBase, OVHBase: a.Verify.OVHBase, InfomaniakBase: a.Verify.InfomaniakBase,
		UpCloudBase: a.Verify.UpCloudBase, VultrBase: a.Verify.VultrBase, AkamaiBase: a.Verify.AkamaiBase,
		ExoscaleBase: a.Verify.ExoscaleBase, GcoreBase: a.Verify.GcoreBase, KamateraBase: a.Verify.KamateraBase, IonosBase: a.Verify.IonosBase,
		TofuDir: tofuDir(),
	}
}

func (a *API) privateKey(ctx context.Context, owner string, ids []string) string {
	for _, id := range ids {
		sec, err := a.Store.Secret(ctx, owner, id)
		if err != nil || !sec.HasPrivate {
			continue
		}
		plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
		if err != nil || len(plain) == 0 {
			continue
		}
		return string(plain)
	}
	return ""
}

func (a *API) openToken(ctx context.Context, owner, id string) (store.Secret, storedToken, error) {
	sec, err := a.Store.Secret(ctx, owner, id)
	if err != nil || sec.Kind != "provider_token" {
		return store.Secret{}, storedToken{}, fmt.Errorf("Choisissez un provider.")
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		return store.Secret{}, storedToken{}, fmt.Errorf("Jeton illisible.")
	}
	var tok storedToken
	if err := json.Unmarshal(plain, &tok); err != nil {
		return store.Secret{}, storedToken{}, fmt.Errorf("Jeton illisible.")
	}
	return sec, tok, nil
}
