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
	// Service is the public service the name leads to; empty is the main.
	Service string `json:"service,omitempty"`
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
		out = append(out, store.Name{ID: newID(), Hostname: host, Zone: zone, SecretID: secretID, Service: strings.TrimSpace(item.Service)})
	}
	return out, nil
}

// serviceOfNames ties names to the public services of a running
// application: the main one is kept without service, another must be
// public.
func serviceOfNames(app store.Application, names []store.Name) error {
	var published []string
	public := map[string]bool{}
	for _, svc := range app.Services {
		if svc.Published {
			published = append(published, svc.Name)
			public[svc.Name] = true
		}
	}
	named := map[string]bool{}
	for _, domain := range app.Domains {
		if domain.Service != "" {
			named[domain.Service] = true
		}
	}
	main := store.MainService(published, named)
	for i, name := range names {
		switch {
		case name.Service == "" || name.Service == main:
			names[i].Service = ""
		case !public[name.Service]:
			return errString("Le nom " + name.Hostname + " mène au service " + name.Service + ", qui n'est pas public.")
		}
	}
	return nil
}

// nameChanges says which names to add and which to take away, keeping the
// ones already there as they are: a name kept keeps its record and route.
func nameChanges(current, wanted []store.Name) (add, remove []store.Name) {
	have := map[string]bool{}
	for _, name := range current {
		have[name.Hostname] = true
	}
	// A name that moves to another service is taken away, then given again.
	moved := map[string]bool{}
	for _, name := range wanted {
		for _, old := range current {
			if old.Hostname == name.Hostname && old.Service != name.Service {
				moved[name.Hostname] = true
				have[name.Hostname] = false
			}
		}
	}
	want := map[string]bool{}
	for _, name := range wanted {
		want[name.Hostname] = true
		if !have[name.Hostname] {
			add = append(add, name)
		}
	}
	for _, name := range current {
		if !want[name.Hostname] || moved[name.Hostname] {
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
	before, err := a.Store.Application(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	wanted, err := a.parseNames(r.Context(), owner, body.Names)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := serviceOfNames(before, wanted); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := a.Store.AppNames(r.Context(), id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	add, remove := nameChanges(current, wanted)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Names are added before others go, so that a name refused leaves the
	// application as it was; a name moving to another service goes first,
	// to be given again.
	adding := map[string]bool{}
	for _, name := range add {
		adding[name.Hostname] = true
	}
	var moving, leaving []store.Name
	for _, name := range remove {
		if adding[name.Hostname] {
			moving = append(moving, name)
		} else {
			leaving = append(leaving, name)
		}
	}
	for _, name := range moving {
		if err := a.Store.RemoveName(r.Context(), id, name.Hostname); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	if err := a.Store.AddNames(r.Context(), id, add); err != nil {
		a.mapErr(w, err)
		return
	}
	for _, name := range leaving {
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
