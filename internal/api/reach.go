package api

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"myinfra/internal/catalog"
	"myinfra/internal/provision"
	"myinfra/internal/store"
)

// A server shows online only while it answers. Every minute this copy
// knocks on the SSH port of each server; a few misses in a row and it shows
// unreachable, with its applications. The provider is asked now and then,
// and soon after a server stops answering, whether the machine still
// exists: one it no longer has shows as gone, ready to be removed.

const (
	reachEvery = time.Minute
	// lostAfter misses in a row make a server unreachable: a reboot or a
	// short network cut does not.
	lostAfter = 3
	// The provider is asked every providerEvery, and every
	// providerLostEvery while the server does not answer.
	providerEvery     = 6 * time.Hour
	providerLostEvery = 5 * time.Minute
)

// reachCache is what the last round saw, read by the views without a query.
type reachCache struct {
	mu      sync.Mutex
	health  map[string]store.ServerHealth
	cluster map[string]string
	// apps counts, per application, the knocks on its address in a row
	// that got no answer; hosts is the address knocked.
	apps  map[string]int
	hosts map[string]string
}

// WatchReach follows the reachability of the servers.
func (a *API) WatchReach(ctx context.Context) {
	a.checkReach(ctx)
	ticker := time.NewTicker(reachEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkReach(ctx)
		}
	}
}

func (a *API) checkReach(ctx context.Context) {
	servers, err := a.Store.WatchedServers(ctx)
	if err != nil {
		return
	}
	var group sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, srv := range servers {
		if !watched(srv) {
			continue
		}
		group.Add(1)
		go func(srv store.WatchedServer) {
			defer group.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			_ = a.Store.MarkReach(ctx, srv.ID, knocks(ctx, srv.Address))
		}(srv)
	}
	group.Wait()
	healths, err := a.Store.ServerHealths(ctx)
	if err != nil {
		return
	}
	for _, srv := range servers {
		if !watched(srv) || srv.ProviderServerID == "" || srv.ProviderID == "" {
			continue
		}
		health := healths[srv.ID]
		every := providerEvery
		if health.Fails >= lostAfter {
			every = providerLostEvery
		}
		if health.ProviderCheckedAt == nil || time.Since(*health.ProviderCheckedAt) >= every {
			a.checkProvider(ctx, srv, health)
		}
	}
	_ = a.Store.PruneHealth(ctx)
	a.loadReach(ctx, servers)
	a.knockApps(ctx, servers)
}

// knockApps calls the address of each published application: the servers
// may answer while the application does not, on a cluster that lost its
// control plane.
func (a *API) knockApps(ctx context.Context, servers []store.WatchedServer) {
	owners := map[string]bool{}
	for _, srv := range servers {
		owners[srv.OwnerID] = true
	}
	type target struct{ id, host string }
	var targets []target
	for owner := range owners {
		apps, err := a.Store.ListApplications(ctx, owner)
		if err != nil {
			continue
		}
		for _, app := range apps {
			if app.Paused || app.RunStatus != "running" {
				continue
			}
			if host := servedHost(app); host != "" {
				targets = append(targets, target{app.ID, host})
			}
		}
	}
	// MyInfra in a cluster, followed or left by this copy, has its address too.
	if self, ok := a.selfRecord(ctx); ok && self.Hostname != "" {
		targets = append(targets, target{self.ID, self.Hostname})
	}
	results := make([]bool, len(targets))
	var group sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i, item := range targets {
		group.Add(1)
		go func(i int, host string) {
			defer group.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i] = served(ctx, host)
		}(i, item.host)
	}
	group.Wait()
	a.reach.mu.Lock()
	defer a.reach.mu.Unlock()
	fails, hosts := map[string]int{}, map[string]string{}
	for i, item := range targets {
		hosts[item.id] = item.host
		if !results[i] {
			fails[item.id] = a.reach.apps[item.id] + 1
		}
	}
	a.reach.apps, a.reach.hosts = fails, hosts
}

// servedHost is the address an application answers on: its own name
// before the one MyInfra gives, never a local one.
func servedHost(app store.Application) string {
	host := strings.ToLower(strings.TrimSpace(app.Hostname))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ""
	}
	return host
}

// knockClient only looks for an answer: the certificate, the content and
// the redirections do not matter.
var knockClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true},
}

// served tells whether an address answers. A gateway error says the proxy
// has no running copy behind it: no answer either.
func served(ctx context.Context, host string) bool {
	for range 2 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
		if err != nil {
			return false
		}
		res, err := knockClient.Do(req)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode != http.StatusBadGateway && res.StatusCode != http.StatusServiceUnavailable && res.StatusCode != http.StatusGatewayTimeout {
				return true
			}
		}
		if ctx.Err() != nil {
			return false
		}
	}
	return false
}

// appSilent tells whether the address of an application stopped answering.
func (a *API) appSilent(id string) (string, bool) {
	a.reach.mu.Lock()
	defer a.reach.mu.Unlock()
	return a.reach.hosts[id], a.reach.apps[id] >= lostAfter
}

// watched tells whether a server should answer: one being set up may not
// yet.
func watched(srv store.WatchedServer) bool {
	return srv.Address != "" && srv.Status != "pending" && srv.Status != "deploying"
}

// knocks knocks on the SSH port of an address, without logging in: a
// login would count against the penalties of OpenSSH.
func knocks(ctx context.Context, address string) bool {
	host := address
	if _, _, err := net.SplitHostPort(address); err != nil {
		host = net.JoinHostPort(address, "22")
	}
	for range 2 {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", host)
		if err == nil {
			_ = conn.Close()
			return true
		}
		if ctx.Err() != nil {
			return false
		}
	}
	return false
}

// checkProvider asks the provider whether the machine of a server still
// exists, and notes it the first time it is gone.
func (a *API) checkProvider(ctx context.Context, srv store.WatchedServer, before store.ServerHealth) string {
	sec, tok, err := a.openToken(ctx, srv.OwnerID, srv.ProviderID)
	if err != nil {
		_ = a.Store.MarkProvider(ctx, srv.ID, "", "Le compte du provider n'est plus lisible.")
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	exists, err := provisionClient(a).Exists(ctx, sec.Provider, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, srv.ProviderServerID)
	if errors.Is(err, provision.ErrNoCheck) {
		_ = a.Store.MarkProvider(ctx, srv.ID, "", "Ce provider ne permet pas de vérifier la machine.")
		return ""
	}
	if err != nil {
		slog.Info("vérification chez le provider", "server", srv.Name, "err", err)
		_ = a.Store.MarkProvider(ctx, srv.ID, "", err.Error())
		return ""
	}
	state := "present"
	if !exists {
		state = "gone"
		if before.ProviderState != "gone" {
			a.note(ctx, srv.OwnerID, "server-gone-"+srv.ID, "server.gone", "bad",
				srv.Name+" n'existe plus chez "+sec.Name+" : retirez-le de MyInfra.", srv.ID, srv.ClusterID)
		}
	}
	_ = a.Store.MarkProvider(ctx, srv.ID, state, "")
	return state
}

func (a *API) loadReach(ctx context.Context, servers []store.WatchedServer) {
	healths, err := a.Store.ServerHealths(ctx)
	if err != nil {
		return
	}
	clusters := map[string]string{}
	for _, srv := range servers {
		if watched(srv) {
			clusters[srv.ID] = srv.ClusterID
		}
	}
	a.reach.mu.Lock()
	a.reach.health, a.reach.cluster = healths, clusters
	a.reach.mu.Unlock()
}

// reachOf says how a server is seen: "gone" when its provider no longer has
// it, "lost" when it stopped answering, "" when all is well or unknown.
func (a *API) reachOf(serverID string) (string, store.ServerHealth) {
	a.reach.mu.Lock()
	defer a.reach.mu.Unlock()
	health, ok := a.reach.health[serverID]
	if !ok {
		return "", health
	}
	return reachState(health), health
}

func reachState(health store.ServerHealth) string {
	switch {
	case health.ProviderState == "gone":
		return "gone"
	case health.Fails >= lostAfter:
		return "lost"
	default:
		return ""
	}
}

// clusterLost tells whether no server of a cluster answers any more.
func (a *API) clusterLost(clusterID string) bool {
	a.reach.mu.Lock()
	defer a.reach.mu.Unlock()
	seen := false
	for id, cluster := range a.reach.cluster {
		if cluster != clusterID {
			continue
		}
		seen = true
		if reachState(a.reach.health[id]) == "" {
			return false
		}
	}
	return seen
}

// reachServer adds to a server what this copy sees of it.
func (a *API) reachServer(view *serverView) {
	state, health := a.reachOf(view.ID)
	view.Reach = state
	view.LostSince = health.LostSince
	view.SeenAt = health.SeenAt
	view.ProviderCheckedAt = health.ProviderCheckedAt
	view.ProviderError = health.ProviderError
}

// reachApp shows an application unreachable when what it runs on is.
func (a *API) reachApp(view *appView) {
	if view.Paused {
		return
	}
	switch {
	case view.ServerID != "":
		if state, _ := a.reachOf(view.ServerID); state != "" {
			view.RunStatus = "unreachable"
			view.RunDetail = "Le serveur " + view.ServerName + " ne répond plus."
			if state == "gone" {
				view.RunDetail = "Le serveur " + view.ServerName + " n'existe plus chez son provider."
			}
		}
	case view.ClusterID != "":
		if a.clusterLost(view.ClusterID) {
			view.RunStatus = "unreachable"
			view.RunDetail = "Aucun serveur du cluster " + view.ClusterName + " ne répond."
			return
		}
	}
	if view.RunStatus == "running" || view.Guarded {
		if host, silent := a.appSilent(view.ID); silent {
			view.RunStatus = "unreachable"
			view.RunDetail = host + " ne répond plus."
		}
	}
}

// checkServerNow probes a server and asks its provider at once, from its
// page.
func (a *API) checkServerNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := userID(ctx)
	row, err := a.Store.Server(ctx, owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	servers, err := a.Store.WatchedServers(ctx)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	for _, srv := range servers {
		if srv.ID != row.ID || !watched(srv) {
			continue
		}
		_ = a.Store.MarkReach(ctx, srv.ID, knocks(ctx, srv.Address))
		if srv.ProviderServerID != "" && srv.ProviderID != "" {
			healths, _ := a.Store.ServerHealths(ctx)
			a.checkProvider(ctx, srv, healths[srv.ID])
		}
	}
	a.loadReach(ctx, servers)
	writeJSON(w, http.StatusOK, a.toServer(row))
}

// machineGone asks the provider whether the machine of a server is already
// gone: its destruction then skips what would wait on it.
func (a *API) machineGone(ctx context.Context, owner, providerID, machineID string) (bool, string) {
	if providerID == "" || machineID == "" {
		return false, ""
	}
	sec, tok, err := a.openToken(ctx, owner, providerID)
	if err != nil {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	exists, err := provisionClient(a).Exists(ctx, sec.Provider, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, machineID)
	return err == nil && !exists, sec.Provider
}
