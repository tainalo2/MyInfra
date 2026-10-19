package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"myinfra/internal/box"
)

// A variable whose name looks like a password or a key is sealed with the
// master key before it is written, and only opened when it is delivered.
var sensitiveName = regexp.MustCompile(`(?i)(PASS|SECRET|TOKEN|KEY|PRIVATE|CREDENTIAL|SALT)`)

const sealedPrefix = "myinfra:sealed:v1:"

// maskedValue stands for a sealed value on the screen.
const maskedValue = "••••••••"

func sensitive(name string) bool {
	return sensitiveName.MatchString(name)
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
	app, err := a.Store.Application(r.Context(), userID(r.Context()), r.PathValue("id"))
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
	a.note(context.Background(), userID(r.Context()), newID(), "variable.revealed", "info", "Variable affichée : "+r.PathValue("name")+".", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"value": plain["v"]})
}
