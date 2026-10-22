package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"myinfra/internal/blueprint"
	"myinfra/internal/catalog"
	"myinfra/internal/deploy"
	"myinfra/internal/dns"
	"myinfra/internal/market"
	"myinfra/internal/store"
)

func (a *API) Templates(ctx context.Context) {
	a.advanceRuns(context.Background())
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.advanceRuns(context.Background())
		}
	}
}

func (a *API) advanceRuns(ctx context.Context) {
	if a.Replica {
		return
	}
	a.templateMu.Lock()
	defer a.templateMu.Unlock()
	rows, err := a.Store.RunningTemplateRuns(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		a.advance(ctx, row)
	}
}

func (a *API) listTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListTemplates(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]templateView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTemplate(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getTemplate(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.Template(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTemplate(row))
}

func (a *API) createTemplate(w http.ResponseWriter, r *http.Request) {
	name, body, message := readTemplate(w, r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	if err := a.Store.CreateTemplate(r.Context(), ownerID(r.Context()), id, name, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Template(r.Context(), ownerID(r.Context()), id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTemplate(row))
}

func (a *API) updateTemplate(w http.ResponseWriter, r *http.Request) {
	name, body, message := readTemplate(w, r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	owner := ownerID(r.Context())
	if err := a.Store.UpdateTemplate(r.Context(), owner, r.PathValue("id"), name, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Template(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTemplate(row))
}

func (a *API) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteTemplate(r.Context(), ownerID(r.Context()), r.PathValue("id")); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) exportTemplate(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	row, err := a.Store.Template(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	body, err := decodeBody(row.Body)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	a.labelAccounts(r.Context(), owner, &body)
	raw, err := json.MarshalIndent(blueprint.Export(row.Name, body), "", "  ")
	if err != nil {
		a.mapErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName(row.Name)+`.json"`)
	_, _ = w.Write(append(raw, '\n'))
}

func (a *API) importTemplateURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	target, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		writeError(w, http.StatusBadRequest, "Indiquez une URL https:// vers un fichier JSON.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Le fichier est injoignable.")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "Le fichier répond "+res.Status+".")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var doc blueprint.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		writeError(w, http.StatusBadRequest, "Le fichier n'est pas un template JSON valide.")
		return
	}
	owner := ownerID(r.Context())
	secrets, err := a.Store.ListSecrets(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var providers, keys []blueprint.Account
	var zones []string
	for _, sec := range secrets {
		switch sec.Kind {
		case "provider_token":
			providers = append(providers, blueprint.Account{ID: sec.ID, Name: sec.Name, Provider: sec.Provider})
		case "ssh_key":
			keys = append(keys, blueprint.Account{ID: sec.ID, Name: sec.Name})
		case "dns_token":
			names, zoneErr := a.zonesOf(r.Context(), owner, sec.ID)
			if zoneErr != nil {
				continue
			}
			zones = append(zones, names...)
		}
	}
	bodyTpl, message := blueprint.Bind(doc, providers, keys, zones)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	name, ok := cleanName(doc.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au template.")
		return
	}
	if message = blueprint.Validate(bodyTpl); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	encoded, err := json.Marshal(bodyTpl)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	if err := a.Store.CreateTemplate(r.Context(), owner, id, name, encoded); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Template(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTemplate(row))
}

func (a *API) importTemplate(w http.ResponseWriter, r *http.Request) {
	var doc blueprint.Document
	if err := readJSON(w, r, &doc); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	secrets, err := a.Store.ListSecrets(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var providers, keys []blueprint.Account
	var zones []string
	for _, sec := range secrets {
		switch sec.Kind {
		case "provider_token":
			providers = append(providers, blueprint.Account{ID: sec.ID, Name: sec.Name, Provider: sec.Provider})
		case "ssh_key":
			keys = append(keys, blueprint.Account{ID: sec.ID, Name: sec.Name})
		case "dns_token":
			names, zoneErr := a.zonesOf(r.Context(), owner, sec.ID)
			if zoneErr != nil {
				continue
			}
			zones = append(zones, names...)
		}
	}
	body, message := blueprint.Bind(doc, providers, keys, zones)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	name, ok := cleanName(doc.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au template.")
		return
	}
	if message = blueprint.Validate(body); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	if err := a.Store.CreateTemplate(r.Context(), owner, id, name, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Template(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTemplate(row))
}

func (a *API) templateFromCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ClusterID string `json:"clusterId"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	cluster, err := a.Store.Cluster(r.Context(), owner, strings.TrimSpace(input.ClusterID))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	origin, _, _, _, err := a.Store.ClusterKube(r.Context(), owner, cluster.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if origin != "created" {
		writeError(w, http.StatusConflict, "Ce cluster est importé.")
		return
	}
	rows, err := a.Store.ListServers(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var members []store.Server
	for _, row := range rows {
		if row.ClusterID == cluster.ID {
			members = append(members, row)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].CreatedAt.Before(members[j].CreatedAt) })
	body := blueprint.Body{ClusterName: cluster.Name}
	for _, member := range members {
		item := blueprint.Server{Name: member.Name, ProviderID: member.ProviderID, Provider: member.Provider, ProviderName: member.ProviderName, ControlPlane: member.ControlPlane}
		if plan := planOf(member.Plan); plan != nil {
			item.Location = plan.Location
			item.ProjectID = plan.ProjectID
			item.OfferID = plan.OfferID
			item.Period = plan.Period
			item.StorageID = plan.StorageID
			item.StorageGB = plan.StorageGB
		}
		ids, err := a.Store.ServerKeyIDs(r.Context(), member.ID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		item.KeyIDs = ids
		for _, id := range ids {
			sec, err := a.Store.Secret(r.Context(), owner, id)
			if err == nil {
				item.KeyNames = append(item.KeyNames, sec.Name)
			}
		}
		body.Servers = append(body.Servers, item)
	}
	apps, err := a.Store.ListApplications(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	for _, listed := range apps {
		if listed.ClusterID != cluster.ID {
			continue
		}
		full, err := a.Store.Application(r.Context(), owner, listed.ID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		item := blueprint.App{
			Name: full.Name, SourceKind: full.SourceKind, Source: full.Source,
			Replicas: full.Replicas, Variables: shareableVars(full.Variables),
		}
		for _, svc := range full.Services {
			item.Services = append(item.Services, blueprint.Service{
				Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk, Replicas: svc.Replicas,
				Listen: svc.Listen, DiskPath: svc.DiskPath, DiskGi: svc.DiskGi, Args: svc.Args,
			})
		}
		for _, domain := range full.Domains {
			if domain.Kind != "custom" || domain.Zone == "" {
				continue
			}
			sub, ok := subdomain(domain.Hostname, domain.Zone)
			if !ok {
				continue
			}
			item.Names = append(item.Names, blueprint.Name{Subdomain: sub, Zone: domain.Zone})
		}
		body.Applications = append(body.Applications, item)
	}
	if message := blueprint.ValidateShape(body); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	if err := a.Store.CreateTemplate(r.Context(), owner, id, cluster.Name, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Template(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTemplate(row))
}

func (a *API) startTemplateRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TargetClusterID string `json:"targetClusterId"`
		blueprint.Body
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	if _, err := a.Store.Template(r.Context(), owner, r.PathValue("id")); err != nil {
		a.mapErr(w, err)
		return
	}
	body := input.Body
	body.ClusterName = strings.TrimSpace(body.ClusterName)
	if body.ClusterName != "" {
		cleaned, ok := cleanName(body.ClusterName)
		if !ok {
			writeError(w, http.StatusBadRequest, "Donnez un nom au cluster.")
			return
		}
		body.ClusterName = cleaned
	}
	for _, app := range body.Applications {
		for _, name := range app.Names {
			if strings.TrimSpace(name.Subdomain) != "" && strings.TrimSpace(name.Zone) == "" {
				writeCoded(w, http.StatusBadRequest, "domain_of", "Choisissez le domaine de "+app.Name+".", map[string]string{"name": app.Name})
				return
			}
		}
	}
	if message := blueprint.Validate(body); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	// Buying a machine is refused on a replica, as for a server alone: the run
	// would wait there for a copy that drives the machines.
	if len(body.Servers) > 0 && a.Replica {
		writeError(w, http.StatusConflict, replicaRefused)
		return
	}
	if a.alone(r.Context()) {
		writeError(w, http.StatusConflict, replicaAlone)
		return
	}
	for _, server := range body.Servers {
		if !blueprint.ServerReady(server) {
			writeCoded(w, http.StatusBadRequest, "complete_server", "Complétez le provider, l'offre et la clé SSH de "+server.Name+".", map[string]string{"name": server.Name})
			return
		}
	}
	target := strings.TrimSpace(input.TargetClusterID)
	if body.ClusterName != "" {
		target = ""
	} else if target == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un cluster.")
		return
	}
	if target != "" {
		origin, _, _, _, err := a.Store.ClusterKube(r.Context(), owner, target)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if len(body.Servers) > 0 && origin != "created" {
			writeError(w, http.StatusConflict, "Ce cluster est importé.")
			return
		}
		if len(body.Servers) == 0 && !a.clusterHasLeader(r.Context(), owner, target) {
			writeError(w, http.StatusConflict, "Ce cluster n'a pas encore de serveur lié.")
			return
		}
	}
	run := blueprint.Build(body, target)
	if target != "" {
		cluster, err := a.Store.Cluster(r.Context(), owner, target)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if len(run.Steps) > 0 && run.Steps[0].Kind == "cluster" {
			run.Steps[0].Name = cluster.Name
		}
	}
	raw, err := json.Marshal(run)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	clusterID := target
	if err := a.Store.CreateTemplateRun(r.Context(), owner, id, r.PathValue("id"), clusterID, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	go a.kickRun(id, owner)
	row, err := a.Store.TemplateRun(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.runView(r.Context(), row))
}

func (a *API) getTemplateRun(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.TemplateRun(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.runView(r.Context(), row))
}

func (a *API) resumeTemplateRun(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	row, err := a.Store.TemplateRun(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if row.Status != "failed" {
		writeError(w, http.StatusConflict, "Cette exécution n'est pas en échec.")
		return
	}
	var run blueprint.Run
	if err := json.Unmarshal(row.Body, &run); err != nil {
		a.mapErr(w, err)
		return
	}
	for i, step := range run.Steps {
		if step.Status != "failed" {
			continue
		}
		if step.Kind == "server" && step.ResourceID != "" {
			if err := a.relaunchServer(r.Context(), owner, step.ResourceID); err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			run.Steps[i].Status = "running"
			run.Steps[i].Detail = ""
			continue
		}
		run.Steps[i].Status = "pending"
		run.Steps[i].Detail = ""
	}
	raw, err := json.Marshal(run)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.UpdateTemplateRun(r.Context(), owner, row.ID, "running", row.ClusterID, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	go a.kickRun(row.ID, owner)
	row.Status = "running"
	row.Body = raw
	writeJSON(w, http.StatusOK, a.runView(r.Context(), row))
}

func (a *API) kickRun(id, owner string) {
	if a.Replica {
		return
	}
	a.templateMu.Lock()
	defer a.templateMu.Unlock()
	row, err := a.Store.TemplateRun(context.Background(), owner, id)
	if err != nil || row.Status != "running" {
		return
	}
	a.advance(context.Background(), row)
}

func (a *API) advance(ctx context.Context, row store.TemplateRun) {
	var run blueprint.Run
	if err := json.Unmarshal(row.Body, &run); err != nil {
		return
	}
	for {
		fresh, err := a.Store.TemplateRun(ctx, row.OwnerID, row.ID)
		if err != nil || fresh.Status != "running" {
			return
		}
		status := map[string]string{}
		leader := false
		clusterID := runClusterID(run)
		if clusterID != "" {
			servers, err := a.Store.ListServers(ctx, row.OwnerID)
			if err != nil {
				return
			}
			for _, srv := range servers {
				if srv.ClusterID == clusterID && srv.ControlPlane && srv.Status == "linked" {
					leader = true
				}
			}
		}
		for i, step := range run.Steps {
			if step.Kind != "server" || step.ResourceID == "" {
				continue
			}
			srv, err := a.Store.Server(ctx, row.OwnerID, step.ResourceID)
			if err != nil {
				run.Steps[i].Status = "failed"
				run.Steps[i].Detail = "Introuvable."
				status[step.ResourceID] = "failed"
				continue
			}
			status[srv.ID] = srv.Status
			if srv.Status == "linked" {
				run.Steps[i].Status = "done"
				run.Steps[i].Detail = ""
			} else if srv.Status == "failed" {
				run.Steps[i].Status = "failed"
				run.Steps[i].Detail = failedDetail(srv.Deploy)
			} else {
				run.Steps[i].Status = "running"
			}
		}
		for i, step := range run.Steps {
			if step.Kind != "application" || step.ResourceID == "" || step.Status == "done" {
				continue
			}
			app, err := a.Store.Application(ctx, row.OwnerID, step.ResourceID)
			if err != nil {
				run.Steps[i].Status = "failed"
				run.Steps[i].Detail = "Introuvable."
				continue
			}
			status[app.ID] = app.RunStatus
			switch app.RunStatus {
			case "running", "paused":
				run.Steps[i].Status = "done"
				run.Steps[i].Detail = ""
			case "failed":
				run.Steps[i].Status = "failed"
				run.Steps[i].Detail = "L'application ne répond pas."
				if app.RunDetail != "" {
					run.Steps[i].Detail = app.RunDetail
				}
			}
		}
		decision := blueprint.Next(run, leader, status)
		var actionErr error
		switch decision.Kind {
		case "cluster":
			actionErr = a.createRunCluster(ctx, row.OwnerID, &run, decision.Index)
		case "server":
			actionErr = a.createRunServer(ctx, row.OwnerID, &run, decision.Index)
		case "application":
			actionErr = a.createRunApp(ctx, row.OwnerID, &run, decision.Index)
		case "wait":
			_ = a.persistRun(ctx, &row, &run, "running")
			return
		case "done":
			_ = a.persistRun(ctx, &row, &run, "done")
			return
		default:
			if decision.Index >= 0 && decision.Index < len(run.Steps) && run.Steps[decision.Index].Status != "failed" {
				run.Steps[decision.Index].Status = "failed"
				if decision.Detail != "" {
					run.Steps[decision.Index].Detail = decision.Detail
				}
			}
			_ = a.persistRun(ctx, &row, &run, "failed")
			return
		}
		if actionErr != nil {
			run.Steps[decision.Index].Status = "failed"
			run.Steps[decision.Index].Detail = actionErr.Error()
			if decision.Kind == "cluster" {
				_ = a.persistRun(ctx, &row, &run, "failed")
				return
			}
			// A server or an application that cannot start leaves the others:
			// Next ends the run as failed once the rest is done.
			if err := a.persistRun(ctx, &row, &run, "running"); err != nil {
				return
			}
			continue
		}
		if err := a.persistRun(ctx, &row, &run, "running"); err != nil {
			if errors.Is(err, store.ErrStopped) && decision.Kind == "server" && decision.Index >= 0 && decision.Index < len(run.Steps) {
				if id := run.Steps[decision.Index].ResourceID; id != "" {
					_ = a.stopServer(ctx, row.OwnerID, id)
				}
			}
			return
		}
	}
}

func (a *API) createRunCluster(ctx context.Context, owner string, run *blueprint.Run, index int) error {
	name, ok := cleanName(run.ClusterName)
	if !ok {
		return fmt.Errorf("Donnez un nom au cluster.")
	}
	id := newID()
	if err := a.Store.CreateCluster(ctx, owner, id, name); err != nil {
		return err
	}
	run.Steps[index].Status = "done"
	run.Steps[index].ResourceID = id
	run.Steps[index].Name = name
	run.TargetClusterID = id
	return nil
}

func (a *API) createRunServer(ctx context.Context, owner string, run *blueprint.Run, index int) error {
	server, ok := serverAt(*run, index)
	if !ok {
		return fmt.Errorf("Serveur introuvable.")
	}
	name, ok := cleanName(server.Name)
	if !ok {
		return fmt.Errorf("Donnez un nom à la machine.")
	}
	clusterID := runClusterID(*run)
	if clusterID == "" {
		return fmt.Errorf("Choisissez un cluster.")
	}
	cluster, err := a.Store.Cluster(ctx, owner, clusterID)
	if err != nil {
		return err
	}
	cat, err := a.catalogFor(ctx, owner, server.ProviderID, server.Location, server.ProjectID)
	if err != nil {
		return err
	}
	plan, err := cat.Plan(server.OfferID, server.Period, server.StorageID, server.StorageGB)
	if err != nil {
		return err
	}
	provider := server.Provider
	if provider == "" && server.ProviderID != "" {
		if sec, err := a.Store.Secret(ctx, owner, server.ProviderID); err == nil {
			provider = sec.Provider
		}
	}
	plan.ProjectID = strings.TrimSpace(server.ProjectID)
	if needsProject(provider) && plan.ProjectID == "" {
		return fmt.Errorf("Choisissez un projet.")
	}
	rawPlan, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	keyIDs := uniqueIDs(server.KeyIDs)
	steps := deploy.Steps(true)
	rawSteps, err := deploy.Encode(steps)
	if err != nil {
		return err
	}
	id := newID()
	token := randomToken()
	controlPlane := false
	serverOrdinal := 0
	for i, step := range run.Steps {
		if step.Kind != "server" {
			continue
		}
		if i == index {
			controlPlane = blueprint.IsControlPlane(*run, serverOrdinal)
			break
		}
		serverOrdinal++
	}
	if err := a.Store.CreateServer(ctx, owner, id, name, token, server.ProviderID, clusterID, "deploying", rawPlan, rawSteps, keyIDs, controlPlane); err != nil {
		return err
	}
	publicKeys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
	if err != nil {
		_ = a.Store.DeleteServer(context.Background(), owner, id)
		return err
	}
	go a.runDeploy(owner, id, name, token, server.ProviderID, clusterID, cluster.Name, keyIDs, publicKeys, a.privateKey(ctx, owner, keyIDs), plan, "", "", steps)
	run.Steps[index].Status = "running"
	run.Steps[index].ResourceID = id
	run.Steps[index].Name = name
	run.Steps[index].Detail = ""
	return nil
}

func (a *API) createRunApp(ctx context.Context, owner string, run *blueprint.Run, index int) error {
	app, ok := appAt(*run, index)
	if !ok {
		return fmt.Errorf("Application introuvable.")
	}
	name, ok := cleanName(app.Name)
	if !ok {
		return fmt.Errorf("Donnez un nom à l'application.")
	}
	clusterID := runClusterID(*run)
	origin, _, _, _, err := a.Store.ClusterKube(ctx, owner, clusterID)
	if err != nil {
		return err
	}
	if origin != "created" && len(run.Servers) > 0 {
		return fmt.Errorf("Ce cluster est lu par Argo CD.")
	}
	if app.Replicas == 0 {
		app.Replicas = 1
	}
	vars := map[string]string{}
	source := strings.TrimSpace(app.Source)
	if app.SourceKind == "git" {
		services := make([]serviceBody, 0, len(app.Services))
		for _, svc := range app.Services {
			services = append(services, serviceBody{Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk, Replicas: svc.Replicas, Listen: svc.Listen, DiskPath: svc.DiskPath, DiskGi: svc.DiskGi, Args: svc.Args})
		}
		built, imageRef, gitVars, err := a.materializeGitApp(ctx, owner, "", clusterID, name, source, services)
		if err != nil {
			return err
		}
		for key, value := range gitVars {
			vars[key] = value
		}
		app.SourceKind = "image"
		source = imageRef
		app.Services = make([]blueprint.Service, len(built))
		for i, svc := range built {
			app.Services[i] = blueprint.Service{Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk, Replicas: svc.Replicas, Listen: svc.Listen, DiskPath: svc.DiskPath, DiskGi: svc.DiskGi, Args: svc.Args}
		}
	}
	if app.Catalog != "" {
		// An app from the catalogue draws its variables, passwords included,
		// now: the template never holds them.
		publicHost := slug(name) + "." + appsDomain(a.AppsDomain)
		for _, item := range app.Names {
			if host, err := dns.Join(item.Subdomain, item.Zone); err == nil && strings.TrimSpace(item.Subdomain) != "" {
				publicHost = host
				break
			}
		}
		_, env, err := market.Expand(app.Catalog, publicHost, nil)
		if err != nil {
			return err
		}
		for key, value := range env {
			vars[key] = value
		}
	}
	for key, value := range app.Variables {
		vars[key] = value
	}
	services := make([]serviceBody, 0, len(app.Services))
	for _, svc := range app.Services {
		services = append(services, serviceBody{
			Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk, Replicas: svc.Replicas,
			Listen: svc.Listen, DiskPath: svc.DiskPath, DiskGi: svc.DiskGi, Args: svc.Args,
		})
	}
	stored, source, err := normalizeServices(name, app.SourceKind, source, app.Replicas, vars, services)
	if err != nil {
		return err
	}
	if err := a.checkLocalImages(ctx, owner, serviceImages(stored)); err != nil {
		return err
	}
	a.fillFromImages(ctx, stored, len(services) == 0)
	if err := a.shapeRedundancy(ctx, owner, clusterID, stored, app.Replicas); err != nil {
		return err
	}
	in := store.NewApplication{
		ID: newID(), OwnerID: owner, Name: name,
		SourceKind: app.SourceKind, Source: source, DestinationKind: "cluster", ClusterID: clusterID,
		Replicas: app.Replicas, Variables: vars, Services: stored,
		DefaultID: newID(), DefaultHost: slug(name) + "." + appsDomain(a.AppsDomain),
	}
	seen := map[string]bool{}
	accounts := map[string]string{}
	for _, item := range app.Names {
		if strings.TrimSpace(item.Subdomain) == "" {
			continue
		}
		host, err := dns.Join(item.Subdomain, item.Zone)
		if err != nil {
			return err
		}
		if seen[host] {
			return fmt.Errorf("Ce nom est déjà dans la liste.")
		}
		seen[host] = true
		zone := strings.ToLower(strings.Trim(strings.TrimSpace(item.Zone), "."))
		secretID, ok := accounts[zone]
		if !ok {
			found, lookupErr := a.accountForZone(ctx, owner, zone)
			if lookupErr != nil {
				return lookupErr
			}
			secretID = found
			accounts[zone] = secretID
		}
		in.Names = append(in.Names, store.Name{ID: newID(), Hostname: host, Zone: zone, SecretID: secretID})
	}
	sealed, err := a.sealVars(in.Variables)
	if err != nil {
		return err
	}
	in.Variables = sealed
	if err := a.Store.CreateApplication(ctx, in); err != nil {
		return err
	}
	if len(in.Names) > 0 {
		publishCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		a.publishPending(publishCtx)
		cancel()
	}
	if len(stored) > 0 {
		go a.deliverClusters(context.Background())
	}
	// The step ends when the application answers, not when it is recorded.
	run.Steps[index].Status = "running"
	run.Steps[index].ResourceID = in.ID
	run.Steps[index].Name = name
	run.Steps[index].Detail = "Démarrage de l'application."
	return nil
}

func (a *API) relaunchServer(ctx context.Context, owner, id string) error {
	return a.restartDeploy(ctx, owner, id, true)
}

// restartDeploy starts the deployment of a server again. replay runs every
// step after the purchase again; otherwise the finished steps are kept.
func (a *API) restartDeploy(ctx context.Context, owner, id string, replay bool) error {
	row, err := a.Store.Server(ctx, owner, id)
	if err != nil {
		return err
	}
	if row.Address == "" && row.ProviderID == "" {
		return fmt.Errorf("Cette machine n'a pas encore d'adresse.")
	}
	steps := stepsOf(row.Deploy)
	if len(steps) == 0 {
		steps = stepsFor(row.ProviderID != "", row.ClusterID)
	}
	if row.Address != "" {
		if !deploy.Done(steps, "machine") {
			steps = deploy.Mark(steps, "machine", "done", row.Address)
		}
		if replay {
			steps = deploy.Replay(steps)
		} else {
			steps = deploy.Resume(steps)
		}
	} else {
		steps = deploy.Resume(steps)
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, id)
	if err != nil {
		return err
	}
	publicKeys, err := a.Store.SSHPublicKeys(ctx, owner, keyIDs)
	if err != nil {
		return err
	}
	raw, err := deploy.Encode(steps)
	if err != nil {
		return err
	}
	if err := a.Store.ClaimDeploy(ctx, owner, id, raw); err != nil {
		return err
	}
	plan := catalog.Plan{}
	if decoded := planOf(row.Plan); decoded != nil {
		plan = *decoded
	}
	go a.runDeploy(owner, id, row.Name, row.JoinToken, row.ProviderID, row.ClusterID, row.ClusterName, keyIDs, publicKeys, a.privateKey(ctx, owner, keyIDs), plan, row.Address, row.ProviderServerID, steps)
	return nil
}

func (a *API) persistRun(ctx context.Context, row *store.TemplateRun, run *blueprint.Run, status string) error {
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	clusterID := runClusterID(*run)
	if err := a.Store.PersistRunningTemplateRun(ctx, row.OwnerID, row.ID, status, clusterID, raw); err != nil {
		if !errors.Is(err, store.ErrStopped) {
			slog.Error("suivi", "id", row.ID, "err", err)
		}
		return err
	}
	row.Body = raw
	row.Status = status
	row.ClusterID = clusterID
	return nil
}

func (a *API) clusterHasLeader(ctx context.Context, owner, clusterID string) bool {
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.ClusterID == clusterID && row.Status == "linked" {
			return true
		}
	}
	return false
}

func (a *API) labelAccounts(ctx context.Context, owner string, body *blueprint.Body) {
	for i, server := range body.Servers {
		if server.ProviderID != "" {
			if sec, err := a.Store.Secret(ctx, owner, server.ProviderID); err == nil {
				body.Servers[i].Provider = sec.Provider
				body.Servers[i].ProviderName = sec.Name
			}
		}
		var names []string
		for _, id := range server.KeyIDs {
			sec, err := a.Store.Secret(ctx, owner, id)
			if err != nil {
				continue
			}
			names = append(names, sec.Name)
		}
		body.Servers[i].KeyNames = names
	}
}

func (a *API) runView(ctx context.Context, row store.TemplateRun) runView {
	var run blueprint.Run
	_ = json.Unmarshal(row.Body, &run)
	view := runView{
		ID: row.ID, TemplateID: row.TemplateID, Status: row.Status, ClusterID: row.ClusterID,
		CreatedAt: row.CreatedAt, Steps: make([]runStepView, 0, len(run.Steps)),
	}
	for _, step := range run.Steps {
		item := runStepView{Kind: step.Kind, Name: step.Name, Status: step.Status, Detail: step.Detail, ResourceID: step.ResourceID}
		if step.Kind == "server" && step.ResourceID != "" {
			if srv, err := a.Store.Server(ctx, row.OwnerID, step.ResourceID); err == nil {
				item.Deploy = stepsOf(srv.Deploy)
				if item.Name == "" {
					item.Name = srv.Name
				}
			}
		}
		view.Steps = append(view.Steps, item)
	}
	return view
}

type templateView struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Body      blueprint.Body `json:"body"`
	CreatedAt time.Time      `json:"createdAt"`
}

type runView struct {
	ID         string        `json:"id"`
	TemplateID string        `json:"templateId"`
	Status     string        `json:"status"`
	ClusterID  string        `json:"clusterId,omitempty"`
	Steps      []runStepView `json:"steps"`
	CreatedAt  time.Time     `json:"createdAt"`
}

type runStepView struct {
	Kind       string        `json:"kind"`
	Name       string        `json:"name"`
	Status     string        `json:"status"`
	Detail     string        `json:"detail,omitempty"`
	ResourceID string        `json:"resourceId,omitempty"`
	Deploy     []deploy.Step `json:"deploy,omitempty"`
}

func toTemplate(row store.Template) templateView {
	body, _ := decodeBody(row.Body)
	return templateView{ID: row.ID, Name: row.Name, Body: body, CreatedAt: row.CreatedAt}
}

func readTemplate(w http.ResponseWriter, r *http.Request) (string, blueprint.Body, string) {
	var input struct {
		Name string `json:"name"`
		blueprint.Body
	}
	if err := readJSON(w, r, &input); err != nil {
		return "", blueprint.Body{}, "Requête illisible."
	}
	name, ok := cleanName(input.Name)
	if !ok {
		return "", blueprint.Body{}, "Donnez un nom au template."
	}
	body := input.Body
	body.ClusterName = strings.TrimSpace(body.ClusterName)
	if body.ClusterName != "" {
		cleaned, ok := cleanName(body.ClusterName)
		if !ok {
			return "", blueprint.Body{}, "Donnez un nom au cluster."
		}
		body.ClusterName = cleaned
	}
	for i, server := range body.Servers {
		cleaned, ok := cleanName(server.Name)
		if !ok {
			return "", blueprint.Body{}, "Donnez un nom à chaque serveur."
		}
		body.Servers[i].Name = cleaned
	}
	for i, app := range body.Applications {
		cleaned, ok := cleanName(app.Name)
		if !ok {
			return "", blueprint.Body{}, "Donnez un nom à chaque application."
		}
		body.Applications[i].Name = cleaned
		if app.Replicas == 0 {
			body.Applications[i].Replicas = 1
		}
	}
	if message := blueprint.Validate(body); message != "" {
		return "", blueprint.Body{}, message
	}
	return name, body, ""
}

func decodeBody(raw []byte) (blueprint.Body, error) {
	var body blueprint.Body
	if len(raw) == 0 {
		return body, nil
	}
	err := json.Unmarshal(raw, &body)
	return body, err
}

func runClusterID(run blueprint.Run) string {
	for _, step := range run.Steps {
		if step.Kind == "cluster" && step.ResourceID != "" {
			return step.ResourceID
		}
	}
	return run.TargetClusterID
}

func serverAt(run blueprint.Run, index int) (blueprint.Server, bool) {
	n := 0
	for i, step := range run.Steps {
		if step.Kind != "server" {
			continue
		}
		if i == index {
			if n < len(run.Servers) {
				return run.Servers[n], true
			}
			return blueprint.Server{}, false
		}
		n++
	}
	return blueprint.Server{}, false
}

func appAt(run blueprint.Run, index int) (blueprint.App, bool) {
	n := 0
	for i, step := range run.Steps {
		if step.Kind != "application" {
			continue
		}
		if i == index {
			if n < len(run.Applications) {
				return run.Applications[n], true
			}
			return blueprint.App{}, false
		}
		n++
	}
	return blueprint.App{}, false
}

func failedDetail(raw []byte) string {
	for _, step := range stepsOf(raw) {
		if step.Status == "failed" && step.Detail != "" {
			return step.Detail
		}
	}
	return "Le déploiement a échoué."
}

func subdomain(host, zone string) (string, bool) {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	zone = strings.ToLower(strings.Trim(strings.TrimSpace(zone), "."))
	if host == "" || zone == "" || host == zone || !strings.HasSuffix(host, "."+zone) {
		return "", false
	}
	return strings.TrimSuffix(host, "."+zone), true
}

func fileName(name string) string {
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
		return "template"
	}
	return out
}
