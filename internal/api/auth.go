package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"myinfra/internal/auth"
	"myinfra/internal/deploy"
	"myinfra/internal/store"
	"myinfra/internal/ui"
)

type attemptGate struct {
	mu    sync.Mutex
	fails int
	until time.Time
}

func (g *attemptGate) begin() bool {
	g.mu.Lock()
	if !g.until.IsZero() && time.Now().Before(g.until) {
		g.mu.Unlock()
		return false
	}
	return true
}

func (g *attemptGate) end(success bool) {
	defer g.mu.Unlock()
	if success {
		g.fails = 0
		g.until = time.Time{}
		return
	}
	g.fails++
	if g.fails < 5 {
		return
	}
	delay := time.Duration(g.fails-4) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	g.until = time.Now().Add(delay)
}

// gateSet keeps one attempt counter per address, so that someone guessing
// the password does not lock the owner out from another address.
type gateSet struct {
	mu    sync.Mutex
	gates map[string]*attemptGate
}

func (s *gateSet) of(ip string) *attemptGate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gates == nil {
		s.gates = map[string]*attemptGate{}
	}
	if len(s.gates) > 4096 {
		now := time.Now()
		for key, gate := range s.gates {
			if gate.mu.TryLock() {
				idle := gate.until.IsZero() || now.After(gate.until.Add(10*time.Minute))
				gate.mu.Unlock()
				if idle {
					delete(s.gates, key)
				}
			}
		}
	}
	gate := s.gates[ip]
	if gate == nil {
		gate = &attemptGate{}
		s.gates[ip] = gate
	}
	return gate
}

// clientIP is the address of the browser. A forwarded address is only
// believed when the request comes from a proxy on this machine or network.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if ip := net.ParseIP(first); ip != nil {
				return ip.String()
			}
		}
	}
	if peer == nil {
		return host
	}
	return peer.String()
}

// announcedIP is an address the machine reports as its own.
func announcedIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return ""
	}
	return ip.String()
}

// publicClientIP is the caller when the proxy preserved a public address.
// A pod address is not where MyInfra can open SSH.
func publicClientIP(r *http.Request) string {
	ip := net.ParseIP(clientIP(r))
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return ""
	}
	return ip.String()
}

func routableIP(raw string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast()
}

// machineToken is the join secret: an Authorization header, or the legacy path.
func machineToken(r *http.Request) string {
	if rest, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if token := strings.TrimSpace(rest); token != "" {
			return token
		}
	}
	return strings.TrimSpace(r.PathValue("token"))
}

// machineCaller reports whether this request may read what the join token
// unlocks. Once the server address is an IP, only that address may continue.
func machineCaller(address, ip string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return true
	}
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	}
	if net.ParseIP(host) == nil {
		return true
	}
	return host == ip
}

const sessionCookie = "myinfra"

func (a *API) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/setup", a.setupState)
	mux.HandleFunc("POST /api/setup", a.setup)
	mux.HandleFunc("POST /api/session", a.login)
	mux.HandleFunc("POST /api/session/totp", a.loginTOTP)
	mux.HandleFunc("DELETE /api/session", a.logout)
	mux.HandleFunc("GET /api/session", a.auth(a.session))
	mux.HandleFunc("POST /api/onboarding", a.auth(a.steady(a.finishOnboarding)))
	mux.HandleFunc("POST /api/password", a.auth(a.steady(a.changePassword)))
	mux.HandleFunc("GET /api/users", a.requireRole(store.RoleAdmin)(a.listUsers))
	mux.HandleFunc("POST /api/users", a.requireRole(store.RoleAdmin)(a.steady(a.createUser)))
	mux.HandleFunc("DELETE /api/users/{id}", a.requireRole(store.RoleAdmin)(a.steady(a.deleteUser)))
	mux.HandleFunc("PATCH /api/account", a.auth(a.steady(a.updateAccount)))
	mux.HandleFunc("POST /api/account/totp", a.auth(a.steady(a.prepareTOTP)))
	mux.HandleFunc("PUT /api/account/totp", a.auth(a.steady(a.confirmTOTP)))
	mux.HandleFunc("POST /api/account/totp/confirm", a.auth(a.steady(a.confirmTOTP)))
	mux.HandleFunc("DELETE /api/account/totp", a.auth(a.steady(a.disableTOTP)))
	mux.HandleFunc("POST /api/webhooks/git/{id}", a.gitWebhook)
	mux.HandleFunc("POST /api/webhooks/git/{id}/{secret}", a.gitWebhook)

	mux.HandleFunc("GET /api/mcp/tokens", a.personOnly(a.listAPITokens))
	mux.HandleFunc("POST /api/mcp/tokens", a.personOnly(a.steady(a.createAPIToken)))
	mux.HandleFunc("DELETE /api/mcp/tokens/{id}", a.personOnly(a.steady(a.deleteAPIToken)))

	mux.HandleFunc("GET /api/costs", a.auth(a.listCosts))
	mux.HandleFunc("POST /api/offers/search", a.auth(a.offerSearch))
	mux.HandleFunc("GET /api/servers/{id}/delete/impact", a.auth(a.deleteImpact("server")))
	mux.HandleFunc("GET /api/clusters/{id}/delete/impact", a.auth(a.deleteImpact("cluster")))
	mux.HandleFunc("GET /api/applications/{id}/delete/impact", a.auth(a.deleteImpact("application")))

	mux.HandleFunc("GET /api/tools", a.auth(a.hostTools))
	mux.HandleFunc("POST /api/tools/{name}", a.deployAuth(a.installTool))

	mux.HandleFunc("GET /api/secrets", a.auth(a.listSecrets))
	mux.HandleFunc("POST /api/secrets/ssh-keys", a.adminAuth(a.steady(a.generateSSHKey)))
	mux.HandleFunc("POST /api/secrets/ssh-keys/import", a.adminAuth(a.steady(a.importSSHKey)))
	mux.HandleFunc("GET /api/secrets/{id}/private", a.adminAuth(a.downloadSSHKey))
	mux.HandleFunc("POST /api/secrets/tokens", a.adminAuth(a.steady(a.createToken)))
	mux.HandleFunc("PUT /api/secrets/{id}", a.adminAuth(a.steady(a.replaceToken)))
	mux.HandleFunc("POST /api/secrets/{id}/check", a.auth(a.probeSecret))
	mux.HandleFunc("GET /api/secrets/{id}/zones", a.auth(a.secretZones))
	mux.HandleFunc("GET /api/dns/zones", a.auth(a.listZones))
	mux.HandleFunc("GET /api/dns/records", a.auth(a.listDNSRecords))
	mux.HandleFunc("POST /api/dns/records/drop", a.adminAuth(a.steady(a.dropDNSName)))
	mux.HandleFunc("POST /api/dns/records/publish", a.adminAuth(a.steady(a.publishDNSName)))
	mux.HandleFunc("GET /api/dns/{host}/tofu", a.auth(a.dnsTofu))
	mux.HandleFunc("PUT /api/dns/{host}/tofu", a.adminAuth(a.steady(a.saveDNSTofu)))
	mux.HandleFunc("DELETE /api/secrets/{id}", a.adminAuth(a.steady(a.deleteSecret)))
	mux.HandleFunc("GET /api/app-secrets", a.auth(a.listAppSecrets))
	mux.HandleFunc("POST /api/app-secrets", a.adminAuth(a.steady(a.createAppSecret)))
	mux.HandleFunc("GET /api/app-secrets/{id}", a.auth(a.getAppSecret))
	mux.HandleFunc("PUT /api/app-secrets/{id}", a.adminAuth(a.steady(a.updateAppSecret)))
	mux.HandleFunc("DELETE /api/app-secrets/{id}", a.adminAuth(a.steady(a.deleteAppSecret)))

	mux.HandleFunc("GET /api/providers/{id}/offers", a.auth(a.providerOffers))
	mux.HandleFunc("GET /api/providers/{id}/projects", a.auth(a.providerProjects))
	mux.HandleFunc("GET /api/overview", a.auth(a.overview))
	mux.HandleFunc("GET /api/servers", a.auth(a.listServers))
	mux.HandleFunc("GET /api/events", a.auth(a.listEvents))
	mux.HandleFunc("POST /api/servers", a.deployAuth(a.steady(a.createServer)))
	mux.HandleFunc("GET /api/servers/{id}", a.auth(a.getServer))
	mux.HandleFunc("PUT /api/servers/{id}/place", a.deployAuth(a.serverSteady(a.setServerPlace)))
	mux.HandleFunc("PUT /api/servers/{id}/hosting", a.deployAuth(a.serverSteady(a.setServerHosting)))
	mux.HandleFunc("GET /api/servers/{id}/tofu", a.auth(a.serverTofu))
	mux.HandleFunc("PUT /api/servers/{id}/tofu", a.deployAuth(a.serverSteady(a.saveServerTofu)))
	mux.HandleFunc("GET /api/servers/{id}/runtime", a.auth(a.serverRuntime))
	mux.HandleFunc("GET /api/servers/{id}/terminal", a.deployAuth(a.serverTerminal))
	mux.HandleFunc("POST /api/servers/{id}/deploy", a.deployAuth(a.serverSteady(a.retryDeploy)))
	mux.HandleFunc("DELETE /api/servers/{id}", a.deployAuth(a.steady(a.deleteServer)))
	mux.HandleFunc("GET /api/deployments", a.auth(a.listDeployments))
	mux.HandleFunc("POST /api/deployments/cancel", a.deployAuth(a.cancelDeployment))
	mux.HandleFunc("GET /api/destructions", a.auth(a.listDestructions))
	mux.HandleFunc("GET /api/destructions/{id}", a.auth(a.getDestruction))
	mux.HandleFunc("POST /api/destructions/{id}/resume", a.deployAuth(a.resumeDestruction))
	mux.HandleFunc("POST /api/servers/{id}/cluster", a.deployAuth(a.serverSteady(a.attachCluster)))
	mux.HandleFunc("GET /api/clusters", a.auth(a.listClusters))
	mux.HandleFunc("POST /api/clusters", a.deployAuth(a.steady(a.createCluster)))
	mux.HandleFunc("DELETE /api/clusters/{id}", a.deployAuth(a.steady(a.deleteCluster)))
	mux.HandleFunc("POST /api/clusters/import", a.deployAuth(a.steady(a.importCluster)))
	mux.HandleFunc("GET /api/clusters/{id}/argo", a.auth(a.argoApps))
	mux.HandleFunc("GET /api/clusters/{id}/usage", a.auth(a.clusterUsage))

	mux.HandleFunc("GET /api/templates", a.auth(a.listTemplates))
	mux.HandleFunc("POST /api/templates", a.deployAuth(a.steady(a.createTemplate)))
	mux.HandleFunc("POST /api/templates/import", a.deployAuth(a.steady(a.importTemplate)))
	mux.HandleFunc("POST /api/templates/import-url", a.deployAuth(a.steady(a.importTemplateURL)))
	mux.HandleFunc("GET /api/starters", a.auth(a.listStarters))
	mux.HandleFunc("POST /api/starters/{id}", a.deployAuth(a.steady(a.useStarter)))
	mux.HandleFunc("POST /api/templates/from-cluster", a.deployAuth(a.steady(a.templateFromCluster)))
	mux.HandleFunc("GET /api/templates/{id}", a.auth(a.getTemplate))
	mux.HandleFunc("PUT /api/templates/{id}", a.deployAuth(a.steady(a.updateTemplate)))
	mux.HandleFunc("DELETE /api/templates/{id}", a.deployAuth(a.steady(a.deleteTemplate)))
	mux.HandleFunc("GET /api/templates/{id}/export", a.auth(a.exportTemplate))
	mux.HandleFunc("POST /api/templates/{id}/runs", a.deployAuth(a.steady(a.startTemplateRun)))
	mux.HandleFunc("GET /api/template-runs/{id}", a.auth(a.getTemplateRun))
	mux.HandleFunc("POST /api/template-runs/{id}/resume", a.deployAuth(a.steady(a.resumeTemplateRun)))

	mux.HandleFunc("GET /api/catalogue", a.auth(a.listCatalogue))
	mux.HandleFunc("GET /api/catalogue/{id}", a.auth(a.getCatalogue))
	mux.HandleFunc("GET /api/catalogue/{id}/versions", a.auth(a.catalogueVersions))
	mux.HandleFunc("POST /api/catalogue/{id}", a.deployAuth(a.steady(a.installCatalogue)))
	mux.HandleFunc("POST /api/catalogue/{id}/copy", a.deployAuth(a.steady(a.copyArticle)))
	mux.HandleFunc("GET /api/packages", a.auth(a.listPackages))
	mux.HandleFunc("POST /api/packages", a.deployAuth(a.steady(a.createPackage)))
	mux.HandleFunc("POST /api/packages/check", a.auth(a.checkPackage))
	mux.HandleFunc("POST /api/packages/format", a.auth(a.formatPackage))
	mux.HandleFunc("POST /api/packages/convert", a.deployAuth(a.convertPackage))
	mux.HandleFunc("POST /api/packages/import", a.deployAuth(a.steady(a.importPackage)))
	mux.HandleFunc("POST /api/packages/import-url", a.deployAuth(a.steady(a.importPackageURL)))
	mux.HandleFunc("GET /api/packages/{id}", a.auth(a.getPackage))
	mux.HandleFunc("PUT /api/packages/{id}", a.deployAuth(a.steady(a.updatePackage)))
	mux.HandleFunc("DELETE /api/packages/{id}", a.deployAuth(a.steady(a.deletePackage)))
	mux.HandleFunc("GET /api/packages/{id}/export", a.auth(a.exportPackage))
	mux.HandleFunc("POST /api/packages/{id}/install", a.deployAuth(a.steady(a.installPackage)))

	mux.HandleFunc("GET /api/applications", a.auth(a.listApplications))
	mux.HandleFunc("POST /api/applications", a.deployAuth(a.steady(a.createApplication)))
	mux.HandleFunc("GET /api/applications/{id}", a.auth(a.getApplication))
	mux.HandleFunc("POST /api/applications/{id}/pause", a.deployAuth(a.appSteady(a.pauseApplication)))
	mux.HandleFunc("GET /api/applications/{id}/redundancy", a.auth(a.redundancyOptions))
	mux.HandleFunc("POST /api/redundancy/preview", a.auth(a.redundancyPreview))
	mux.HandleFunc("POST /api/applications/{id}/redundancy", a.deployAuth(a.appSteady(a.setRedundancy)))
	mux.HandleFunc("PUT /api/applications/{id}/names", a.deployAuth(a.appSteady(a.setNames)))
	mux.HandleFunc("GET /api/images", a.auth(a.listLocalImages))
	mux.HandleFunc("POST /api/images", a.adminAuth(a.steady(a.uploadLocalImage)))
	mux.HandleFunc("POST /api/images/fetch", a.adminAuth(a.steady(a.fetchLocalImage)))
	mux.HandleFunc("DELETE /api/images/{id}", a.adminAuth(a.steady(a.deleteLocalImage)))
	mux.HandleFunc("GET /api/applications/{id}/variables/{name}", a.adminAuth(a.revealVariable))
	mux.HandleFunc("PUT /api/applications/{id}/variables", a.deployAuth(a.appSteady(a.setVariables)))
	mux.HandleFunc("GET /api/applications/{id}/runtime", a.auth(a.applicationRuntime))
	mux.HandleFunc("GET /api/applications/{id}/backups", a.auth(a.listApplicationBackups))
	mux.HandleFunc("GET /api/applications/{id}/backups/{service}", a.adminAuth(a.downloadApplicationBackup))
	mux.HandleFunc("POST /api/applications/{id}/backups/{service}", a.deployAuth(a.restoreApplicationBackup))
	mux.HandleFunc("GET /api/applications/{id}/releases", a.auth(a.applicationReleases))
	mux.HandleFunc("POST /api/applications/{id}/images", a.deployAuth(a.appSteady(a.replaceImages)))
	mux.HandleFunc("PUT /api/applications/{id}/services", a.deployAuth(a.appSteady(a.setServices)))
	mux.HandleFunc("GET /api/applications/{id}/package", a.auth(a.applicationPackage))
	mux.HandleFunc("POST /api/applications/{id}/package", a.deployAuth(a.steady(a.keepApplicationPackage)))
	mux.HandleFunc("GET /api/applications/{id}/pods/{pod}/logs", a.auth(a.podLogs))
	mux.HandleFunc("POST /api/applications/{id}/pods/{pod}/reset", a.deployAuth(a.appSteady(a.resetPod)))
	mux.HandleFunc("GET /api/applications/{id}/pods/{pod}/terminal", a.deployAuth(a.podTerminal))
	mux.HandleFunc("DELETE /api/applications/{id}", a.deployAuth(a.steady(a.deleteApplication)))

	mux.HandleFunc("GET /api/version", a.auth(a.getVersion))
	mux.HandleFunc("GET /api/activity", a.auth(a.activity))
	mux.HandleFunc("GET /api/updates", a.auth(a.listUpdates))
	mux.HandleFunc("POST /api/updates/check", a.auth(a.checkUpdatesNow))
	mux.HandleFunc("POST /api/updates/myinfra", a.deployAuth(a.steady(a.updateMyInfra)))
	mux.HandleFunc("POST /api/myinfra/stop", a.adminAuth(a.stopMyInfra))
	mux.HandleFunc("GET /api/applications/{id}/update/impact", a.auth(a.applicationUpdateImpact))
	mux.HandleFunc("POST /api/applications/{id}/update", a.deployAuth(a.appSteady(a.updateApplication)))

	mux.HandleFunc("GET /api/servers/{id}/update/impact", a.auth(a.serverUpdateImpact))
	mux.HandleFunc("POST /api/servers/{id}/update", a.deployAuth(a.steady(a.updateServer)))
	mux.HandleFunc("GET /api/clusters/{id}/update/impact", a.auth(a.clusterUpdateImpact))
	mux.HandleFunc("POST /api/clusters/{id}/update", a.deployAuth(a.steady(a.updateCluster)))
	mux.HandleFunc("GET /api/update-jobs", a.auth(a.listUpdateJobs))
	mux.HandleFunc("GET /api/update-jobs/{id}", a.auth(a.getUpdateJob))
	mux.HandleFunc("POST /api/update-jobs/{id}/resume", a.deployAuth(a.steady(a.resumeUpdateJob)))
	mux.HandleFunc("POST /api/update-jobs/{id}/cancel", a.deployAuth(a.cancelUpdateJob))

	mux.HandleFunc("GET /api/transfer", a.auth(a.transferStatus))
	mux.HandleFunc("GET /api/transfer/preview", a.auth(a.transferPreview))
	mux.HandleFunc("POST /api/transfer", a.adminDrive(a.startTransfer))
	mux.HandleFunc("POST /api/clusters/{id}/transfer", a.adminDrive(a.startPodTransfer))
	mux.HandleFunc("POST /api/clusters/{id}/transfer/clean", a.adminDrive(a.cleanPodTransfer))
	mux.HandleFunc("POST /api/bootstrap", a.bootstrap)
	mux.HandleFunc("GET /api/sync", a.auth(a.syncStatus))
	mux.HandleFunc("POST /api/sync/peers", a.requireRole(store.RoleAdmin)(a.steady(a.addSyncPeer)))
	mux.HandleFunc("DELETE /api/sync/peers", a.requireRole(store.RoleAdmin)(a.steady(a.deleteSyncPeer)))
	mux.HandleFunc("GET /api/sync/state", a.syncState)
	mux.HandleFunc("POST /api/sync/snapshot", a.syncSnapshot)
	mux.HandleFunc("POST /api/sync/yield", a.syncYield)
	mux.HandleFunc("POST /api/sync/lead", a.syncLead)
	mux.HandleFunc("POST /api/copies/lead", a.requireRole(store.RoleAdmin)(a.takeLead))
	mux.HandleFunc("POST /api/servers/{id}/check", a.auth(a.checkServerNow))
	mux.HandleFunc("POST /api/copies/handover", a.requireRole(store.RoleAdmin)(a.steady(a.handOver)))

	mux.HandleFunc("POST /api/join", a.join)
	mux.HandleFunc("GET /join", a.joinScript)
	mux.HandleFunc("GET /join/{token}", a.joinScript)
	mux.HandleFunc("GET /agent/sync", a.agentSync)
	mux.HandleFunc("GET /agent/{token}/sync", a.agentSync)
	mux.HandleFunc("POST /api/agent", a.agentReport)
	mux.HandleFunc("POST /api/agent/{token}", a.agentReport)

	// The MCP server calls the routes above through this same router.
	mcp := a.mcpServer(mux)
	mux.HandleFunc("POST /mcp", mcp)
	mux.HandleFunc("GET /mcp", mcp)
	mux.HandleFunc("DELETE /mcp", mcp)
	a.router = mux

	deploy.UseHostKeys(
		func(address string) (string, error) { return a.Store.SSHHostKey(context.Background(), address) },
		func(address, key string) error { return a.Store.SaveSSHHostKey(context.Background(), address, key) },
	)
	dist, err := fs.Sub(ui.Dist, "dist")
	if err == nil {
		if _, err = fs.Stat(dist, "index.html"); err != nil {
			// Built by go build alone: ./build puts the interface in.
			mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Interface absente de cet exécutable : construisez-le avec ./build.", http.StatusServiceUnavailable)
			})
		}
	}
	if err == nil {
		files := http.FileServer(http.FS(dist))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name != "" {
				if info, statErr := fs.Stat(dist, name); statErr == nil && !info.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
			}
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Path != "/api/health" && foreignBrowser(r) {
				writeError(w, http.StatusForbidden, "Origine refusée.")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func foreignBrowser(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return true
	}
	return !sameHost(parsed.Host, r.Host)
}

func sameHost(left, right string) bool {
	leftHost, _, err := net.SplitHostPort(left)
	if err != nil {
		leftHost = left
	}
	rightHost, _, err := net.SplitHostPort(right)
	if err != nil {
		rightHost = right
	}
	return strings.EqualFold(leftHost, rightHost)
}

func (a *API) setupState(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.CountUsers(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needed": n == 0, "users": n})
}

func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.CountUsers(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "Le mot de passe est déjà choisi.")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if len(body.Password) < 8 || len(body.Password) > 72 {
		writeError(w, http.StatusBadRequest, "Le mot de passe doit contenir entre 8 et 72 caractères.")
		return
	}
	gate := a.authGates.of(clientIP(r))
	if !gate.begin() {
		writeError(w, http.StatusTooManyRequests, "Trop de tentatives. Réessayez dans un instant.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	gate.end(err == nil)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	if err := a.Store.CreateFirstUser(r.Context(), id, string(hash)); err != nil {
		if errors.Is(err, store.ErrSetupDone) {
			writeError(w, http.StatusConflict, "Le mot de passe est déjà choisi.")
			return
		}
		a.mapErr(w, err)
		return
	}
	a.startSession(w, r, id)
}

func (a *API) issueLoginChallenge(ctx context.Context, userID string) (string, error) {
	token := randomToken()
	err := a.Store.CreateLoginChallenge(ctx, hashToken(token), userID, time.Now().Add(5*time.Minute))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *API) takeLoginChallenge(ctx context.Context, token string) (string, bool) {
	if strings.TrimSpace(token) == "" {
		return "", false
	}
	userID, err := a.Store.TakeLoginChallenge(ctx, hashToken(token))
	return userID, err == nil
}

func (a *API) accountForLogin(ctx context.Context, login string) (store.UserAccount, error) {
	if strings.TrimSpace(login) != "" {
		account, err := a.Store.UserByLogin(ctx, login)
		if err != nil {
			return store.UserAccount{}, err
		}
		return account, nil
	}
	id, _, err := a.Store.User(ctx)
	if err != nil {
		return store.UserAccount{}, err
	}
	return a.Store.UserAccount(ctx, id)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	account, err := a.accountForLogin(r.Context(), body.Login)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if strings.TrimSpace(body.Login) == "" {
				writeError(w, http.StatusConflict, "Choisissez d'abord un mot de passe.")
				return
			}
			writeError(w, http.StatusUnauthorized, "Identifiant ou mot de passe incorrect.")
			return
		}
		a.mapErr(w, err)
		return
	}
	gate := a.authGates.of(clientIP(r))
	if !gate.begin() {
		writeError(w, http.StatusTooManyRequests, "Trop de tentatives. Réessayez dans un instant.")
		return
	}
	match := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(body.Password)) == nil
	gate.end(match)
	if !match {
		writeError(w, http.StatusUnauthorized, "Identifiant ou mot de passe incorrect.")
		return
	}
	if account.TOTPSecret != "" {
		challenge, err := a.issueLoginChallenge(r.Context(), account.ID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"totpRequired": true,
			"challenge":    challenge,
		})
		return
	}
	a.startSession(w, r, account.ID)
}

func (a *API) loginTOTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Challenge string `json:"challenge"`
		TOTP      string `json:"totp"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	userID, ok := a.takeLoginChallenge(r.Context(), strings.TrimSpace(body.Challenge))
	if !ok {
		writeError(w, http.StatusUnauthorized, "Cette vérification a expiré. Reconnectez-vous.")
		return
	}
	account, err := a.Store.UserAccount(r.Context(), userID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if account.TOTPSecret == "" {
		writeError(w, http.StatusBadRequest, "La double authentification n'est pas active sur ce compte.")
		return
	}
	gate := a.authGates.of(clientIP(r))
	if !gate.begin() {
		writeError(w, http.StatusTooManyRequests, "Trop de tentatives. Réessayez dans un instant.")
		return
	}
	match := auth.Validate(account.TOTPSecret, body.TOTP)
	gate.end(match)
	if !match {
		writeError(w, http.StatusUnauthorized, "Code à deux facteurs incorrect.")
		return
	}
	a.startSession(w, r, account.ID)
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if len(body.Password) < 8 || len(body.Password) > 72 {
		writeError(w, http.StatusBadRequest, "Le mot de passe doit contenir entre 8 et 72 caractères.")
		return
	}
	id, hash, err := a.Store.User(r.Context())
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if id != userID(r.Context()) {
		writeError(w, http.StatusUnauthorized, "Connexion requise.")
		return
	}
	gate := a.authGates.of(clientIP(r))
	if !gate.begin() {
		writeError(w, http.StatusTooManyRequests, "Trop de tentatives. Réessayez dans un instant.")
		return
	}
	match := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Current)) == nil
	gate.end(match)
	if !match {
		writeError(w, http.StatusUnauthorized, "Mot de passe actuel incorrect.")
		return
	}
	if body.Current == body.Password {
		writeError(w, http.StatusBadRequest, "Choisissez un mot de passe différent.")
		return
	}
	next, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.UpdatePassword(r.Context(), id, string(next)); err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.DeleteUserSessions(r.Context(), id); err != nil {
		a.mapErr(w, err)
		return
	}
	a.startSession(w, r, id)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_ = a.Store.DeleteSession(r.Context(), hashToken(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.SecureCookie,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	actor := userID(r.Context())
	done, err := a.Store.Onboarded(r.Context(), actor)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if !done {
		owner, err := a.Store.InfrastructureOwner(r.Context())
		if err == nil && owner != actor {
			done, _ = a.Store.Onboarded(r.Context(), owner)
		}
	}
	account, err := a.Store.UserAccount(r.Context(), actor)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"start": !done,
		"role":  account.Role,
		"login": account.Login,
		"totp":  account.TOTPSecret != "",
	})
}

func (a *API) finishOnboarding(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.FinishOnboarding(r.Context(), userID(r.Context())); err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) startSession(w http.ResponseWriter, r *http.Request, userID string) {
	raw := randomToken()
	expires := time.Now().Add(30 * 24 * time.Hour)
	if err := a.Store.CreateSession(r.Context(), hashToken(raw), userID, expires); err != nil {
		a.mapErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.SecureCookie,
		MaxAge:   30 * 24 * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var userID string
		if bearer, ok := bearerToken(r); ok {
			token, err := a.Store.UserByAPIToken(ctx, hashToken(bearer))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "Jeton d'accès invalide ou expiré.")
				return
			}
			userID = token.UserID
			ctx = withToken(ctx, token)
		} else {
			cookie, err := r.Cookie(sessionCookie)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "Connexion requise.")
				return
			}
			userID, err = a.Store.UserBySession(ctx, hashToken(cookie.Value))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "Connexion requise.")
				return
			}
		}
		owner, err := a.Store.InfrastructureOwner(ctx)
		if err != nil {
			owner = userID
		}
		ctx = withOwner(withUser(ctx, userID), owner)
		next(w, r.WithContext(ctx))
	}
}

const replicaRefused = "Cette copie est une réplique : elle ne pilote pas les machines. Faites-le depuis la copie principale, ou prenez la main dans Copies."

const replicaAlone = "Cette copie est une réplique, et aucune copie principale ne la synchronise : la demande ne serait jamais exécutée. Ajoutez la copie principale dans Copies, ou relancez cette copie sans MYINFRA_REPLICA=1."

// alone tells whether a replica records work that no main copy will carry
// out: it pulls from no peer, or sync is off with the development key.
func (a *API) alone(ctx context.Context) bool {
	if !a.Replica {
		return false
	}
	peers, err := a.Store.ListPeers(ctx)
	if err != nil {
		return false
	}
	return lonelyReplica(a.Replica, devMaster(a.Master), len(peers))
}

func lonelyReplica(replica, devKey bool, peers int) bool {
	return replica && (devKey || peers == 0)
}

// drives refuses, on a replica, the actions that act on machines right away.
// The rows a replica writes reach the main copy by sync, which drives them.
func (a *API) drives(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Replica {
			writeError(w, http.StatusConflict, replicaRefused)
			return
		}
		next(w, r)
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func newID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}

func cleanName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return "", false
	}
	return name, true
}
