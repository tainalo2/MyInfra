package api

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"myinfra/internal/auth"
	"myinfra/internal/store"
)

type userView struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Role  string `json:"role"`
	TOTP  bool   `json:"totp"`
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListUserAccounts(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]userView, 0, len(rows))
	for _, row := range rows {
		out = append(out, userView{ID: row.ID, Login: row.Login, Role: row.Role, TOTP: row.TOTPSecret != ""})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if len(body.Password) < 8 || len(body.Password) > 72 {
		writeError(w, http.StatusBadRequest, "Le mot de passe doit contenir entre 8 et 72 caractères.")
		return
	}
	role := body.Role
	if role != store.RoleAdmin && role != store.RoleDeploy && role != store.RoleRead {
		writeError(w, http.StatusBadRequest, "Rôle inconnu.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	if err := a.Store.CreateUserAccount(r.Context(), id, body.Login, role, string(hash)); err != nil {
		writeError(w, http.StatusBadRequest, "Ce nom de connexion existe déjà.")
		return
	}
	writeJSON(w, http.StatusCreated, userView{ID: id, Login: body.Login, Role: role})
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == userID(r.Context()) {
		writeError(w, http.StatusBadRequest, "Vous ne pouvez pas supprimer votre propre compte.")
		return
	}
	n, _ := a.Store.CountUsers(r.Context())
	if n <= 1 {
		writeError(w, http.StatusBadRequest, "Il doit rester au moins un compte.")
		return
	}
	if err := a.Store.DeleteUserAccount(r.Context(), r.PathValue("id")); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) updateAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login string `json:"login"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	login := strings.TrimSpace(body.Login)
	if login == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un identifiant.")
		return
	}
	id := userID(r.Context())
	if err := a.Store.UpdateUserLogin(r.Context(), id, login); err != nil {
		if err.Error() == "identifiant pris" {
			writeError(w, http.StatusBadRequest, "Ce nom de connexion existe déjà.")
			return
		}
		a.mapErr(w, err)
		return
	}
	account, err := a.Store.UserAccount(r.Context(), id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"login": account.Login})
}

func (a *API) prepareTOTP(w http.ResponseWriter, r *http.Request) {
	account, err := a.Store.UserAccount(r.Context(), userID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if account.TOTPSecret != "" {
		writeError(w, http.StatusConflict, "La double authentification est déjà active.")
		return
	}
	secret, err := auth.GenerateSecret()
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"uri":    auth.ProvisioningURI("MyInfra", account.Login, secret),
	})
}

func (a *API) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string `json:"secret"`
		TOTP   string `json:"totp"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	account, err := a.Store.UserAccount(r.Context(), userID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if account.TOTPSecret != "" {
		writeError(w, http.StatusConflict, "La double authentification est déjà active.")
		return
	}
	secret := strings.TrimSpace(body.Secret)
	if secret == "" || !auth.Validate(secret, body.TOTP) {
		writeError(w, http.StatusBadRequest, "Code à deux facteurs incorrect.")
		return
	}
	if err := a.Store.SetUserTOTP(r.Context(), userID(r.Context()), secret); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) disableTOTP(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.SetUserTOTP(r.Context(), userID(r.Context()), ""); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
