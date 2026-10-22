package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"myinfra/internal/box"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
)

var sharedObject = regexp.MustCompile(`^sec[a-z0-9]{1,40}$`)

type grantError struct {
	text   string
	code   string
	params map[string]string
}

func (e grantError) Error() string { return e.text }

type appRefView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type appSecretView struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	KeyCount int          `json:"keyCount,omitempty"`
	Keys     []string     `json:"keys,omitempty"`
	Apps     []appRefView `json:"apps"`
}

func (a *API) listAppSecrets(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	rows, err := a.Store.ListAppSecrets(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	refs, err := a.Store.ApplicationRefs(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]appSecretView, 0, len(rows))
	for _, row := range rows {
		view, err := a.secretView(row, refs, false)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getAppSecret(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	row, err := a.Store.AppSecret(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	refs, err := a.Store.ApplicationRefs(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	view, err := a.secretView(row, refs, true)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) createAppSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string            `json:"name"`
		Entries map[string]string `json:"entries"`
		Apps    []string          `json:"apps"`
	}
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Indiquez un nom.")
		return
	}
	if err := entriesOK(body.Entries); err != nil {
		writeGrant(w, err)
		return
	}
	owner := ownerID(r.Context())
	apps, err := a.ownedApps(r, owner, body.Apps)
	if err != nil {
		writeGrant(w, err)
		return
	}
	if err := a.grantClash(r, owner, "", body.Entries, apps); err != nil {
		writeGrant(w, err)
		return
	}
	raw, err := json.Marshal(body.Entries)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed, err := a.seal(raw)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	row := store.AppSecret{
		ID: newID(), OwnerID: owner, Name: name, Apps: apps,
		Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce, WrappedDEK: sealed.WrappedDEK,
	}
	if err := a.Store.CreateAppSecret(r.Context(), row); err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": row.ID})
}

func (a *API) updateAppSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   *string           `json:"name"`
		Set    map[string]string `json:"set"`
		Remove []string          `json:"remove"`
		Apps   *[]string         `json:"apps"`
	}
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	owner := ownerID(r.Context())
	row, err := a.Store.AppSecret(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if body.Name != nil {
		name, ok := cleanName(*body.Name)
		if !ok {
			writeError(w, http.StatusBadRequest, "Indiquez un nom.")
			return
		}
		row.Name = name
	}
	data, err := a.openEntries(row)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	for _, key := range body.Remove {
		delete(data, key)
	}
	for key, value := range body.Set {
		if value == "" {
			writeError(w, http.StatusBadRequest, "Une valeur vide n'est pas enregistrée. Retirez la clé si elle ne sert plus.")
			return
		}
		data[key] = value
	}
	if err := entriesOK(data); err != nil {
		writeGrant(w, err)
		return
	}
	if body.Apps != nil {
		apps, err := a.ownedApps(r, owner, *body.Apps)
		if err != nil {
			writeGrant(w, err)
			return
		}
		row.Apps = apps
	}
	if err := a.grantClash(r, owner, row.ID, data, row.Apps); err != nil {
		writeGrant(w, err)
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed, err := a.seal(raw)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	row.Ciphertext, row.Nonce, row.WrappedDEK = sealed.Ciphertext, sealed.Nonce, sealed.WrappedDEK
	if err := a.Store.UpdateAppSecret(r.Context(), row); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteAppSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConfirmedName string `json:"confirmedName"`
	}
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	owner := ownerID(r.Context())
	row, err := a.Store.AppSecret(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if body.ConfirmedName != row.Name {
		writeError(w, http.StatusBadRequest, "La confirmation ne correspond pas.")
		return
	}
	if err := a.Store.DeleteAppSecret(r.Context(), owner, row.ID); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) secretView(row store.AppSecret, refs []store.AppRef, withKeys bool) (appSecretView, error) {
	data, err := a.openEntries(row)
	if err != nil {
		return appSecretView{}, err
	}
	names := map[string]string{}
	for _, ref := range refs {
		names[ref.ID] = ref.Name
	}
	view := appSecretView{ID: row.ID, Name: row.Name, Apps: []appRefView{}}
	for _, id := range row.Apps {
		name, ok := names[id]
		if !ok {
			continue
		}
		view.Apps = append(view.Apps, appRefView{ID: id, Name: name})
	}
	sort.Slice(view.Apps, func(i, j int) bool { return view.Apps[i].Name < view.Apps[j].Name })
	if withKeys {
		view.Keys = keyNames(data)
	} else {
		view.KeyCount = len(data)
	}
	return view, nil
}

func (a *API) openEntries(row store.AppSecret) (map[string]string, error) {
	plain, err := a.Box.Open(box.Sealed{Ciphertext: row.Ciphertext, Nonce: row.Nonce, WrappedDEK: row.WrappedDEK})
	if err != nil {
		return nil, err
	}
	var data map[string]string
	if err := json.Unmarshal(plain, &data); err != nil || data == nil {
		return nil, fmt.Errorf("secret illisible")
	}
	return data, nil
}

func (a *API) ownedApps(r *http.Request, owner string, ids []string) ([]string, error) {
	if ids == nil {
		return []string{}, nil
	}
	refs, err := a.Store.ApplicationRefs(r.Context(), owner)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, ref := range refs {
		known[ref.ID] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !known[id] {
			return nil, grantError{text: "Une application indiquée n'existe pas."}
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (a *API) grantClash(r *http.Request, owner, self string, keys map[string]string, apps []string) error {
	if len(apps) == 0 || len(keys) == 0 {
		return nil
	}
	rows, err := a.Store.ListAppSecrets(r.Context(), owner)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, id := range apps {
		want[id] = true
	}
	for _, row := range rows {
		if row.ID == self {
			continue
		}
		share := false
		for _, id := range row.Apps {
			if want[id] {
				share = true
				break
			}
		}
		if !share {
			continue
		}
		data, err := a.openEntries(row)
		if err != nil {
			return err
		}
		for key := range keys {
			if _, ok := data[key]; ok {
				return grantError{
					text:   "La clé " + key + " est déjà accordée à une de ces applications par « " + row.Name + " ».",
					code:   "secret_key_taken",
					params: map[string]string{"key": key, "name": row.Name},
				}
			}
		}
	}
	return nil
}

func (a *API) attachShared(ctx context.Context, owner, appID string, spec *manifest.App) error {
	rows, err := a.Store.GrantedAppSecrets(ctx, owner, appID)
	if err != nil {
		return err
	}
	return a.appendShared(spec, rows)
}

func (a *API) sharedEnv(ctx context.Context, appID string) (map[string]string, error) {
	rows, err := a.Store.AppSecretEntries(ctx, appID)
	if err != nil {
		return nil, err
	}
	var parts []map[string]string
	for _, row := range rows {
		data, err := a.openEntries(row)
		if err != nil {
			return nil, err
		}
		if err := entriesOK(data); err != nil {
			return nil, err
		}
		parts = append(parts, data)
	}
	return mergeSecretEnv(nil, parts), nil
}

func (a *API) appendShared(spec *manifest.App, rows []store.AppSecret) error {
	for _, row := range rows {
		data, err := a.openEntries(row)
		if err != nil {
			return err
		}
		if err := entriesOK(data); err != nil {
			return err
		}
		spec.Shared = append(spec.Shared, manifest.SharedSecret{Name: manifest.SecretObject(row.ID), Data: data})
	}
	return nil
}

func entriesOK(data map[string]string) error {
	if len(data) == 0 {
		return grantError{text: "Un secret contient au moins une clé."}
	}
	if len(data) > 50 {
		return grantError{text: "Un secret contient au plus 50 clés."}
	}
	if !manifest.EnvOK(data) {
		return grantError{text: "Une clé ou une valeur n'est pas acceptée."}
	}
	return nil
}

func keyNames(data map[string]string) []string {
	names := make([]string, 0, len(data))
	for key := range data {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}

func writeGrant(w http.ResponseWriter, err error) {
	var ge grantError
	if errors.As(err, &ge) {
		if ge.code != "" {
			writeCoded(w, http.StatusBadRequest, ge.code, ge.text, ge.params)
			return
		}
		writeError(w, http.StatusBadRequest, ge.text)
		return
	}
	writeError(w, http.StatusInternalServerError, "Une erreur est survenue.")
}

func mergeSecretEnv(base map[string]string, shared []map[string]string) map[string]string {
	out := map[string]string{}
	for _, data := range shared {
		for key, value := range data {
			out[key] = value
		}
	}
	for key, value := range base {
		out[key] = value
	}
	return out
}
