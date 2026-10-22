package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/dns"
	"myinfra/internal/store"
)

// The names of an application change while it runs: only its routes move.
// A new name waits for its DNS record, then the next delivery gives it a
// route and Traefik a certificate; a name taken away loses its record first,
// then the next delivery prunes its route.

type nameBody struct {
	Subdomain string `json:"subdomain"`
	Zone      string `json:"zone"`
}

// parseNames checks the names asked for and finds the DNS account of each zone.
func (a *API) parseNames(ctx context.Context, owner string, items []nameBody) ([]store.Name, error) {
	if len(items) > 10 {
		return nil, errString("Dix noms au plus.")
	}
	seen := map[string]bool{}
	accounts := map[string]string{}
	var out []store.Name
	for _, item := range items {
		host, err := dns.Join(item.Subdomain, item.Zone)
		if err != nil {
			return nil, err
		}
		if seen[host] {
			return nil, errString("Ce nom est déjà dans la liste.")
		}
		seen[host] = true
		zone := strings.ToLower(strings.Trim(strings.TrimSpace(item.Zone), "."))
		secretID, ok := accounts[zone]
		if !ok {
			found, err := a.accountForZone(ctx, owner, zone)
			if err != nil {
				return nil, err
			}
			secretID = found
			accounts[zone] = secretID
		}
		out = append(out, store.Name{ID: newID(), Hostname: host, Zone: zone, SecretID: secretID})
	}
	return out, nil
}

// nameChanges says which names to add and which to take away, keeping the
// ones already there as they are: a name kept keeps its record and route.
func nameChanges(current, wanted []store.Name) (add, remove []store.Name) {
	have := map[string]bool{}
	for _, name := range current {
		have[name.Hostname] = true
	}
	want := map[string]bool{}
	for _, name := range wanted {
		want[name.Hostname] = true
		if !have[name.Hostname] {
			add = append(add, name)
		}
	}
	for _, name := range current {
		if !want[name.Hostname] {
			remove = append(remove, name)
		}
	}
	return add, remove
}

func (a *API) setNames(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Names []nameBody `json:"names"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	id := r.PathValue("id")
	if id == selfAppID {
		writeError(w, http.StatusConflict, "L'adresse de MyInfra se change depuis Transfert.")
		return
	}
	if _, err := a.Store.Application(r.Context(), owner, id); err != nil {
		a.mapErr(w, err)
		return
	}
	wanted, err := a.parseNames(r.Context(), owner, body.Names)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := a.Store.AppNames(r.Context(), id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	add, remove := nameChanges(current, wanted)
	if err := a.Store.AddNames(r.Context(), id, add); err != nil {
		a.mapErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, name := range remove {
		// The record goes first: a name kept in the list after a failure can
		// be taken away again, a record without a name would be forgotten.
		if err := a.dropName(ctx, owner, name); err != nil {
			slog.Info("dns", "name", name.Hostname, "err", err)
			writeError(w, http.StatusBadGateway, "Le nom "+name.Hostname+" n'a pas pu être retiré du DNS. Réessayez dans un instant.")
			return
		}
		if err := a.Store.RemoveName(r.Context(), id, name.Hostname); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	if len(add) > 0 {
		a.publishPending(ctx)
	}
	if len(add) > 0 || len(remove) > 0 {
		go a.deliverClusters(context.Background())
	}
	app, err := a.Store.Application(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toApp(app))
}
