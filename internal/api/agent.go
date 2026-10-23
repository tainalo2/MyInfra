package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"myinfra/internal/agent"
	"myinfra/internal/store"
)

// workloads are the applications a machine alone must run, variables opened.
func (a *API) workloads(ctx context.Context, token string) ([]agent.Workload, error) {
	apps, err := a.Store.DesiredApps(ctx, token)
	if err != nil {
		return nil, err
	}
	work := make([]agent.Workload, 0, len(apps))
	addresses := map[string]string{}
	for _, app := range apps {
		address, seen := addresses[app.ID]
		if !seen {
			if address, err = a.Store.Address(ctx, app.ID); err != nil {
				return nil, err
			}
			addresses[app.ID] = address
		}
		shared, err := a.sharedEnv(ctx, app.ID)
		if err != nil {
			return nil, err
		}
		env, err := a.openVars(app.Env)
		if err != nil {
			return nil, err
		}
		merged := fillMap(mergeSecretEnv(env, []map[string]string{shared}), address)
		work = append(work, agent.Workload{
			ID: app.ID, Service: app.Service, Image: app.Image, Hosts: app.Hosts, Zones: app.Zones, Env: merged,
			Disk: app.Disk, DiskPath: app.DiskPath, DiskGi: app.DiskGi, Listen: app.Listen, Args: app.Args,
			Published: app.Published, Replicas: app.Replicas, Paused: app.Paused, Spec: specAt(app.Spec, address),
			Login: a.agentLogin(ctx, app.Image),
		})
	}
	return work, nil
}

func (a *API) agentSync(w http.ResponseWriter, r *http.Request) {
	token := machineToken(r)
	server, _, err := a.Store.ServerByToken(r.Context(), token)
	if err != nil || !machineCaller(server.Address, clientIP(r)) {
		http.Error(w, "Introuvable.\n", http.StatusNotFound)
		return
	}
	// No plaintext variables until this machine has called home from its address.
	var work []agent.Workload
	if strings.TrimSpace(server.Address) != "" {
		work, err = a.workloads(r.Context(), token)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Une erreur est survenue.\n", http.StatusInternalServerError)
			return
		}
	}
	script, err := agent.Sync(a.reachURL(), token, work)
	if err != nil {
		http.Error(w, "Introuvable.\n", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func (a *API) agentReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	// A machine reachable over SSH is declared online by the delivery, once
	// its address answers in HTTPS. The agent only reports a failure then.
	token := machineToken(r)
	server, _, err := a.Store.ServerByToken(r.Context(), token)
	if err != nil || !machineCaller(server.Address, clientIP(r)) {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	if body.Status == "running" {
		if server.Address != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	if err := a.Store.ReportRun(r.Context(), token, body.ID, body.Status); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
