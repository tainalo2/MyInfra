package api

import (
	"context"
	"net/http"

	"myinfra/internal/store"
)

func roleRank(role string) int {
	switch role {
	case store.RoleAdmin:
		return 3
	case store.RoleDeploy:
		return 2
	case store.RoleRead:
		return 1
	default:
		return 0
	}
}

func (a *API) userRole(ctx context.Context, userID string) string {
	row, err := a.Store.UserAccount(ctx, userID)
	if err != nil {
		return store.RoleRead
	}
	if row.Role == "" {
		return store.RoleAdmin
	}
	return row.Role
}

func (a *API) deployAuth(next http.HandlerFunc) http.HandlerFunc {
	return a.requireRole(store.RoleDeploy)(a.drives(next))
}

func (a *API) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return a.requireRole(store.RoleAdmin)(next)
}

func (a *API) adminDrive(next http.HandlerFunc) http.HandlerFunc {
	return a.requireRole(store.RoleAdmin)(a.drives(next))
}

func (a *API) requireRole(minRole string) func(http.HandlerFunc) http.HandlerFunc {
	need := roleRank(minRole)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return a.auth(func(w http.ResponseWriter, r *http.Request) {
			if roleRank(a.userRole(r.Context(), userID(r.Context()))) < need {
				writeError(w, http.StatusForbidden, "Droits insuffisants.")
				return
			}
			next(w, r)
		})
	}
}
