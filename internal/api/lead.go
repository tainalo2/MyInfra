package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"myinfra/internal/store"
)

// One copy drives the machines at a time; the others follow it by sync and
// stay ready to take over. Each change of lead raises the term of the copies
// that take part, and a copy that finds a later term elsewhere stops
// driving: a main copy that crashed and comes back after a takeover steps
// aside instead of driving next to the new one.

// otherCopyView is the other copy a transfer left: the one this copy follows,
// or the one that follows it once this copy took the lead back.
type otherCopyView struct {
	URL    string     `json:"url"`
	Method string     `json:"method,omitempty"`
	Drives bool       `json:"drives"`
	LastOK *time.Time `json:"lastOk,omitempty"`
	// LastError is the last failed exchange with it, empty once it answers.
	LastError string `json:"lastError,omitempty"`
}

// otherCopy describes the other copy of the role, with the last exchange
// with it.
func (a *API) otherCopy(ctx context.Context, role store.CopyRole) *otherCopyView {
	if role.MainURL == "" {
		return nil
	}
	view := &otherCopyView{URL: role.MainURL, Method: role.MainMethod, Drives: role.Replica}
	if peers, err := a.Store.ListPeers(ctx); err == nil {
		for _, peer := range peers {
			if peer.URL == role.MainURL {
				view.LastOK, view.LastError = peer.LastOK, peer.LastError
			}
		}
	}
	return view
}

type termBody struct {
	Epoch int64 `json:"epoch"`
}

// takeLead makes this replica the copy that drives the machines: when the
// main copy fails, or to act from here. The main copy, when it answers,
// gets the last changes of this one and steps aside first; when it does not,
// the person forces the takeover, and it steps aside once it is back.
func (a *API) takeLead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Force bool `json:"force"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if !a.Replica {
		writeCoded(w, http.StatusConflict, "lead_already", "Cette copie pilote déjà les machines.", nil)
		return
	}
	if a.RoleFixed {
		writeCoded(w, http.StatusConflict, "role_fixed", "MYINFRA_REPLICA fixe le rôle de cette copie : relancez-la sans cette variable pour prendre la main.", nil)
		return
	}
	ctx := r.Context()
	owner := ownerID(ctx)
	role, err := a.Store.CopyRole(ctx)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	epoch := role.Epoch + 1
	reached := false
	if role.MainURL != "" {
		if state, err := a.peerState(ctx, role.MainURL); err == nil {
			if state.Epoch >= epoch {
				epoch = state.Epoch + 1
			}
			// The changes made here reach it before it stops driving.
			a.syncPeers(ctx)
			err := a.peerTerm(ctx, role.MainURL, "/api/sync/yield", epoch)
			if errors.Is(err, errOutdatedCopy) {
				// Forced, the two would drive side by side: that copy runs
				// and would never step aside.
				writeCoded(w, http.StatusConflict, "main_outdated", "La copie principale répond, mais sa version ne sait pas céder la main : mettez-la à jour, ou retirez-la, avant de prendre la main d'ici.", nil)
				return
			}
			reached = err == nil
		}
	}
	if !reached && !body.Force {
		writeCoded(w, http.StatusConflict, "main_unreachable", "La copie principale ne répond pas : forcez la reprise, elle cédera la main dès qu'elle sera de retour.", nil)
		return
	}
	if role.MainURL != "" {
		// Kept as a peer: the sync goes on, and tells that copy, once back,
		// that it no longer drives.
		_ = a.Store.AddPeer(ctx, role.MainURL)
	}
	role.Replica, role.Epoch = false, epoch
	if err := a.Store.SetCopyRole(ctx, role); err != nil {
		a.mapErr(w, err)
		return
	}
	message := "Cette copie a pris la main : elle pilote les machines."
	if !reached {
		message = "Cette copie a pris la main sans réponse de la copie principale : celle-ci cédera dès son retour."
	}
	a.note(ctx, owner, newID(), "copy.lead", "warn", message, "", "")
	a.Replica = false
	a.resumeDestructions()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "restarting", "reached": reached})
	a.restartForRole()
}

// handOver gives the lead back to the other copy, once it answers again:
// this copy syncs it, starts again as its replica, and the other drives.
func (a *API) handOver(w http.ResponseWriter, r *http.Request) {
	if a.Replica {
		writeError(w, http.StatusConflict, replicaRefused)
		return
	}
	if a.RoleFixed {
		writeCoded(w, http.StatusConflict, "role_fixed", "MYINFRA_REPLICA fixe le rôle de cette copie : relancez-la sans cette variable pour prendre la main.", nil)
		return
	}
	ctx := r.Context()
	owner := ownerID(ctx)
	if len(a.operations(ctx, owner)) > 0 {
		writeCoded(w, http.StatusConflict, "lead_busy", "Des opérations sont en cours sur les machines : attendez leur fin pour rendre la main.", nil)
		return
	}
	role, err := a.Store.CopyRole(ctx)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if role.MainURL == "" {
		writeError(w, http.StatusConflict, "Aucune autre copie ne peut reprendre la main.")
		return
	}
	state, err := a.peerState(ctx, role.MainURL)
	if err != nil {
		writeCoded(w, http.StatusBadGateway, "copy_unreachable", "L'autre copie ne répond pas : la main reste ici.", nil)
		return
	}
	epoch := max(role.Epoch, state.Epoch) + 1
	a.syncPeers(ctx)
	previous := role
	role.Replica, role.Epoch = true, epoch
	// Stopped first here, so that the two never drive at once.
	if err := a.Store.SetCopyRole(ctx, role); err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.peerTerm(ctx, role.MainURL, "/api/sync/lead", epoch); err != nil {
		_ = a.Store.SetCopyRole(ctx, previous)
		writeCoded(w, http.StatusBadGateway, "copy_unreachable", "L'autre copie n'a pas pris la main : elle reste ici.", nil)
		return
	}
	a.note(ctx, owner, newID(), "copy.handover", "info", "La main est rendue à "+role.MainURL+" : cette copie la suit de nouveau.", "", "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	a.restartForRole()
}

// syncYield, called by a copy that takes the lead with a later term, makes
// this one a replica of it.
func (a *API) syncYield(w http.ResponseWriter, r *http.Request) {
	a.syncTerm(w, r, true)
}

// syncLead, called by the copy that hands over, makes this one drive.
func (a *API) syncLead(w http.ResponseWriter, r *http.Request) {
	a.syncTerm(w, r, false)
}

func (a *API) syncTerm(w http.ResponseWriter, r *http.Request, replica bool) {
	if !a.allowSync(w, r) {
		return
	}
	var body termBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if a.RoleFixed {
		writeCoded(w, http.StatusConflict, "role_fixed", "MYINFRA_REPLICA fixe le rôle de cette copie.", nil)
		return
	}
	role, err := a.Store.CopyRole(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if body.Epoch <= role.Epoch {
		writeCoded(w, http.StatusConflict, "term_stale", "Cette copie connaît un mandat plus récent.", nil)
		return
	}
	next := store.CopyRole{Replica: replica, Epoch: body.Epoch}
	if replica {
		// The copy that leads now may be out of reach, on a computer: it
		// syncs this one from there.
		next.MainMethod = "local"
	}
	if err := a.Store.SetCopyRole(r.Context(), next); err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	if replica != a.Replica {
		if !replica {
			a.Replica = false
			a.resumeDestructions()
		}
		a.restartForRole()
	}
}

// errOutdatedCopy is the answer of a copy older than the terms: it knows
// neither route.
var errOutdatedCopy = errors.New("version sans changement de main")

// peerTerm passes a term to another copy, on one of the two routes above.
func (a *API) peerTerm(ctx context.Context, peer, path string, epoch int64) error {
	body, _ := json.Marshal(termBody{Epoch: epoch})
	res, err := a.peerRequest(ctx, http.MethodPost, peer, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed {
		return errOutdatedCopy
	}
	if res.StatusCode != http.StatusOK {
		return syncHTTPError(res.StatusCode)
	}
	// Only a copy that knows the route says so: a page served in its place
	// changed nothing.
	var answer struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&answer); err != nil || answer.Status != "ok" {
		return errOutdatedCopy
	}
	return nil
}

// fence settles two copies that both drive, met during the sync: the one
// with the earlier term steps aside.
func (a *API) fence(ctx context.Context, peer string, state peerState) {
	if a.Replica || !state.Driving || a.RoleFixed {
		return
	}
	role, err := a.Store.CopyRole(ctx)
	if err != nil {
		return
	}
	switch {
	case state.Epoch < role.Epoch:
		if err := a.peerTerm(ctx, peer, "/api/sync/yield", role.Epoch); err == nil {
			slog.Warn("l'autre copie pilotait encore, elle cède la main", "peer", peer)
		}
	case state.Epoch > role.Epoch:
		slog.Warn("une autre copie a pris la main, celle-ci la suit", "peer", peer)
		role.Replica, role.Epoch, role.MainURL = true, state.Epoch, peer
		if err := a.Store.SetCopyRole(ctx, role); err == nil {
			a.restartForRole()
		}
	}
}

// WatchRole starts this copy again when its role changes underneath it: on
// the other pod of MyInfra in a cluster, which shares its database with the
// one that received the change.
func (a *API) WatchRole(ctx context.Context) {
	if a.RoleFixed {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if role, err := a.Store.CopyRole(ctx); err == nil && role.Replica != a.Replica {
				slog.Info("le rôle de cette copie a changé, elle redémarre")
				a.restartForRole()
				return
			}
		}
	}
}

// roleRestartDelay leaves the answer the time to reach the screen.
var roleRestartDelay = 3 * time.Second

// restartForRole starts this copy again a moment later, once the answer is
// sent, to drive or follow as its role now says.
func (a *API) restartForRole() {
	if !a.roleChanging.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(roleRestartDelay)
		if a.Restart != nil {
			a.Restart()
			return
		}
		if a.Shutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = a.Shutdown(ctx)
		}
	}()
}
