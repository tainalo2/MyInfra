package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"myinfra/internal/box"
	"myinfra/internal/manifest"
	"myinfra/internal/pack"
	"myinfra/internal/store"
)

// A variable whose name looks like a password or a key is sealed with the
// master key before it is written, and only opened when it is delivered.

const sealedPrefix = "myinfra:sealed:v1:"

// maskedValue stands for a sealed value on the screen.
const maskedValue = "••••••••"

func sensitive(name string) bool {
	return pack.Sensitive(name)
}

type sealedValue struct {
	C []byte `json:"c"`
	N []byte `json:"n"`
	W []byte `json:"w"`
}

func (a *API) sealVars(vars map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(vars))
	for key, value := range vars {
		if !sensitive(key) || value == "" || strings.HasPrefix(value, sealedPrefix) || a.Box == nil {
			out[key] = value
			continue
		}
		sealed, err := a.Box.Seal([]byte(value))
		if err != nil {
			return nil, fmt.Errorf("Une variable n'a pas pu être chiffrée.")
		}
		raw, err := json.Marshal(sealedValue{C: sealed.Ciphertext, N: sealed.Nonce, W: sealed.WrappedDEK})
		if err != nil {
			return nil, err
		}
		out[key] = sealedPrefix + base64.RawStdEncoding.EncodeToString(raw)
	}
	return out, nil
}

func (a *API) openVars(vars map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(vars))
	for key, value := range vars {
		if !strings.HasPrefix(value, sealedPrefix) {
			out[key] = value
			continue
		}
		plain, err := a.openValue(value)
		if err != nil {
			return nil, err
		}
		out[key] = plain
	}
	return out, nil
}

func (a *API) openValue(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, sealedPrefix))
	if err != nil {
		return "", fmt.Errorf("Une variable est illisible.")
	}
	var sealed sealedValue
	if err := json.Unmarshal(raw, &sealed); err != nil || a.Box == nil {
		return "", fmt.Errorf("Une variable est illisible.")
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sealed.C, Nonce: sealed.N, WrappedDEK: sealed.W})
	if err != nil {
		return "", fmt.Errorf("Une variable est illisible.")
	}
	return string(plain), nil
}

// maskVars is what the screen receives: sensitive values never travel.
func maskVars(vars map[string]string) map[string]string {
	if len(vars) == 0 {
		return vars
	}
	out := make(map[string]string, len(vars))
	for key, value := range vars {
		if sensitive(key) || strings.HasPrefix(value, sealedPrefix) {
			out[key] = maskedValue
			continue
		}
		out[key] = value
	}
	return out
}

// shareableVars leaves out what must not travel in a template file. A
// database password is created again when the template is deployed.
func shareableVars(vars map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range vars {
		if sensitive(key) || strings.HasPrefix(value, sealedPrefix) {
			continue
		}
		out[key] = value
	}
	return out
}

// revealVariable returns one sealed value, on purpose, to its owner.
func (a *API) revealVariable(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	value, ok := app.Variables[r.PathValue("name")]
	if !ok {
		writeError(w, http.StatusNotFound, "Variable introuvable.")
		return
	}
	plain, err := a.openVars(map[string]string{"v": value})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.note(context.Background(), ownerID(r.Context()), newID(), "variable.revealed", "info", "Variable affichée : "+r.PathValue("name")+".", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"value": plain["v"]})
}

// setVariables changes the variables of a running application. The copies
// restart one after the other with them; when they do not start, the
// previous variables are put back.
func (a *API) setVariables(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Variables map[string]string `json:"variables"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if blocked := rolloutBlocked(app); blocked != "" {
		writeError(w, http.StatusConflict, blocked)
		return
	}
	next := map[string]string{}
	for key, value := range body.Variables {
		key = strings.TrimSpace(key)
		// A masked value is the sealed one, unchanged.
		if old, ok := app.Variables[key]; ok && value == maskedValue {
			value = old
		}
		next[key] = value
	}
	if !manifest.EnvOK(next) {
		writeError(w, http.StatusBadRequest, "Une variable est illisible : un nom fait de lettres, chiffres et _, une valeur sur une ligne.")
		return
	}
	sealed, err := a.sealVars(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sameVars(sealed, app.Variables) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	images := make([]store.ImageSnap, 0, len(app.Services))
	for _, svc := range app.Services {
		images = append(images, store.ImageSnap{Name: svc.Name, Image: svc.Image})
	}
	if err := a.Store.BeginRollout(r.Context(), app.ID, app.RunStatus, images); err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.KeepRollbackVariables(r.Context(), app.ID, app.Variables); err != nil {
		_ = a.Store.FinishRollout(r.Context(), app.ID, "", "", "")
		a.mapErr(w, err)
		return
	}
	if err := a.Store.SetVariables(r.Context(), app.ID, sealed); err != nil {
		_ = a.Store.FinishRollout(r.Context(), app.ID, "", "", "")
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), owner, newID(), "app.update", "info", "Variables de "+app.Name+" modifiées.", "", app.ClusterID)
	a.startRollout(app.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "updating"})
}

func sameVars(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}
