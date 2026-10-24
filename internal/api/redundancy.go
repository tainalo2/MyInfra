package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/pack"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

// redundancy sorts the services of an application by how each can survive the
// loss of a server.
type redundancy struct {
	// Scaled run as many copies as asked.
	Scaled []string `json:"scaled"`
	// Failover keep one copy whose SQLite files are replicated: it starts
	// again on another server if its own fails.
	Failover []string `json:"failover"`
	// Database move their data to a replicated PostgreSQL and their files to
	// the store of the cluster, then run as many copies as asked.
	Database []string `json:"database"`
	// Alone keep files MyInfra knows nothing about, on their node disk.
	Alone []string `json:"alone"`
}

// profileOf is what MyInfra knows of a service's data. One that mounts a
// disk shared with other services stays where that disk is, whatever its
// image: no copies, no replicated SQLite, no move to PostgreSQL.
func profileOf(svc store.Service) market.Profile {
	if spec.Shares(svc.Spec) {
		return market.Profile{Kind: market.Unknown}
	}
	return market.ProfileOf(svc.Image, svc.Disk)
}

func redundancyPlan(services []store.Service) redundancy {
	plan := redundancy{Scaled: []string{}, Failover: []string{}, Database: []string{}, Alone: []string{}}
	for _, svc := range services {
		switch profileOf(svc).Kind {
		case market.Stateless:
			plan.Scaled = append(plan.Scaled, svc.Name)
		case market.SingleWriter:
			plan.Failover = append(plan.Failover, svc.Name)
		case market.Database:
			plan.Database = append(plan.Database, svc.Name)
		default:
			plan.Alone = append(plan.Alone, svc.Name)
		}
	}
	return plan
}

// redundancyOptions tells the dialog what each service would become.
func (a *API) redundancyOptions(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	nodes := 0
	if app.ClusterID != "" {
		if nodes, err = a.linkedNodes(r.Context(), owner, app.ClusterID); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	backedUp := []string{}
	for _, svc := range app.Services {
		if svc.Backup != "" {
			backedUp = append(backedUp, svc.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan": redundancyPlan(app.Services), "nodes": nodes, "storageNodes": storageNodes, "backedUp": backedUp,
	})
}

func (a *API) setRedundancy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Copies int `json:"copies"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	if r.PathValue("id") == selfAppID {
		writeError(w, http.StatusConflict, "Les copies de MyInfra se règlent depuis Transfert.")
		return
	}
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.ClusterID == "" {
		writeError(w, http.StatusConflict, "Une machine seule n'a qu'un serveur : ajoutez-la à un cluster pour répartir l'application.")
		return
	}
	if app.Paused {
		writeError(w, http.StatusConflict, "Reprenez l'application avant d'ajouter des copies.")
		return
	}
	nodes, err := a.linkedNodes(r.Context(), owner, app.ClusterID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if nodes < 2 {
		writeError(w, http.StatusConflict, "Ce cluster n'a qu'un serveur lié : il n'y a nulle part où répartir les copies.")
		return
	}
	plan := redundancyPlan(app.Services)
	if len(plan.Scaled)+len(plan.Failover)+len(plan.Database) == 0 {
		writeError(w, http.StatusConflict, "Chaque service de cette application garde sur son disque des fichiers que MyInfra ne sait pas répliquer : une copie aurait un disque vide, pas les données.")
		return
	}
	if len(plan.Scaled)+len(plan.Database) > 0 && (body.Copies < 2 || body.Copies > nodes) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Choisissez entre 2 et %d copies, une par serveur du cluster.", nodes))
		return
	}
	if len(plan.Failover)+len(plan.Database) > 0 && nodes < storageNodes {
		writeError(w, http.StatusConflict, errStorageNodes.Error())
		return
	}
	if body.Copies >= 2 && body.Copies <= nodes {
		if err := a.Store.SetApplicationReplicas(r.Context(), app.ID, body.Copies); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	scaled := map[string]int{}
	for _, name := range plan.Scaled {
		scaled[name] = body.Copies
	}
	before := map[string]int{}
	for _, svc := range app.Services {
		before[svc.Name] = svc.Replicas
	}
	if len(scaled) > 0 {
		if err := a.Store.SetServiceReplicas(r.Context(), app.ID, scaled); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	for _, svc := range app.Services {
		for _, name := range plan.Failover {
			if svc.Name == name && svc.Backup == "" {
				if err := a.Store.SetServiceBackup(r.Context(), app.ID, name, "migrating"); err != nil {
					a.mapErr(w, err)
					return
				}
			}
		}
	}
	if len(plan.Database) > 0 {
		if err := a.startDatabase(r.Context(), owner, app, body.Copies); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	a.note(r.Context(), owner, newID(), "app.redundancy", "info", redundancyNote(app.Name, plan, body.Copies), "", app.ClusterID)
	go a.spreadCopies(owner, app.ID, app.ClusterID, scaled, before)
	fresh, err := a.Store.Application(r.Context(), owner, app.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"application": toApp(fresh), "plan": plan})
}

func redundancyNote(name string, plan redundancy, copies int) string {
	var parts []string
	if len(plan.Scaled)+len(plan.Database) > 0 {
		parts = append(parts, fmt.Sprintf("%d copies demandées", copies))
	}
	if len(plan.Database) > 0 {
		parts = append(parts, "base PostgreSQL répliquée")
	}
	if len(plan.Failover) > 0 {
		parts = append(parts, "sauvegarde continue et bascule automatique")
	}
	return name + " : " + strings.Join(parts, ", ") + "."
}

func (a *API) linkedNodes(ctx context.Context, owner, clusterID string) (int, error) {
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return 0, err
	}
	nodes := 0
	for _, srv := range servers {
		if srv.ClusterID == clusterID && srv.Status == "linked" {
			nodes++
		}
	}
	return nodes, nil
}

// spreadCopies delivers the new copies, then moves the copies that already
// shared one server: Kubernetes never moves a running pod by itself, so copies
// started when the cluster had a single node stay together.
func (a *API) spreadCopies(owner, appID, clusterID string, scaled, before map[string]int) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	a.forgetApplied("app:" + clusterID + ":" + appID)
	a.deliverClusters(ctx)
	host, err := a.clusterHost(ctx, owner, clusterID)
	if err != nil {
		return
	}
	namespace := manifest.Namespace(appID)
	for name := range scaled {
		if before[name] < 2 {
			// From one copy, the pod template gains the spread rule: every
			// copy is created again and placed apart.
			continue
		}
		out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" get pods -l myinfra.io/service="+name+" -o jsonpath='{range .items[*]}{.spec.nodeName}{\"\\n\"}{end}'")
		if err != nil {
			continue
		}
		if distinctLines(out) > 1 {
			continue
		}
		if _, err := deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" rollout restart deploy/"+name); err != nil {
			slog.Info("redondance", "app", appID, "service", name, "err", err)
		}
	}
}

func distinctLines(out string) int {
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			seen[line] = true
		}
	}
	return len(seen)
}

// shapeRedundancy gives each service of a new application what the asked
// level means for it. Nothing is there to move yet: a single writer starts
// backed up, a database service starts in its replicated PostgreSQL. A
// service with a disk MyInfra does not know keeps one copy: copies of a disk
// would each have their own data.
func (a *API) shapeRedundancy(ctx context.Context, owner, clusterID string, services []store.Service, copies int) error {
	if copies <= 1 {
		return shapeServices(services, copies, 0)
	}
	if clusterID == "" {
		return errors.New("Une machine seule n'a qu'un serveur : choisissez un cluster pour répartir l'application.")
	}
	nodes, err := a.linkedNodes(ctx, owner, clusterID)
	if err != nil {
		return err
	}
	return shapeServices(services, copies, nodes)
}

func shapeServices(services []store.Service, copies, nodes int) error {
	if copies <= 1 {
		for i := range services {
			if services[i].Disk {
				services[i].Replicas = 1
			}
		}
		return nil
	}
	if copies > nodes {
		if nodes <= 1 {
			return errors.New("Ce cluster n'a qu'un serveur lié : il n'y a pas d'autre serveur où mettre une copie.")
		}
		return fmt.Errorf("Ce cluster a %d serveurs liés : choisissez au plus %d copies.", nodes, nodes)
	}
	plan := redundancyPlan(services)
	if len(plan.Failover)+len(plan.Database) > 0 && nodes < storageNodes {
		return errStorageNodes
	}
	for i := range services {
		switch profileOf(services[i]).Kind {
		case market.Stateless:
			services[i].Replicas = copies
		case market.SingleWriter:
			services[i].Replicas = 1
			services[i].Backup = "ready"
		case market.Database:
			services[i].Replicas = copies
			services[i].Database = "ready"
		default:
			services[i].Replicas = 1
		}
	}
	return nil
}

// redundancyPreview tells the creation form what each service would become,
// before the application exists.
func (a *API) redundancyPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID string `json:"clusterId"`
		Image     string `json:"image"`
		Article   string `json:"article"`
		// Package is a saved package, by its id.
		Package string `json:"package"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	var services []store.Service
	if body.Package != "" {
		row, err := a.Store.Package(r.Context(), ownerID(r.Context()), body.Package)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		var p pack.Package
		if err := json.Unmarshal(row.Body, &p); err != nil {
			a.mapErr(w, err)
			return
		}
		for _, svc := range p.Services {
			services = append(services, store.Service{Name: svc.Name, Image: svc.Image, Disk: len(svc.Volumes) > 0})
		}
	} else if body.Article != "" {
		p, ok := market.PackageOf(body.Article)
		if !ok {
			writeError(w, http.StatusNotFound, "Cet article est introuvable.")
			return
		}
		for _, svc := range p.Services {
			services = append(services, store.Service{Name: svc.Name, Image: svc.Image, Disk: len(svc.Volumes) > 0})
		}
	} else if image := strings.TrimSpace(body.Image); image != "" {
		// Whether the image declares a disk is read at creation; a known
		// image tells it already.
		profile := market.ProfileOf(image, true)
		services = append(services, store.Service{Name: "app", Image: image, Disk: profile.Kind != market.Unknown})
	}
	nodes := 0
	if body.ClusterID != "" {
		var err error
		if nodes, err = a.linkedNodes(r.Context(), ownerID(r.Context()), body.ClusterID); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": redundancyPlan(services), "nodes": nodes, "storageNodes": storageNodes})
}
