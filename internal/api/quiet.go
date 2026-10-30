package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"myinfra/internal/store"
)

// A quiet update passes without interruption: no application stops while it
// runs. Those are the ones a person can apply together, and the ones applied
// at night when asked. A major version never is: its notes come first.

// quietItem is one update that passes without interruption, and why.
type quietItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// quietResult is what applying one quiet update gave.
type quietResult struct {
	quietItem
	Error string `json:"error,omitempty"`
}

// quietUpdates lists the updates of an owner that pass without interruption.
// Servers of a cluster are updated together, one after the other: the cluster
// is the item. A machine alone stops what it runs, unless it runs nothing.
func (a *API) quietUpdates(ctx context.Context, owner string) ([]quietItem, error) {
	list, err := a.updateList(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := []quietItem{}
	clusters := map[string]bool{}
	var clusterOrder []string
	for _, item := range list {
		if item.Level == "major" {
			continue
		}
		switch item.Kind {
		case "myinfra":
			// Two copies in a cluster are replaced one after the other.
			if view := a.versionView(); view.CanUpdate && view.Method == "pod" && view.Updating == "" {
				out = append(out, quietItem{Kind: "myinfra", ID: "myinfra", Name: "MyInfra", Reason: "copies"})
			}
		case "app":
			app, err := a.Store.Application(ctx, owner, item.ID)
			if err != nil || app.Rollout == "updating" || a.updateRunningOn(ctx, owner, app.ServerID, app.ClusterID) {
				continue
			}
			if impact := imageImpact(app); impact.Effect == effectNone {
				out = append(out, quietItem{Kind: "app", ID: app.ID, Name: app.Name, Reason: impact.Reason})
			}
		case "server":
			server, err := a.Store.Server(ctx, owner, item.ID)
			if err != nil || a.updateRunningOn(ctx, owner, server.ID, server.ClusterID) {
				continue
			}
			if server.ClusterID != "" {
				if !clusters[server.ClusterID] {
					clusters[server.ClusterID] = true
					clusterOrder = append(clusterOrder, server.ClusterID)
				}
				continue
			}
			if impact, err := a.updateImpact(ctx, owner, "", server.ID); err == nil && impactQuiet(impact) {
				out = append(out, quietItem{Kind: "server", ID: server.ID, Name: server.Name, Reason: "elsewhere"})
			}
		case "library":
			var detail struct {
				Edited bool `json:"edited"`
			}
			// A copy the person changed is replaced only on their word.
			if json.Unmarshal(item.Detail, &detail) == nil && !detail.Edited {
				out = append(out, quietItem{Kind: "library", ID: item.ID, Name: item.Name, Reason: "library"})
			}
		}
	}
	for _, clusterID := range clusterOrder {
		impact, err := a.updateImpact(ctx, owner, clusterID, "")
		if err != nil || !impactQuiet(impact) {
			continue
		}
		name := clusterID
		if cluster, ok := a.ownedCluster(ctx, owner, clusterID); ok {
			name = cluster.Name
		}
		out = append(out, quietItem{Kind: "cluster", ID: clusterID, Name: name, Reason: "copies"})
	}
	return out, nil
}

// impactQuiet tells that an update of servers stops no application. A cluster
// with a single driver is left to the person: while it restarts, nothing
// can be deployed.
func impactQuiet(view impactView) bool {
	return view.Effect == effectNone && !slices.Contains(view.Notes, "single-plane")
}

// applyQuiet launches every quiet update of an owner. Library copies and
// applications first, servers next, MyInfra last since it may restart.
func (a *API) applyQuiet(ctx context.Context, owner string) ([]quietResult, error) {
	items, err := a.quietUpdates(ctx, owner)
	if err != nil {
		return nil, err
	}
	order := map[string]int{"library": 0, "app": 1, "server": 2, "cluster": 2, "myinfra": 3}
	slices.SortStableFunc(items, func(x, y quietItem) int { return order[x.Kind] - order[y.Kind] })
	out := make([]quietResult, 0, len(items))
	for _, item := range items {
		var err error
		switch item.Kind {
		case "app":
			err = a.within(ctx, owner, a.appSteady(a.updateApplication), "/api/applications/"+item.ID+"/update", map[string]string{"id": item.ID})
		case "server":
			err = a.within(ctx, owner, a.steady(a.updateServer), "/api/servers/"+item.ID+"/update", map[string]string{"id": item.ID})
		case "cluster":
			err = a.within(ctx, owner, a.steady(a.updateCluster), "/api/clusters/"+item.ID+"/update", map[string]string{"id": item.ID})
		case "library":
			kind, id, _ := strings.Cut(item.ID, ":")
			err = a.within(ctx, owner, a.steady(a.updateLibraryCopy), "/api/updates/library/"+kind+"/"+id, map[string]string{"kind": kind, "id": id})
		case "myinfra":
			err = a.within(ctx, owner, a.steady(a.updateMyInfra), "/api/updates/myinfra", nil)
		}
		result := quietResult{quietItem: item}
		if err != nil {
			result.Error = err.Error()
		}
		out = append(out, result)
	}
	return out, nil
}

// within runs a route of the API for an owner, as the person would, with
// its guards: an update applied with others, or at night, takes the same way
// and says no for the same reasons.
func (a *API) within(ctx context.Context, owner string, handler http.HandlerFunc, path string, values map[string]string) error {
	req, err := http.NewRequestWithContext(withOwner(ctx, owner), http.MethodPost, path, io.NopCloser(bytes.NewReader([]byte("{}"))))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range values {
		req.SetPathValue(key, value)
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	handler(rec, req)
	if rec.status < 400 {
		return nil
	}
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.body.Bytes(), &refusal)
	if refusal.Error == "" {
		refusal.Error = fmt.Sprintf("MyInfra a refusé (%d).", rec.status)
	}
	return errors.New(refusal.Error)
}

func (a *API) listQuietUpdates(w http.ResponseWriter, r *http.Request) {
	items, err := a.quietUpdates(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updates": items})
}

func (a *API) applyQuietUpdates(w http.ResponseWriter, r *http.Request) {
	if a.Replica {
		writeError(w, http.StatusConflict, "Cette copie de MyInfra ne pilote pas les machines.")
		return
	}
	owner := ownerID(r.Context())
	results, err := a.applyQuiet(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if len(results) == 0 {
		writeError(w, http.StatusConflict, "Aucune mise à jour ne passe sans interruption pour le moment.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// autoUpdateView is the nightly setting as the screen shows it.
type autoUpdateView struct {
	Enabled bool       `json:"enabled"`
	Hour    int        `json:"hour"`
	LastRun *time.Time `json:"lastRun,omitempty"`
}

func (a *API) getAutoUpdates(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.AutoUpdateOf(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, autoUpdateView{Enabled: row.Enabled, Hour: row.Hour, LastRun: row.LastRun})
}

func (a *API) setAutoUpdates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
		Hour    *int `json:"hour"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	row, err := a.Store.AutoUpdateOf(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	hour := row.Hour
	if body.Hour != nil {
		hour = *body.Hour
	}
	if hour < 0 || hour > 23 {
		writeError(w, http.StatusBadRequest, "L'heure va de 0 à 23.")
		return
	}
	if err := a.Store.SetAutoUpdate(r.Context(), owner, body.Enabled, hour); err != nil {
		a.mapErr(w, err)
		return
	}
	message := "Correctifs de nuit désactivés."
	if body.Enabled {
		message = fmt.Sprintf("Correctifs de nuit activés, à partir de %02d:00.", hour)
	}
	a.note(r.Context(), owner, newID(), "update.auto", "info", message, "", "")
	writeJSON(w, http.StatusOK, autoUpdateView{Enabled: body.Enabled, Hour: hour, LastRun: row.LastRun})
}

// nightWindow is how long after the chosen hour the nightly patches may
// still start, when MyInfra was off at the hour itself.
const nightWindow = 3 * time.Hour

// nightDue tells whether the nightly patches of a setting should run now:
// past the chosen hour, inside the window, and not yet this night.
func nightDue(row store.AutoUpdate, now time.Time) bool {
	if !row.Enabled {
		return false
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), row.Hour, 0, 0, 0, now.Location())
	if now.Before(start) {
		start = start.AddDate(0, 0, -1)
	}
	if now.Sub(start) >= nightWindow {
		return false
	}
	return row.LastRun == nil || row.LastRun.Before(start)
}

// nightly applies, for each owner who asked, the updates that pass without
// interruption. It runs on the copy that drives the machines only.
func (a *API) nightly(ctx context.Context) {
	if a.Replica || a.transferRunning() {
		return
	}
	rows, err := a.Store.AutoUpdates(ctx)
	if err != nil {
		slog.Warn("correctifs de nuit", "err", err)
		return
	}
	now := time.Now()
	for _, row := range rows {
		if !nightDue(row, now) {
			continue
		}
		if err := a.Store.MarkAutoUpdateRun(ctx, row.OwnerID, now); err != nil {
			slog.Warn("correctifs de nuit", "owner", row.OwnerID, "err", err)
			continue
		}
		night := withSource(ctx, "auto")
		results, err := a.applyQuiet(night, row.OwnerID)
		if err != nil {
			slog.Warn("correctifs de nuit", "owner", row.OwnerID, "err", err)
			continue
		}
		if len(results) == 0 {
			continue
		}
		var done, failed []string
		for _, result := range results {
			if result.Error != "" {
				failed = append(failed, result.Name+" ("+result.Error+")")
			} else {
				done = append(done, result.Name)
			}
		}
		if len(done) > 0 {
			a.note(night, row.OwnerID, newID(), "update.auto", "info", "Correctifs de nuit lancés : "+strings.Join(done, ", ")+".", "", "")
		}
		if len(failed) > 0 {
			a.note(night, row.OwnerID, newID(), "update.auto", "warn", "Correctifs de nuit refusés : "+strings.Join(failed, " ; ")+".", "", "")
		}
	}
}
