package api

import (
	"context"
	"net/http"

	"myinfra/internal/assistant"
	"myinfra/internal/commit"
	"myinfra/internal/store"
)

// assistantAuth guards the assistant routes by role, on the server, for
// every route the assistant service mounts:
//
//   - consultation (status, pricing list, usage, usage stream): any
//     signed-in account, the read-only one included, so the pane can show
//     past work;
//   - a new turn with a provider (chat): deploy or admin. The read-only
//     role never reaches the Provider Adapter;
//   - keys and pricing changes: admin.
//
// The role also travels in the context so the Harness Router refuses a
// read-only turn by itself, should a route be reached another way.
func (a *API) assistantAuth(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		role := a.effectiveRole(r.Context())
		if roleRank(role) < roleRank(assistantRouteRole(r.Pattern)) {
			writeError(w, http.StatusForbidden, "Droits insuffisants.")
			return
		}
		next(w, r.WithContext(assistant.WithRole(r.Context(), role)))
	})
}

// assistantRouteRole is the least role a mounted assistant route needs.
func assistantRouteRole(pattern string) string {
	switch pattern {
	case "POST /api/assistant/chat":
		return store.RoleDeploy
	case "PUT /api/assistant/pricing", "PUT /api/assistant/keys/{provider}", "DELETE /api/assistant/keys/{provider}":
		return store.RoleAdmin
	default:
		return store.RoleRead
	}
}

// commitActor is who asks the Commit Gateway to write, as the backend sees
// them: the effective role (capped by an access token) and the explicit
// "write templates" permission of the account. The deploy role alone does
// not write; an admin always does.
func (a *API) commitActor(ctx context.Context) commit.Actor {
	role := a.effectiveRole(ctx)
	actor := commit.Actor{ID: userID(ctx), Admin: role == store.RoleAdmin}
	if roleRank(role) >= roleRank(store.RoleDeploy) {
		if account, err := a.Store.UserAccount(ctx, userID(ctx)); err == nil {
			actor.WriteTemplates = account.WriteTemplates
		}
	}
	return actor
}
