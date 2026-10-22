package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"myinfra/internal/blueprint"
	"myinfra/internal/store"
)

type deploymentView struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Step   string `json:"step,omitempty"`
	To     string `json:"to"`
}

func (a *API) listDeployments(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.OpenDeployments(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	ordered := make([]deploymentView, 0, len(rows)+1)
	a.transfer.mu.Lock()
	running := a.transfer.running
	step := a.transfer.step
	a.transfer.mu.Unlock()
	if running {
		ordered = append(ordered, deploymentView{
			ID: "transfer", Kind: "transfer", Name: "MyInfra", Status: "deploying", Step: step, To: "/transfert/suivi",
		})
	}
	for _, row := range rows {
		ordered = append(ordered, deploymentOf(row))
	}
	writeJSON(w, http.StatusOK, ordered)
}

func (a *API) cancelDeployment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	var err error
	switch body.Kind {
	case "template":
		err = a.stopTemplate(r.Context(), owner, body.ID)
	case "server":
		err = a.stopServer(r.Context(), owner, body.ID)
	case "application":
		err = a.stopApplication(r.Context(), owner, body.ID)
	case "transfer":
		err = a.stopTransfer()
	default:
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if errors.Is(err, store.ErrStopped) {
		writeError(w, http.StatusConflict, "Ce déploiement n'est plus en cours.")
		return
	}
	if err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) stopTemplate(ctx context.Context, owner, id string) error {
	row, err := a.Store.TemplateRun(ctx, owner, id)
	if err != nil {
		return err
	}
	if row.Status != "running" {
		return store.ErrStopped
	}
	var run blueprint.Run
	if err := json.Unmarshal(row.Body, &run); err != nil {
		return err
	}
	for i, step := range run.Steps {
		if step.Status == "done" || step.Status == "failed" || step.Status == "cancelled" {
			continue
		}
		run.Steps[i].Status = "cancelled"
		run.Steps[i].Detail = ""
		if step.Kind == "server" && step.ResourceID != "" {
			_ = a.stopServer(ctx, owner, step.ResourceID)
		}
		if step.Kind == "application" && step.ResourceID != "" {
			_ = a.stopApplication(ctx, owner, step.ResourceID)
		}
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return a.Store.CancelTemplateRun(ctx, owner, id, raw)
}

func (a *API) stopApplication(ctx context.Context, owner, id string) error {
	app, err := a.Store.Application(ctx, owner, id)
	if err != nil {
		return err
	}
	if app.Rollout == "updating" {
		go a.restoreRollout(id)
		return nil
	}
	if app.Paused || app.RunStatus != "preparing" {
		return store.ErrStopped
	}
	if err := a.Store.SetPaused(ctx, owner, id, true); err != nil {
		return err
	}
	return a.Store.SetRunStatus(ctx, id, "paused")
}

func deploymentOf(row store.OpenDeployment) deploymentView {
	view := deploymentView{ID: row.ID, Kind: row.Kind, Name: row.Name, Status: row.Status, Step: row.Detail}
	switch row.Kind {
	case "server":
		view.To = "/serveurs/" + row.ID
		view.Step = currentStep(row.Raw)
	case "application":
		view.To = "/applications/" + row.ID
	case "template":
		view.To = "/templates/suivis/" + row.ID
		view.Status = "deploying"
		view.Name, view.Step = templateCourse(row.Raw)
	}
	return view
}

func currentStep(raw []byte) string {
	var steps []struct {
		Label  string `json:"label"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &steps) != nil {
		return ""
	}
	for _, want := range []string{"running", "pending"} {
		for _, step := range steps {
			if step.Status != want {
				continue
			}
			if step.Label != "" {
				return step.Label
			}
			return step.Name
		}
	}
	return ""
}

func templateCourse(raw []byte) (string, string) {
	var run struct {
		ClusterName string `json:"clusterName"`
		Steps       []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &run) != nil {
		return "", ""
	}
	name := run.ClusterName
	step := ""
	for _, want := range []string{"running", "pending"} {
		if step != "" {
			break
		}
		for _, item := range run.Steps {
			if item.Status == want {
				step = item.Name
				break
			}
		}
	}
	if name == "" {
		name = step
	}
	return name, step
}
