package api

import (
	"context"
	"net/http"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
	"myinfra/internal/usage"
)

// What an update does to an application, from the least to the most.
const (
	effectNone  = "none"  // copies keep answering
	effectShort = "short" // about 30 seconds, while one copy starts elsewhere
	effectDown  = "down"  // until the copy starts again in the same place
)

var effectRank = map[string]int{effectNone: 0, effectShort: 1, effectDown: 2}

// impactApp says what an update does to one application, and why: the
// reason is a key the interface translates.
type impactApp struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Effect string `json:"effect"`
	Reason string `json:"reason"`
}

type impactView struct {
	Effect string      `json:"effect"`
	Apps   []impactApp `json:"apps"`
	// Notes are keys for what else the person should know.
	Notes []string `json:"notes"`
}

func impactOf(apps []impactApp, notes ...string) impactView {
	view := impactView{Effect: effectNone, Apps: apps, Notes: notes}
	if view.Apps == nil {
		view.Apps = []impactApp{}
	}
	if view.Notes == nil {
		view.Notes = []string{}
	}
	for _, app := range apps {
		if effectRank[app.Effect] > effectRank[view.Effect] {
			view.Effect = app.Effect
		}
	}
	return view
}

func worse(current impactApp, effect, reason string) impactApp {
	if effectRank[effect] > effectRank[current.Effect] {
		current.Effect, current.Reason = effect, reason
	}
	return current
}

// imageImpact is what new images do to an application: a copy that keeps
// its files on its disk stops before the new one starts on the same disk.
func imageImpact(app store.Application) impactApp {
	out := impactApp{ID: app.ID, Name: app.Name, Effect: effectNone, Reason: "rolling"}
	if app.Paused {
		out.Reason = "paused"
		return out
	}
	for _, svc := range app.Services {
		switch {
		case !svc.Disk || svc.Database == "ready":
		case svc.Backup == "ready":
			out = worse(out, effectShort, "failover")
		case app.ClusterID != "" && svc.Replicas > 1:
			// Copies with their own disks are replaced one after the other.
		default:
			out = worse(out, effectDown, "disk")
		}
	}
	return out
}

func (a *API) applicationUpdateImpact(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	notes := []string{}
	if found, err := a.Store.UpdateOf(r.Context(), "app", app.ID); err == nil && found.Level == "major" {
		notes = append(notes, "major")
	}
	writeJSON(w, http.StatusOK, impactOf([]impactApp{imageImpact(app)}, notes...))
}

// nodeImpact is what emptying and restarting one node of a cluster does to
// the applications of that cluster. Copies elsewhere keep answering; a copy
// whose disk lives on the node waits for it.
func nodeImpact(server store.Server, apps []store.Application, sample usage.Sample, measured bool) []impactApp {
	node := deploy.NodeName(server.Name)
	var out []impactApp
	for _, app := range apps {
		if app.ClusterID != server.ClusterID || app.Paused {
			continue
		}
		item := impactApp{ID: app.ID, Name: app.Name, Effect: effectNone, Reason: "elsewhere"}
		ns := manifest.Namespace(app.ID)
		for _, svc := range app.Services {
			// Without a reading of the cluster, every copy may be on the node.
			here := !measured
			for _, pod := range sample.Pods {
				if pod.Namespace == ns && pod.Service == svc.Name && pod.Node == node {
					here = true
				}
			}
			if !here {
				continue
			}
			switch {
			case !svc.Disk || svc.Database == "ready":
				if svc.Replicas > 1 {
					item = worse(item, effectNone, "copies")
					if item.Reason == "elsewhere" {
						item.Reason = "copies"
					}
				} else {
					item = worse(item, effectShort, "moves")
				}
			case svc.Backup == "ready":
				item = worse(item, effectShort, "failover")
			default:
				item = worse(item, effectDown, "disk")
			}
		}
		out = append(out, item)
	}
	return out
}

// machineImpact is what updating a machine alone does: everything it runs
// stops while it restarts.
func machineImpact(server store.Server, apps []store.Application) []impactApp {
	var out []impactApp
	for _, app := range apps {
		if app.ServerID != server.ID || app.ClusterID != "" || app.Paused {
			continue
		}
		out = append(out, impactApp{ID: app.ID, Name: app.Name, Effect: effectDown, Reason: "machine"})
	}
	return out
}

// detailedApps loads the services of every application of an owner.
func (a *API) detailedApps(ctx context.Context, owner string) ([]store.Application, error) {
	rows, err := a.Store.ListApplications(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]store.Application, 0, len(rows))
	for _, row := range rows {
		app, err := a.Store.Application(ctx, owner, row.ID)
		if err != nil {
			continue
		}
		out = append(out, app)
	}
	return out, nil
}
