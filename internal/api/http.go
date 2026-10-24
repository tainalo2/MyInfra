package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"myinfra/internal/box"
	"myinfra/internal/store"
	"myinfra/internal/verify"
)

type API struct {
	Store        *store.Store
	Box          *box.Box
	Verify       verify.Client
	PublicURL    string
	AppsDomain   string
	SecureCookie bool
	Image        string
	// Version is the release this copy runs, "dev" when built from source.
	Version string
	// Postgres is the major version of the PostgreSQL the executable carries.
	Postgres string
	// Local is true for the executable with its own PostgreSQL.
	Local bool
	// Restart stops the server and starts the executable again, after an update.
	Restart func()
	// Stop ends MyInfra.app on macOS, which runs without a window to close.
	Stop      func()
	Master    []byte
	Bootstrap string
	Replica   bool
	// RoleFixed is true when MYINFRA_REPLICA sets the role, whatever the
	// database says: the lead cannot change from the screen.
	RoleFixed    bool
	roleChanging atomic.Bool
	reach        reachCache
	Shutdown     func(context.Context) error
	transfer     transferState
	deployMu     sync.Mutex
	deployStopMu sync.Mutex
	deployStop   map[string]context.CancelFunc
	deliverMu    sync.Mutex
	publishMu    sync.Mutex
	namesMu      sync.Mutex
	appliedMu    sync.Mutex
	applied      map[string]appliedState
	swept        map[string]time.Time
	starting     map[string]time.Time
	// Guarded by appliedMu: when certificates were last kept, and when
	// Traefik was last asked again for missing ones, per cluster.
	certificateSwept   map[string]time.Time
	certificateRetried map[string]time.Time
	failures           map[string]int
	published          map[string]string
	templateMu         sync.Mutex
	destructionMu      sync.Mutex
	syncMu             sync.Mutex
	destructionBusy    map[string]bool
	authGates          gateSet
	alerts             *alertState
	// router is the last router built, kept for the tests to look routes up.
	router *http.ServeMux
}

func (a *API) Handler() http.Handler { return a.routes() }

type ctxKey int

const userKey ctxKey = 1
const ownerKey ctxKey = 2
const tokenKey ctxKey = 3
const sourceKey ctxKey = 4

func withUser(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userKey, id)
}

func withOwner(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ownerKey, id)
}

func withToken(ctx context.Context, token store.APIToken) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

// tokenOf is the access token a request came with, when it did not come
// with a session.
func tokenOf(ctx context.Context) (store.APIToken, bool) {
	token, ok := ctx.Value(tokenKey).(store.APIToken)
	return token, ok
}

// withSource marks a request made for the MCP server; only that server sets
// it, on the requests it makes itself, so a caller cannot claim it.
func withSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceKey, source)
}

// sourceOf tells where a request comes from: "mcp" for the MCP server, "api"
// for an access token used directly, "ui" for a person's session.
func sourceOf(ctx context.Context) string {
	if source, ok := ctx.Value(sourceKey).(string); ok && source != "" {
		return source
	}
	if _, ok := tokenOf(ctx); ok {
		return "api"
	}
	return "ui"
}

// actorOf names who acted: the access token's name, or empty for a session.
func actorOf(ctx context.Context) string {
	if token, ok := tokenOf(ctx); ok {
		return token.Name
	}
	return ""
}

func userID(ctx context.Context) string {
	id, _ := ctx.Value(userKey).(string)
	return id
}

// ownerID is the account that owns infrastructure rows (first user). Logins
// with other roles still see and act on that same estate.
func ownerID(ctx context.Context) string {
	id, _ := ctx.Value(ownerKey).(string)
	if id != "" {
		return id
	}
	return userID(ctx)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func (a *API) mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Introuvable.")
	case errors.Is(err, store.ErrHasApps):
		writeError(w, http.StatusConflict, "Cette machine porte des applications. Retirez-les avant de la rattacher à un cluster.")
	case errors.Is(err, store.ErrInCluster):
		writeError(w, http.StatusConflict, "Cette machine est dans un cluster. Choisissez le cluster.")
	case errors.Is(err, store.ErrLinked):
		writeError(w, http.StatusConflict, "Cette machine est déjà reliée.")
	case errors.Is(err, store.ErrProviderBusy):
		writeError(w, http.StatusConflict, "Ce provider a encore des serveurs. Supprimez-les avant.")
	case errors.Is(err, store.ErrNameTaken):
		writeError(w, http.StatusConflict, "Ce nom est déjà utilisé.")
	default:
		slog.Error("requête", "err", err)
		writeError(w, http.StatusInternalServerError, "Une erreur est survenue.")
	}
}

func (a *API) seal(plaintext []byte) (store.Secret, error) {
	sealed, err := a.Box.Seal(plaintext)
	if err != nil {
		return store.Secret{}, err
	}
	return store.Secret{Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce, WrappedDEK: sealed.WrappedDEK}, nil
}

func appsDomain(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), ".")
	if raw == "" {
		return "apps.localhost"
	}
	return raw
}
