package api

import (
	"net/http"
	"strings"
	"time"

	"myinfra/internal/store"
)

// tokenPrefix marks MyInfra access tokens, so that secret scanners and people
// recognise one that leaked.
const tokenPrefix = "myi_"

// bearerToken reads an access token from the Authorization header.
func bearerToken(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, tokenPrefix) {
		return "", false
	}
	return value, true
}

type apiTokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	User       string     `json:"user,omitempty"`
	Mine       bool       `json:"mine"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Token      string     `json:"token,omitempty"`
}

func toAPIToken(row store.APIToken, me string) apiTokenView {
	return apiTokenView{
		ID: row.ID, Name: row.Name, Role: row.Role, User: row.UserLogin, Mine: row.UserID == me,
		CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt, ExpiresAt: row.ExpiresAt,
	}
}

// personOnly keeps access tokens from managing access tokens: a program
// given a token cannot make itself another one, nor keep its own alive.
func (a *API) personOnly(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := tokenOf(r.Context()); ok {
			writeError(w, http.StatusForbidden, "Les jetons d'accès se gèrent depuis l'interface de MyInfra.")
			return
		}
		next(w, r)
	})
}

func (a *API) listAPITokens(w http.ResponseWriter, r *http.Request) {
	me := userID(r.Context())
	scope := me
	if a.userRole(r.Context(), me) == store.RoleAdmin {
		scope = ""
	}
	rows, err := a.Store.ListAPITokens(r.Context(), scope)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]apiTokenView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIToken(row, me))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createAPIToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
		Days int    `json:"days"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au jeton.")
		return
	}
	if roleRank(body.Role) == 0 {
		writeError(w, http.StatusBadRequest, "Choisissez un rôle.")
		return
	}
	me := userID(r.Context())
	if roleRank(body.Role) > roleRank(a.userRole(r.Context(), me)) {
		writeError(w, http.StatusForbidden, "Un jeton ne peut pas avoir plus de droits que vous.")
		return
	}
	if body.Days < 0 || body.Days > 3660 {
		writeError(w, http.StatusBadRequest, "La durée va de 1 jour à 10 ans, ou sans fin.")
		return
	}
	row := store.APIToken{ID: newID(), UserID: me, Name: name, Role: body.Role, CreatedAt: time.Now()}
	if body.Days > 0 {
		expires := time.Now().Add(time.Duration(body.Days) * 24 * time.Hour)
		row.ExpiresAt = &expires
	}
	secret := tokenPrefix + randomToken()
	if err := a.Store.CreateAPIToken(r.Context(), row, hashToken(secret)); err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), ownerID(r.Context()), newID(), "token.created", "info", "Jeton d'accès créé : "+name+" ("+body.Role+").", "", "")
	view := toAPIToken(row, me)
	view.Token = secret
	writeJSON(w, http.StatusCreated, view)
}

func (a *API) deleteAPIToken(w http.ResponseWriter, r *http.Request) {
	me := userID(r.Context())
	scope := me
	if a.userRole(r.Context(), me) == store.RoleAdmin {
		scope = ""
	}
	if err := a.Store.DeleteAPIToken(r.Context(), scope, r.PathValue("id")); err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), ownerID(r.Context()), newID(), "token.revoked", "warn", "Jeton d'accès révoqué.", "", "")
	w.WriteHeader(http.StatusNoContent)
}
