package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"myinfra/internal/box"
	"myinfra/internal/catalog"
	"myinfra/internal/provision"
	"myinfra/internal/store"
)

func (a *API) providerOffers(w http.ResponseWriter, r *http.Request) {
	cat, err := a.offers(r, r.PathValue("id"), r.URL.Query().Get("location"), r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cat)
}

func (a *API) providerProjects(w http.ResponseWriter, r *http.Request) {
	sec, tok, err := a.openProvider(r.Context(), userID(r.Context()), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	projects, err := provisionClient(a).Projects(r.Context(), sec.Provider, tok)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if projects == nil {
		projects = []provision.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

func (a *API) offers(r *http.Request, id, location, project string) (catalog.Catalog, error) {
	return a.catalogFor(r.Context(), userID(r.Context()), id, location, project)
}

func (a *API) catalogFor(ctx context.Context, owner, id, location, project string) (catalog.Catalog, error) {
	sec, tok, err := a.openProvider(ctx, owner, id)
	if err != nil {
		return catalog.Catalog{}, err
	}
	client := catalog.Client{
		HTTP: a.Verify.HTTP, HetznerBase: a.Verify.HetznerBase, ScalewayBase: a.Verify.ScalewayBase,
		OVHBase: a.Verify.OVHBase, DigitalOceanBase: a.Verify.DigitalOceanBase,
		InfomaniakBase: a.Verify.InfomaniakBase,
		UpCloudBase:    a.Verify.UpCloudBase, VultrBase: a.Verify.VultrBase, AkamaiBase: a.Verify.AkamaiBase,
		ExoscaleBase: a.Verify.ExoscaleBase, GcoreBase: a.Verify.GcoreBase, KamateraBase: a.Verify.KamateraBase, IonosBase: a.Verify.IonosBase,
		AllowTest: a.Verify.AllowTest,
	}
	cat, err := client.List(ctx, sec.Provider, tok, location)
	if err != nil || sec.Provider != "ovh" || strings.TrimSpace(project) == "" {
		return cat, err
	}
	flavors, err := provisionClient(a).OVHFlavors(ctx, tok, strings.TrimSpace(project), cat.Location)
	if err != nil {
		return catalog.Catalog{}, err
	}
	cat.Offers = catalog.MergeOVH(cat.Offers, flavors)
	return cat, nil
}

func (a *API) openProvider(ctx context.Context, owner, id string) (store.Secret, catalog.Creds, error) {
	sec, err := a.Store.Secret(ctx, owner, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Secret{}, catalog.Creds{}, fmt.Errorf("Introuvable.")
		}
		return store.Secret{}, catalog.Creds{}, fmt.Errorf("Une erreur est survenue.")
	}
	if sec.Kind != "provider_token" {
		return store.Secret{}, catalog.Creds{}, fmt.Errorf("Introuvable.")
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		return store.Secret{}, catalog.Creds{}, fmt.Errorf("Jeton illisible.")
	}
	var tok storedToken
	if err := json.Unmarshal(plain, &tok); err != nil {
		return store.Secret{}, catalog.Creds{}, fmt.Errorf("Jeton illisible.")
	}
	return sec, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, nil
}

func needsProject(provider string) bool {
	return provider == "ovh" || provider == "digitalocean"
}
