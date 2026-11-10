package assistant

import (
	"context"
	"net/http"

	harness "myinfra/internal/harness-router"
)

type roleKey struct{}

// WithRole marks the request with the role the caller acts with. The API
// sets it after authentication; the chat handler hands it to the Harness
// Router, which refuses a read-only role before any provider call.
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, roleKey{}, role)
}

// RoleOf is the role set by WithRole, or empty when none was set: the
// router then refuses, so a route mounted without the API wrap is closed.
func RoleOf(ctx context.Context) string {
	role, _ := ctx.Value(roleKey{}).(string)
	return role
}

// CanChat mirrors the router's rule, for callers that want to refuse early.
func CanChat(role string) bool { return harness.CanChat(role) }

// AsRole is a wrap for Register that marks every request with one role,
// for an embedder that authenticated otherwise, or a test. The API of
// MyInfra uses its own wrap, which reads the role of the signed-in account.
func AsRole(role string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(WithRole(r.Context(), role)))
		}
	}
}
