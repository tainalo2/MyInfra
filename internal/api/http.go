package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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
	Restart      func()
	Master       []byte
	Bootstrap    string
	Replica      bool
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
}

func (a *API) Handler() http.Handler { return a.routes() }

type ctxKey int

const userKey ctxKey = 1

func withUser(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userKey, id)
}

func userID(ctx context.Context) string {
	id, _ := ctx.Value(userKey).(string)
	return id
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
