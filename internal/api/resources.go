package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"myinfra/internal/catalog"
	"myinfra/internal/deploy"
	"myinfra/internal/join"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/provision"
	"myinfra/internal/registry"
	"myinfra/internal/store"
	"myinfra/internal/usage"
)

func (a *API) listServers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListServers(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]serverView, 0, len(rows))
	for _, row := range rows {
		out = append(out, a.toServer(row))
	}
	writeJSON(w, http.StatusOK, out)
}

type serverView struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Status       string        `json:"status"`
	ClusterID    string        `json:"clusterId,omitempty"`
	JoinCommand  string        `json:"joinCommand"`
	Address      string        `json:"address,omitempty"`
	ProviderID   string        `json:"providerId,omitempty"`
	Provider     string        `json:"provider,omitempty"`
	ProviderName string        `json:"providerName,omitempty"`
	ClusterName  string        `json:"clusterName,omitempty"`
	ControlPlane bool          `json:"controlPlane,omitempty"`
	Tofu         bool          `json:"tofu,omitempty"`
	Plan         *catalog.Plan `json:"plan,omitempty"`
	Deploy       []deploy.Step `json:"deploy,omitempty"`
	Cost         *billView     `json:"cost,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	// Reach is "lost" when the server stopped answering, "gone" when its
	// provider no longer has it.
	Reach             string     `json:"reach,omitempty"`
	LostSince         *time.Time `json:"lostSince,omitempty"`
	SeenAt            *time.Time `json:"seenAt,omitempty"`
	ProviderCheckedAt *time.Time `json:"providerCheckedAt,omitempty"`
	ProviderError     string     `json:"providerError,omitempty"`
	// Place is the city of a machine added by hand. A bought machine uses its plan.
	Place string `json:"place,omitempty"`
}

func (a *API) toServer(row store.Server) serverView {
	plan := planOf(row.Plan)
	view := serverView{
		ID: row.ID, Name: row.Name, Status: row.Status, ClusterID: row.ClusterID,
		JoinCommand:  join.Command(a.reachURL(), row.JoinToken),
		Address:      row.Address,
		ProviderID:   row.ProviderID,
		Provider:     row.Provider,
		ProviderName: row.ProviderName,
		ClusterName:  row.ClusterName,
		ControlPlane: row.ControlPlane,
		Tofu:         row.ProviderServerID != "" && provisionClient(a).HasTofuState(row.Provider, row.ProviderServerID),
		Plan:         plan,
		Deploy:       stepsOf(row.Deploy),
		CreatedAt:    row.CreatedAt,
		Place:        manualPlace(row.Plan),
	}
	if plan != nil {
		view.Cost = billOf(plan.Hourly, plan.Monthly, plan.Currency, row.CreatedAt)
	}
	a.reachServer(&view)
	return view
}

func stepsOf(raw []byte) []deploy.Step {
	if len(raw) == 0 {
		return nil
	}
	var steps []deploy.Step
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil
	}
	return steps
}

func manualPlace(raw []byte) string {
	if planOf(raw) != nil || len(raw) == 0 {
		return ""
	}
	var body struct {
		Location string `json:"location"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Location)
}

func planOf(raw []byte) *catalog.Plan {
	if len(raw) == 0 {
		return nil
	}
	var plan catalog.Plan
	if err := json.Unmarshal(raw, &plan); err != nil || plan.OfferID == "" {
		return nil
	}
	return &plan
}

func (a *API) createServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string   `json:"name"`
		KeyIDs       []string `json:"keyIds"`
		ProviderID   string   `json:"providerId"`
		ClusterID    string   `json:"clusterId"`
		OfferID      string   `json:"offerId"`
		Location     string   `json:"location"`
		Period       string   `json:"period"`
		StorageID    string   `json:"storageId"`
		StorageGB    int      `json:"storageGb"`
		ProjectID    string   `json:"projectId"`
		ControlPlane bool     `json:"controlPlane"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if a.refuseBusyTarget(w, r, "", body.ClusterID) {
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom à la machine.")
		return
	}
	providerID := strings.TrimSpace(body.ProviderID)
	if providerID == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un provider.")
		return
	}
	if providerID == "manual" {
		providerID = ""
	}
	if providerID != "" && a.Replica {
		writeError(w, http.StatusConflict, replicaRefused)
		return
	}
	keyIDs := uniqueIDs(body.KeyIDs)
	if len(keyIDs) == 0 {
		writeError(w, http.StatusBadRequest, "Choisissez au moins une clé SSH.")
		return
	}
	// Without a cluster, the machine runs alone: Docker and the proxy, no Kubernetes.
	clusterID := strings.TrimSpace(body.ClusterID)
	clusterName := ""
	if clusterID != "" {
		clusters, err := a.Store.ListClusters(r.Context(), ownerID(r.Context()))
		if err != nil {
			a.mapErr(w, err)
			return
		}
		for _, cluster := range clusters {
			if cluster.ID == clusterID {
				clusterName = cluster.Name
			}
		}
		if clusterName == "" {
			writeError(w, http.StatusBadRequest, "Choisissez un cluster.")
			return
		}
	}
	controlPlane := body.ControlPlane && clusterID != ""
	existing, err := a.Store.ListServers(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	hasControlPlane := false
	for _, row := range existing {
		if row.ClusterID == clusterID && row.ControlPlane {
			hasControlPlane = true
			break
		}
	}
	if !hasControlPlane && clusterID != "" {
		controlPlane = true
	}
	var plan catalog.Plan
	var rawPlan []byte
	if providerID != "" {
		sec, _, err := a.openProvider(r.Context(), ownerID(r.Context()), providerID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if needsProject(sec.Provider) && strings.TrimSpace(body.ProjectID) == "" {
			writeError(w, http.StatusBadRequest, "Choisissez un projet.")
			return
		}
		cat, err := a.offers(r, providerID, body.Location, body.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		plan, err = cat.Plan(strings.TrimSpace(body.OfferID), body.Period, strings.TrimSpace(body.StorageID), body.StorageGB)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		plan.ProjectID = strings.TrimSpace(body.ProjectID)
		rawPlan, err = json.Marshal(plan)
		if err != nil {
			a.mapErr(w, err)
			return
		}
	}
	id := newID()
	token := randomToken()
	owner := ownerID(r.Context())
	status := "pending"
	if providerID != "" {
		status = "deploying"
	}
	steps := stepsFor(providerID != "", clusterID)
	rawSteps, err := deploy.Encode(steps)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.CreateServer(r.Context(), owner, id, name, token, providerID, clusterID, status, rawPlan, rawSteps, keyIDs, controlPlane); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Server(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), owner, "opened-"+id, "server.opened", "info", "Serveur ouvert.", id, row.ClusterID)
	if providerID != "" {
		publicKeys, err := a.Store.SSHPublicKeys(r.Context(), owner, keyIDs)
		if err != nil {
			_ = a.Store.DeleteServer(context.Background(), owner, id)
			a.mapErr(w, err)
			return
		}
		privateKey := a.privateKey(r.Context(), owner, keyIDs)
		go a.runDeploy(owner, id, name, token, providerID, clusterID, clusterName, keyIDs, publicKeys, privateKey, plan, "", "", steps)
	}
	writeJSON(w, http.StatusCreated, a.toServer(row))
}

// machineScript is the first boot of a machine: the SSH key and the address
// MyInfra should call. Docker or Kubernetes come afterwards over SSH.
func stepsFor(cloud bool, clusterID string) []deploy.Step {
	if clusterID == "" {
		return deploy.AloneSteps(cloud)
	}
	return deploy.Steps(cloud)
}

func machineScript(baseURL, token string, keys []string, clusterNode bool) (string, error) {
	if clusterNode {
		return join.NodeScript(baseURL, token, keys)
	}
	return join.Script(baseURL, token, keys)
}

func (a *API) purchase(ctx context.Context, owner, providerID, id, name, token string, keyIDs []string, plan catalog.Plan, clusterNode bool) (string, string, error) {
	keys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
	if err != nil {
		return "", "", err
	}
	script, err := machineScript(a.PublicURL, token, keys, clusterNode)
	if err != nil {
		return "", "", err
	}
	sec, tok, err := a.openToken(ctx, owner, providerID)
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	machine, err := provisionClient(a).Create(ctx, sec.Provider, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, provision.Spec{
		Name: name, Location: plan.Location, OfferID: plan.OfferID, OfferName: plan.Name,
		UserData: script, RootGB: rootGB(plan), ProjectID: plan.ProjectID, SSHKeys: keys, Key: id,
	})
	if err != nil {
		return "", "", err
	}
	return machine.IPv4, machine.ID, nil
}

func rootGB(plan catalog.Plan) int {
	if plan.StorageID == "boot" && plan.StorageGB > 0 {
		return plan.StorageGB
	}
	if plan.DiskGB > 0 {
		return plan.DiskGB
	}
	return plan.StorageGB
}

func uniqueIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (a *API) getServer(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.Server(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.toServer(row))
}

func (a *API) setServerPlace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Place string `json:"place"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	place, ok := cleanPlace(body.Place)
	if !ok {
		writeError(w, http.StatusBadRequest, "Choisissez une ville.")
		return
	}
	owner := ownerID(r.Context())
	row, err := a.Store.Server(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if row.ProviderID != "" {
		writeError(w, http.StatusConflict, "Le lieu d'un serveur acheté vient de son offre.")
		return
	}
	if err := a.Store.SetServerPlace(r.Context(), owner, row.ID, place); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err = a.Store.Server(r.Context(), owner, row.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.toServer(row))
}

func cleanPlace(raw string) (string, bool) {
	place := strings.TrimSpace(raw)
	if place == "" {
		return "", true
	}
	if utf8.RuneCountInString(place) > 80 {
		return "", false
	}
	for _, r := range place {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' || r == '-' || r == '\'' || r == '.' || r == '’' {
			continue
		}
		return "", false
	}
	return place, true
}

func (a *API) deleteServer(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	row, err := a.Store.Server(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if !confirmedName(w, r, row.Name) {
		return
	}
	names, err := a.Store.ServerNames(r.Context(), owner, row.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	a.respondDestruction(w, r, owner, "server", row.ID, row.Name, serverSteps(row, names))
}

func (a *API) attachCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID string `json:"clusterId"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if err := a.Store.AttachCluster(r.Context(), ownerID(r.Context()), r.PathValue("id"), strings.TrimSpace(body.ClusterID)); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listClusters(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListClusters(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]clusterView, 0, len(rows))
	for _, row := range rows {
		out = append(out, clusterView{ID: row.ID, Name: row.Name, Origin: row.Origin})
	}
	writeJSON(w, http.StatusOK, out)
}

type clusterView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Origin string `json:"origin,omitempty"`
}

func (a *API) createCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au cluster.")
		return
	}
	id := newID()
	owner := ownerID(r.Context())
	if err := a.Store.CreateCluster(r.Context(), owner, id, name); err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), owner, "opened-cluster-"+id, "cluster.opened", "info", "Cluster créé.", "", id)
	writeJSON(w, http.StatusCreated, clusterView{ID: id, Name: name})
}

func (a *API) deleteCluster(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	cluster, err := a.Store.Cluster(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	// A cluster that holds MyInfra asks for the phrase, whichever copy asks:
	// the one in the cluster, or a copy that follows it.
	expected := cluster.Name
	if app, ok := a.selfRecord(r.Context()); ok && app.ClusterID == cluster.ID {
		expected = selfDestroyPhrase
	}
	if !confirmedName(w, r, expected) {
		return
	}
	// From a copy that follows the MyInfra of this cluster, that copy would
	// destroy the servers it runs on and stop half way. This copy takes over
	// instead: it leaves the sync, drives again, and runs the destruction.
	takeOver := false
	selfName := store.Name{}
	// After this copy took the lead back, the copy of the cluster still
	// follows it: its peer and its name go the same way.
	if role, err := a.Store.CopyRole(r.Context()); err == nil && role.MainMethod == "pod" && role.MainClusterID == cluster.ID {
		// That copy writes its name again until its servers are gone: the
		// name is removed once more after them.
		if zone := zoneOfURL(role.MainURL); zone != "" {
			if account, err := a.accountForZone(r.Context(), owner, zone); err == nil {
				selfName = store.Name{Hostname: hostOfURL(role.MainURL), Zone: zone, SecretID: account}
			}
		}
		_ = a.Store.DeletePeer(r.Context(), role.MainURL)
		if zone := zoneOfURL(role.MainURL); zone != "" {
			if sec, creds, err := a.dnsAccountOf(r.Context(), owner, zone); err == nil {
				_ = a.dnsClient().DropAddress(r.Context(), sec.Provider, creds, zone, hostOfURL(role.MainURL))
			}
		}
		if err := a.Store.SetCopyRole(r.Context(), store.CopyRole{Epoch: role.Epoch + 1}); err != nil {
			a.mapErr(w, err)
			return
		}
		takeOver = role.Replica
	}
	rows, err := a.Store.ListServers(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	names, err := a.Store.ClusterNames(r.Context(), owner, cluster.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	members := make([]store.Server, 0)
	for _, row := range rows {
		if row.ClusterID == cluster.ID {
			members = append(members, row)
		}
	}
	if selfName.Hostname != "" {
		names = append(names, selfName)
	}
	if takeOver {
		row, err := a.startDestruction(r.Context(), owner, "cluster", cluster.ID, cluster.Name, clusterSteps(members, names, cluster.ID))
		if err != nil {
			a.mapErr(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, destructionViewOf(row))
		// Started again as the copy that drives, it resumes the destruction.
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
	a.respondDestruction(w, r, owner, "cluster", cluster.ID, cluster.Name, clusterSteps(members, names, cluster.ID))
}

func (a *API) dropName(ctx context.Context, owner string, name store.Name) error {
	sec, tok, err := a.dnsToken(ctx, owner, name.SecretID)
	if err != nil {
		return nil
	}
	return a.dnsClient().DropAddress(ctx, sec.Provider, credsOf(tok), name.Zone, name.Hostname)
}

func (a *API) join(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token   string `json:"token"`
		Address string `json:"address"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	token := machineToken(r)
	if token == "" {
		token = strings.TrimSpace(body.Token)
	}
	if token == "" {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	row, _, err := a.Store.ServerByToken(r.Context(), token)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	// A machine bought by MyInfra is already being deployed: its call changes nothing.
	if row.Status == "deploying" {
		writeJSON(w, http.StatusOK, map[string]string{"status": row.Status})
		return
	}
	if row.Status != "pending" && row.Status != "linked" && row.Status != "failed" {
		writeJSON(w, http.StatusOK, map[string]string{"status": row.Status})
		return
	}
	announced := announcedIP(body.Address)
	if announced == "" {
		announced = publicClientIP(r)
	}
	if announced != "" && announced != row.Address && (row.Address == "" || (!routableIP(row.Address) && routableIP(announced))) {
		if err := a.Store.SetJoinAddress(r.Context(), token, announced); err != nil {
			a.mapErr(w, err)
			return
		}
		row.Address = announced
	}
	if row.Address == "" {
		writeError(w, http.StatusConflict, "MyInfra n'a pas pu lire l'adresse de cette machine.")
		return
	}
	done := "docker"
	if row.ClusterID != "" {
		done = "cluster"
	}
	if row.Status == "linked" && deploy.Done(stepsOf(row.Deploy), done) {
		writeJSON(w, http.StatusOK, map[string]string{"status": row.Status})
		return
	}
	// The pasted command only opened SSH. Docker or Kubernetes continue from here.
	owner, err := a.Store.ServerOwner(r.Context(), row.ID)
	if err == nil && !a.Replica {
		err = a.restartDeploy(context.Background(), owner, row.ID, false)
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deploying"})
}

func (a *API) joinScript(w http.ResponseWriter, r *http.Request) {
	row, keys, err := a.Store.ServerByToken(r.Context(), machineToken(r))
	if err != nil {
		http.Error(w, "Introuvable.\n", http.StatusNotFound)
		return
	}
	script, err := machineScript(a.reachURL(), machineToken(r), keys, row.ClusterID != "")
	if err != nil {
		http.Error(w, "Introuvable.\n", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func (a *API) listApplications(w http.ResponseWriter, r *http.Request) {
	out, _, err := a.applications(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// applications lists the owner's apps with their live health, and returns the
// cluster samples read on the way so a caller can reuse them.
func (a *API) applications(ctx context.Context, owner string) ([]appView, map[string]usage.Sample, error) {
	rows, err := a.Store.ListApplications(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	samples := map[string]usage.Sample{}
	out := make([]appView, 0, len(rows))
	for _, row := range rows {
		if row.ClusterID != "" {
			sample, ok := samples[row.ClusterID]
			if !ok {
				// A cluster that no longer answers keeps an old measure:
				// it is not shown.
				if !a.clusterLost(row.ClusterID) {
					sample, _ = a.clusterSample(ctx, owner, row.ClusterID)
				}
				samples[row.ClusterID] = sample
			}
			if !a.clusterLost(row.ClusterID) {
				row.RunStatus = a.noteAppHealth(ctx, row, sample)
			}
		}
		view := toApp(row)
		a.reachApp(&view)
		out = append(out, view)
	}
	if self, ok := a.selfView(ctx, owner); ok {
		a.reachApp(&self)
		out = append(out, self)
	}
	return out, samples, nil
}

func (a *API) getApplication(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == selfAppID {
		self, ok := a.selfView(r.Context(), ownerID(r.Context()))
		if !ok {
			writeError(w, http.StatusNotFound, "Introuvable.")
			return
		}
		a.reachApp(&self)
		writeJSON(w, http.StatusOK, self)
		return
	}
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	view := toApp(app)
	a.reachApp(&view)
	writeJSON(w, http.StatusOK, view)
}

func (a *API) createApplication(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name            string            `json:"name"`
		Names           []nameBody        `json:"names"`
		SourceKind      string            `json:"sourceKind"`
		Source          string            `json:"source"`
		DestinationKind string            `json:"destinationKind"`
		ServerID        string            `json:"serverId"`
		ClusterID       string            `json:"clusterId"`
		Replicas        int               `json:"replicas"`
		Variables       map[string]string `json:"variables"`
		Services        []serviceBody     `json:"services"`
		Listen          int               `json:"listen"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if a.refuseBusyTarget(w, r, body.ServerID, body.ClusterID) {
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom à l'application.")
		return
	}
	if body.SourceKind != "image" && body.SourceKind != "template" && body.SourceKind != "git" {
		writeError(w, http.StatusBadRequest, "Choisissez une image, un dépôt Git ou un modèle.")
		return
	}
	source := strings.TrimSpace(body.Source)
	if source == "" || len(source) > 300 {
		writeError(w, http.StatusBadRequest, "Indiquez la source.")
		return
	}
	if body.DestinationKind != "server" && body.DestinationKind != "cluster" {
		writeError(w, http.StatusBadRequest, "Choisissez une machine ou un cluster.")
		return
	}
	if body.Replicas == 0 {
		body.Replicas = 1
	}
	if body.Replicas < 1 || body.Replicas > 10 {
		writeError(w, http.StatusBadRequest, "Le nombre de copies va de 1 à 10.")
		return
	}
	if body.DestinationKind == "server" && strings.TrimSpace(body.ServerID) == "" {
		writeError(w, http.StatusBadRequest, "Choisissez une machine.")
		return
	}
	if body.DestinationKind == "cluster" && strings.TrimSpace(body.ClusterID) == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un cluster.")
		return
	}
	if len(body.Names) > 10 {
		writeError(w, http.StatusBadRequest, "Dix noms au plus.")
		return
	}
	if body.Variables == nil {
		body.Variables = map[string]string{}
	}
	gitSource := ""
	if body.SourceKind == "git" {
		gitSource = source
	}
	if body.Listen < 0 || body.Listen > 65535 {
		writeError(w, http.StatusBadRequest, "Le port va de 1 à 65535.")
		return
	}
	implicit := len(body.Services) == 0
	clusterID := ""
	serverID := ""
	if body.DestinationKind == "cluster" {
		clusterID = strings.TrimSpace(body.ClusterID)
	} else {
		serverID = strings.TrimSpace(body.ServerID)
	}
	if gitSource != "" {
		built, imageRef, gitVars, err := a.materializeGitApp(r.Context(), ownerID(r.Context()), serverID, clusterID, name, gitSource, body.Services)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		for key, value := range gitVars {
			body.Variables[key] = value
		}
		body.Services = built
		source = imageRef
		body.SourceKind = "image"
	}
	services, source, err := normalizeServices(name, body.SourceKind, source, body.Replicas, body.Variables, body.Services)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if implicit && len(services) == 1 && body.Listen > 0 {
		services[0].Listen = body.Listen
	}
	if err := a.checkLocalImages(r.Context(), ownerID(r.Context()), serviceImages(services)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.fillFromImages(r.Context(), services, implicit)
	if err := a.shapeRedundancy(r.Context(), ownerID(r.Context()), clusterID, services, body.Replicas); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in := store.NewApplication{
		ID: newID(), OwnerID: ownerID(r.Context()), Name: name,
		SourceKind: body.SourceKind, Source: source, DestinationKind: body.DestinationKind,
		Replicas: body.Replicas, Variables: body.Variables, Services: services,
		DefaultID: newID(), DefaultHost: slug(name) + "." + appsDomain(a.AppsDomain),
	}
	if body.DestinationKind == "server" {
		server, err := a.Store.Server(r.Context(), ownerID(r.Context()), body.ServerID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if server.ClusterID != "" {
			writeError(w, http.StatusBadRequest, "Cette machine appartient à un cluster.")
			return
		}
		in.ServerID = body.ServerID
	} else {
		origin, _, _, _, err := a.Store.ClusterKube(r.Context(), ownerID(r.Context()), body.ClusterID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if origin != "created" {
			writeError(w, http.StatusConflict, "Ce cluster est lu par Argo CD.")
			return
		}
		in.ClusterID = body.ClusterID
	}
	names, err := a.parseNames(r.Context(), in.OwnerID, body.Names)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Names = names
	sealed, err := a.sealVars(in.Variables)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	in.Variables = sealed
	if err := a.Store.CreateApplication(r.Context(), in); err != nil {
		a.mapErr(w, err)
		return
	}
	if len(in.Names) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		a.publishPending(ctx)
		cancel()
	}
	app, err := a.Store.Application(r.Context(), in.OwnerID, in.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if in.DestinationKind == "cluster" && len(services) > 0 {
		go a.deliverClusters(context.Background())
	}
	writeJSON(w, http.StatusCreated, toApp(app))
}

type domainView struct {
	Hostname string `json:"hostname"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Zone     string `json:"zone,omitempty"`
	Record   string `json:"record,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type appView struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	SourceKind      string            `json:"sourceKind"`
	Source          string            `json:"source"`
	DestinationKind string            `json:"destinationKind"`
	ServerID        string            `json:"serverId,omitempty"`
	ServerName      string            `json:"serverName,omitempty"`
	ClusterID       string            `json:"clusterId,omitempty"`
	ClusterName     string            `json:"clusterName,omitempty"`
	Hostname        string            `json:"hostname"`
	RunStatus       string            `json:"runStatus,omitempty"`
	Rollout         string            `json:"rollout,omitempty"`
	RolloutDetail   string            `json:"rolloutDetail,omitempty"`
	RunDetail       string            `json:"runDetail,omitempty"`
	Failover        bool              `json:"failover,omitempty"`
	Replicas        int               `json:"replicas,omitempty"`
	Paused          bool              `json:"paused,omitempty"`
	Logo            string            `json:"logo,omitempty"`
	Variables       map[string]string `json:"variables,omitempty"`
	Domains         []domainView      `json:"domains,omitempty"`
	Services        []serviceView     `json:"services,omitempty"`
	Guarded         bool              `json:"guarded,omitempty"`
}

type serviceBody struct {
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Published bool     `json:"published"`
	Disk      bool     `json:"disk"`
	Replicas  int      `json:"replicas"`
	Listen    int      `json:"listen,omitempty"`
	DiskPath  string   `json:"diskPath,omitempty"`
	DiskGi    int      `json:"diskGi,omitempty"`
	Args      []string `json:"args,omitempty"`
}

type serviceView struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Published bool   `json:"published"`
	Disk      bool   `json:"disk"`
	Replicas  int    `json:"replicas"`
}

func logoImages(app store.Application) []string {
	images := []string{app.Source}
	for _, svc := range app.Services {
		images = append(images, svc.Image)
	}
	return images
}

func (a *API) pauseApplication(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == selfAppID {
		writeError(w, http.StatusConflict, "MyInfra reste en ligne pour piloter le cluster.")
		return
	}
	var body struct {
		Paused bool `json:"paused"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	if err := a.Store.SetPaused(r.Context(), owner, r.PathValue("id"), body.Paused); err != nil {
		a.mapErr(w, err)
		return
	}
	status := "preparing"
	if body.Paused {
		status = "paused"
	}
	_ = a.Store.SetRunStatus(r.Context(), r.PathValue("id"), status)
	go a.deliverClusters(context.Background())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toApp(app))
}

func toApp(app store.Application) appView {
	view := appView{
		ID: app.ID, Name: app.Name, SourceKind: app.SourceKind, Source: app.Source,
		DestinationKind: app.DestinationKind, ServerID: app.ServerID, ServerName: app.ServerName,
		ClusterID: app.ClusterID, ClusterName: app.ClusterName,
		Hostname: app.Hostname, Replicas: app.Replicas, Variables: maskVars(app.Variables),
		RunStatus: app.RunStatus, Rollout: app.Rollout, RolloutDetail: app.RolloutDetail, RunDetail: app.RunDetail, Failover: app.Failover,
		Paused: app.Paused, Logo: market.Logo(logoImages(app)...),
	}
	var defaultHost string
	customs := 0
	for _, d := range app.Domains {
		if d.Kind == "default" {
			defaultHost = d.Hostname
		}
		if d.Kind == "custom" {
			customs++
		}
	}
	for _, d := range app.Domains {
		if d.Kind == "default" && customs > 0 {
			continue
		}
		item := domainView{Hostname: d.Hostname, Kind: d.Kind, Status: d.Status, Zone: d.Zone, Detail: d.Detail}
		if d.Kind == "custom" && d.Zone == "" && d.Status == "pending_dns" && defaultHost != "" {
			item.Record = "CNAME " + d.Hostname + " " + defaultHost
		}
		view.Domains = append(view.Domains, item)
	}
	if view.Domains == nil {
		view.Domains = []domainView{}
	}
	for _, svc := range app.Services {
		view.Services = append(view.Services, serviceView{
			Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk, Replicas: svc.Replicas,
		})
	}
	return view
}

var serviceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

func acceptableImage(image string) bool {
	if strings.Contains(image, "://") || strings.Contains(image, "..") {
		return false
	}
	_, err := registry.Parse(image)
	return err == nil
}

// gitUnsupported answers an application to build from its Git repository:
// MyInfra does not build images yet.
const gitUnsupported = "MyInfra ne construit pas encore d'image depuis un dépôt Git : publiez l'image dans un registre, ou importez-la."

func normalizeServices(appName, sourceKind, source string, replicas int, vars map[string]string, body []serviceBody) ([]store.Service, string, error) {
	if !manifest.EnvOK(vars) {
		return nil, "", fmt.Errorf("Une variable est illisible.")
	}
	if len(body) == 0 {
		if sourceKind != "image" {
			return nil, source, nil
		}
		service := slug(appName)
		if !serviceName.MatchString(service) {
			service = "app"
		}
		body = []serviceBody{{Name: service, Image: source, Published: true, Replicas: replicas}}
	}
	if sourceKind != "image" {
		return nil, "", fmt.Errorf("Une application à plusieurs services part d'images.")
	}
	published := 0
	seen := map[string]bool{}
	out := make([]store.Service, 0, len(body))
	for _, item := range body {
		name := strings.TrimSpace(item.Name)
		image := strings.TrimSpace(item.Image)
		if !serviceName.MatchString(name) || seen[name] {
			return nil, "", fmt.Errorf("Chaque service a un nom en minuscules.")
		}
		if !acceptableImage(image) {
			return nil, "", fmt.Errorf("Indiquez le chemin de l'image, par exemple ghostfolio/ghostfolio:2.168.0.")
		}
		seen[name] = true
		copies := item.Replicas
		if copies == 0 {
			copies = replicas
		}
		if copies < 1 || copies > 10 {
			return nil, "", fmt.Errorf("La redondance va de 1 à 10.")
		}
		if item.Published {
			published++
			source = image
		}
		if item.Listen < 0 || item.Listen > 65535 || item.DiskGi < 0 || item.DiskGi > 1024 {
			return nil, "", fmt.Errorf("Un service est illisible.")
		}
		out = append(out, store.Service{
			ID: newID(), Name: name, Image: image, Published: item.Published, Disk: item.Disk, Replicas: copies,
			Listen: item.Listen, DiskPath: strings.TrimSpace(item.DiskPath), DiskGi: item.DiskGi, Args: item.Args,
		})
	}
	if published != 1 {
		return nil, "", fmt.Errorf("Un seul service reçoit le domaine.")
	}
	for _, svc := range out {
		if !svc.Disk {
			continue
		}
		// Only a database waits for a password at its first start.
		key := databasePassword(svc)
		if key == "" {
			continue
		}
		if vars[key] == "" {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				return nil, "", fmt.Errorf("Le mot de passe du disque n'a pas pu être créé.")
			}
			vars[key] = hex.EncodeToString(buf)
		}
	}
	return out, source, nil
}

func databasePassword(svc store.Service) string {
	image := strings.ToLower(svc.Image)
	switch {
	case svc.Name == "postgres" || strings.Contains(image, "postgres"):
		return "POSTGRES_PASSWORD"
	case strings.Contains(image, "mariadb"):
		return "MARIADB_ROOT_PASSWORD"
	case strings.Contains(image, "mysql"):
		return "MYSQL_ROOT_PASSWORD"
	}
	return ""
}

var imageVolume = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

// fillFromImages reads EXPOSE and VOLUME from the registry for services that
// do not say where they listen or keep files. A registry that does not answer
// leaves the defaults in place. keepVolumes gives a disk to an image that
// declares a folder to keep, when the person did not describe the services.
func (a *API) fillFromImages(ctx context.Context, services []store.Service, keepVolumes bool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for i := range services {
		svc := &services[i]
		if svc.Listen > 0 && (!svc.Disk || svc.DiskPath != "") && !(keepVolumes && !svc.Disk) {
			continue
		}
		img, err := a.inspectImage(ctx, svc.Image)
		if err != nil {
			continue
		}
		if svc.Listen == 0 {
			svc.Listen = img.Port()
		}
		if keepVolumes && !svc.Disk && len(img.Volumes) > 0 {
			svc.Disk = true
		}
		if svc.Disk && svc.DiskPath == "" {
			for _, path := range img.Volumes {
				if imageVolume.MatchString(path) && !strings.Contains(path, "..") {
					svc.DiskPath = path
					break
				}
			}
		}
	}
}

func slug(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	dash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "app"
	}
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

func serviceImages(services []store.Service) []string {
	out := make([]string, 0, len(services))
	for _, svc := range services {
		out = append(out, svc.Image)
	}
	return out
}
