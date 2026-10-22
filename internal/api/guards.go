package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"time"
)

// steady refuses an action while MyInfra moves to another place: what it
// would change after the data is copied would be lost with this copy.
func (a *API) steady(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.transferRunning() {
			writeCoded(w, http.StatusConflict, "transfer_running", "MyInfra est en cours de transfert : attendez la fin pour modifier quoi que ce soit.", nil)
			return
		}
		next(w, r)
	}
}

// appSteady refuses to change an application while it, or the servers it
// runs on, are being updated. Removing it stays possible: it is a separate
// route.
func (a *API) appSteady(next http.HandlerFunc) http.HandlerFunc {
	return a.steady(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerID(r.Context())
		if app, err := a.Store.Application(r.Context(), owner, r.PathValue("id")); err == nil {
			if app.Rollout == "updating" {
				writeCoded(w, http.StatusConflict, "app_updating", "Cette application est en cours de mise à jour : attendez la fin pour la modifier.", nil)
				return
			}
			if a.updateRunningOn(r.Context(), owner, app.ServerID, app.ClusterID) {
				writeCoded(w, http.StatusConflict, "servers_updating", "Les serveurs de cette application sont en cours de mise à jour : attendez la fin pour la modifier.", nil)
				return
			}
		}
		next(w, r)
	})
}

// serverSteady refuses to change a server while it, or its cluster, is being
// updated. Removing it stays possible.
func (a *API) serverSteady(next http.HandlerFunc) http.HandlerFunc {
	return a.steady(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerID(r.Context())
		if server, err := a.Store.Server(r.Context(), owner, r.PathValue("id")); err == nil && a.updateRunningOn(r.Context(), owner, server.ID, server.ClusterID) {
			writeCoded(w, http.StatusConflict, "servers_updating", "Ce serveur est en cours de mise à jour : attendez la fin pour le modifier.", nil)
			return
		}
		next(w, r)
	})
}

// updateRunningOn tells whether an update job runs on a server or a cluster.
func (a *API) updateRunningOn(ctx context.Context, owner, serverID, clusterID string) bool {
	jobs, err := a.Store.RunningUpdateJobs(ctx)
	if err != nil {
		return false
	}
	for _, job := range jobs {
		if job.OwnerID != owner {
			continue
		}
		if clusterID != "" && job.ClusterID == clusterID {
			return true
		}
		if serverID == "" {
			continue
		}
		if job.ServerID == serverID {
			return true
		}
		var steps []updateStep
		_ = json.Unmarshal(job.Steps, &steps)
		for _, step := range steps {
			if step.ServerID == serverID {
				return true
			}
		}
	}
	return false
}

// refuseBusyTarget answers for a creation on a server or a cluster being
// updated, and returns true when it did.
func (a *API) refuseBusyTarget(w http.ResponseWriter, r *http.Request, serverID, clusterID string) bool {
	if a.updateRunningOn(r.Context(), ownerID(r.Context()), serverID, clusterID) {
		writeCoded(w, http.StatusConflict, "servers_updating", "Ce serveur ou ce cluster est en cours de mise à jour : attendez la fin.", nil)
		return true
	}
	return false
}

// activityView lists what this copy is doing on machines right now. On a
// copy running on the person's computer, the screen asks not to switch it
// off meanwhile.
type activityView struct {
	Local      bool                `json:"local"`
	Operations []activityOperation `json:"operations"`
	// Replica is true on a copy that follows another; Main is that copy,
	// with the last exchange with it, for the screen to offer the lead.
	Replica bool           `json:"replica"`
	Main    *otherCopyView `json:"main,omitempty"`
}

type activityOperation struct {
	Kind  string     `json:"kind"`
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	To    string     `json:"to,omitempty"`
	Since *time.Time `json:"since,omitempty"`
	// ClusterID and Servers are what an update holds: their changes wait.
	ClusterID string   `json:"clusterId,omitempty"`
	Servers   []string `json:"servers,omitempty"`
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	view := activityView{Local: a.onPersonalMachine(), Operations: []activityOperation{}, Replica: a.Replica}
	if a.Replica {
		if role, err := a.Store.CopyRole(ctx); err == nil {
			view.Main = a.otherCopy(ctx, role)
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	view.Operations = a.operations(ctx, ownerID(ctx))
	writeJSON(w, http.StatusOK, view)
}

// operations lists what this copy is doing on machines for a person.
func (a *API) operations(ctx context.Context, owner string) []activityOperation {
	out := []activityOperation{}
	a.transfer.mu.Lock()
	if a.transfer.running {
		op := activityOperation{Kind: "transfer", ID: "transfer", Name: "MyInfra", To: "/transfert/suivi"}
		if !a.transfer.started.IsZero() {
			since := a.transfer.started
			op.Since = &since
		}
		out = append(out, op)
	}
	a.transfer.mu.Unlock()
	if rows, err := a.Store.OpenDeployments(ctx, owner); err == nil {
		for _, row := range rows {
			item := deploymentOf(row)
			since := row.CreatedAt
			out = append(out, activityOperation{Kind: "deploy", ID: item.ID, Name: item.Name, To: item.To, Since: &since})
		}
	}
	if rows, err := a.Store.RunningDestructions(ctx); err == nil {
		for _, row := range rows {
			if row.OwnerID != owner {
				continue
			}
			since := row.CreatedAt
			out = append(out, activityOperation{Kind: "destroy", ID: row.ID, Name: row.Name, To: "/destructions/" + row.ID, Since: &since})
		}
	}
	if jobs, err := a.Store.RunningUpdateJobs(ctx); err == nil {
		for _, job := range jobs {
			if job.OwnerID != owner {
				continue
			}
			since := job.CreatedAt
			op := activityOperation{Kind: "update", ID: job.ID, Name: a.jobView(ctx, job).Name, To: "/mises-a-jour", Since: &since, ClusterID: job.ClusterID}
			var steps []updateStep
			_ = json.Unmarshal(job.Steps, &steps)
			for _, step := range steps {
				if len(op.Servers) == 0 || op.Servers[len(op.Servers)-1] != step.ServerID {
					op.Servers = append(op.Servers, step.ServerID)
				}
			}
			out = append(out, op)
		}
	}
	if ids, started, err := a.Store.PendingRollouts(ctx); err == nil {
		for i, id := range ids {
			app, err := a.Store.Application(ctx, owner, id)
			if err != nil {
				continue
			}
			since := started[i]
			op := activityOperation{Kind: "rollout", ID: id, Name: app.Name, To: "/applications/" + id, Since: &since, ClusterID: app.ClusterID}
			if app.ServerID != "" {
				op.Servers = []string{app.ServerID}
			}
			out = append(out, op)
		}
	}
	return out
}

// onPersonalMachine is true for a copy on the person's computer: the
// executable, or a copy reached on a local address. Switching it off cuts
// what it drives over SSH.
func (a *API) onPersonalMachine() bool {
	if a.Local {
		return true
	}
	parsed, err := url.Parse(a.PublicURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}
