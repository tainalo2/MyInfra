package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"myinfra/internal/blueprint"
	"myinfra/internal/library"
	"myinfra/internal/market"
	"myinfra/internal/store"
)

type starterApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Logo string `json:"logo,omitempty"`
}

type starterView struct {
	ID            string                           `json:"id"`
	Version       string                           `json:"version"`
	Name          string                           `json:"name"`
	Summary       string                           `json:"summary"`
	Sizing        string                           `json:"sizing"`
	Translations  map[string]library.TemplateWords `json:"translations,omitempty"`
	Servers       int                              `json:"servers"`
	ControlPlanes int                              `json:"controlPlanes"`
	Apps          []starterApp                     `json:"apps"`
	Community     bool                             `json:"community,omitempty"`
}

// listStarters gives the ready-made templates, for the templates page.
func (a *API) listStarters(w http.ResponseWriter, r *http.Request) {
	out := []starterView{}
	for _, starter := range market.Starters() {
		view := starterView{
			ID: starter.ID, Version: starter.Version, Name: starter.Name, Summary: starter.Summary, Sizing: starter.Sizing,
			Translations: starter.Translations, Servers: len(starter.Servers), Apps: []starterApp{}, Community: starter.Community,
		}
		for _, server := range starter.Servers {
			if server.ControlPlane {
				view.ControlPlanes++
			}
		}
		for _, app := range starter.Apps {
			view.Apps = append(view.Apps, starterApp{ID: app.ID, Name: market.ArticleName(app.ID), Logo: market.ArticleLogo(app.ID)})
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
}

// useStarter turns a ready-made template into one of the person's own: its
// servers wait for a provider, an offer and keys, its applications for the
// zone of their names, as in an imported template.
func (a *API) useStarter(w http.ResponseWriter, r *http.Request) {
	starter, ok := market.StarterOf(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(input.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au template.")
		return
	}
	owner := ownerID(r.Context())
	// With a single DNS zone, the names are ready to publish.
	zone := ""
	if zones := a.ownerZones(r, owner); len(zones) == 1 {
		zone = zones[0]
	}
	body, err := starterBody(starter, zone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if message := blueprint.Validate(body); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	// A second use of the same starter gets a numbered name.
	id := newID()
	for attempt := 1; ; attempt++ {
		candidate := name
		if attempt > 1 {
			candidate = fmt.Sprintf("%s %d", name, attempt)
		}
		err = a.Store.CreateTemplate(r.Context(), owner, id, candidate, raw)
		if !errors.Is(err, store.ErrNameTaken) || attempt >= 20 {
			break
		}
	}
	if err != nil {
		a.mapErr(w, err)
		return
	}
	// The template keeps where it comes from, for its newer versions.
	origin := market.StarterOrigin(starter)
	origin.Sum = bodySum(raw)
	if mark, err := json.Marshal(origin); err == nil {
		if err := a.Store.SetTemplateLibrary(r.Context(), owner, id, mark); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	row, err := a.Store.Template(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTemplate(row))
}

// ownerZones lists the DNS zones the person's accounts hold.
func (a *API) ownerZones(r *http.Request, owner string) []string {
	secrets, err := a.Store.ListSecrets(r.Context(), owner)
	if err != nil {
		return nil
	}
	var zones []string
	for _, sec := range secrets {
		if sec.Kind != "dns_token" {
			continue
		}
		if names, err := a.zonesOf(r.Context(), owner, sec.ID); err == nil {
			zones = append(zones, names...)
		}
	}
	return zones
}

// starterBody is the template a starter gives: its servers wait for a
// provider, an offer and keys.
func starterBody(starter market.Starter, zone string) (blueprint.Body, error) {
	body := blueprint.Body{ClusterName: starter.Cluster}
	for _, server := range starter.Servers {
		body.Servers = append(body.Servers, blueprint.Server{Name: server.Name, ControlPlane: server.ControlPlane})
	}
	for _, item := range starter.Apps {
		// The template carries the application whole, as a package: it runs
		// as the catalogue describes it, files and checks included.
		p, ok := market.PackageOf(item.ID)
		if !ok {
			return body, fmt.Errorf("Cet article est introuvable.")
		}
		app := blueprint.App{Name: p.Name, SourceKind: "image", Replicas: 1, Package: &p, Inputs: map[string]string{},
			Names: []blueprint.Name{{Subdomain: item.Subdomain, Zone: zone}}}
		for _, svc := range p.Services {
			if svc.Public {
				app.Source = svc.Image
			}
		}
		body.Applications = append(body.Applications, app)
	}
	return body, nil
}
