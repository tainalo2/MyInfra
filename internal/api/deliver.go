package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"myinfra/internal/cert"
	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
)

var (
	deliverPauseMu    sync.Mutex
	deliverPauseUntil = map[string]time.Time{}
)

func pauseDeliver(address string) {
	holdDeliver(address, 90*time.Second)
}

// holdDeliver keeps the reconciler from putting replicas and disks back
// while a node move is copying them.
func holdDeliver(address string, d time.Duration) {
	deliverPauseMu.Lock()
	deliverPauseUntil[address] = time.Now().Add(d)
	deliverPauseMu.Unlock()
}

func releaseDeliver(address string) {
	deliverPauseMu.Lock()
	delete(deliverPauseUntil, address)
	deliverPauseMu.Unlock()
}

func deliverPaused(address string) bool {
	deliverPauseMu.Lock()
	defer deliverPauseMu.Unlock()
	return time.Now().Before(deliverPauseUntil[address])
}

func (a *API) Deliver(ctx context.Context) {
	go a.resumeRollouts()
	a.deliverClusters(ctx)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.deliverClusters(context.Background())
		}
	}
}

func (a *API) deliverClusters(ctx context.Context) {
	if a.Replica {
		return
	}
	a.deliverMu.Lock()
	defer a.deliverMu.Unlock()
	leaders, err := a.Store.ClusterLeaders(ctx)
	if err == nil {
		for _, leader := range leaders {
			a.deliverCluster(ctx, leader)
		}
	}
	a.deliverAlone(ctx)
}

// reapplyEvery puts back, now and then, what someone may have changed by hand
// on the cluster. Between two of these, an unchanged manifest is not sent.
const reapplyEvery = 10 * time.Minute

// startupLimit is how long an application may stay in preparation before it
// is shown as failed.
const startupLimit = 15 * time.Minute

type appliedState struct {
	hash string
	at   time.Time
}

func (a *API) appliedRecently(key, hash string) bool {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	state, ok := a.applied[key]
	return ok && state.hash == hash && time.Since(state.at) < reapplyEvery
}

// rememberApplied records a manifest. changed says its content differs from
// the last one sent; first says nothing was known yet, after a restart.
func (a *API) rememberApplied(key, hash string) (changed, first bool) {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	if a.applied == nil {
		a.applied = map[string]appliedState{}
	}
	previous, ok := a.applied[key]
	a.applied[key] = appliedState{hash: hash, at: time.Now()}
	return !ok || previous.hash != hash, !ok
}

func (a *API) forgetApplied(prefix string) {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	for key := range a.applied {
		if strings.HasPrefix(key, prefix) {
			delete(a.applied, key)
		}
	}
}

func digest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (a *API) deliverCluster(ctx context.Context, leader store.ClusterLeader) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	keyIDs, err := a.Store.ServerKeyIDs(ctx, leader.ServerID)
	if err != nil {
		return
	}
	key := a.privateKey(ctx, leader.OwnerID, keyIDs)
	if key == "" {
		return
	}
	host := deploy.Host{Address: leader.Address, PrivateKey: key}
	if deliverPaused(leader.Address) {
		return
	}
	apps, err := a.Store.ClusterApps(ctx, leader.ClusterID)
	if err != nil {
		return
	}
	zones := a.clusterZones(ctx, leader.ClusterID, apps)
	changed := false
	if err := a.applyProxy(ctx, host, leader, zones); err != nil {
		slog.Info("proxy", "err", err)
		pauseDeliver(leader.Address)
		return
	}
	// The images MyInfra keeps are on every node before the pods ask for them.
	unloaded := a.loadClusterImages(ctx, leader, apps)
	// The store of the cluster holds the copies of the applications' data.
	keep := map[string]bool{manifest.StorageNamespace: true}
	var watch []store.ClusterApp
	for _, app := range apps {
		parts, ok := a.clusterServices(ctx, host, leader.OwnerID, leader.ClusterID, app)
		if !ok {
			keep[manifest.Namespace(app.ID)] = true
			continue
		}
		env, err := a.openVars(app.Env)
		if err != nil {
			slog.Info("variables", "app", app.ID, "err", "lecture impossible")
			_ = a.Store.SetRunStatus(ctx, app.ID, "failed")
			_ = a.Store.SetRunDetail(ctx, app.ID, "Les variables de l'application n'ont pas pu être déchiffrées.")
			keep[manifest.Namespace(app.ID)] = true
			continue
		}
		if detail := imageTrouble(app.Services, unloaded); detail != "" {
			_ = a.Store.SetRunStatus(ctx, app.ID, "failed")
			_ = a.Store.SetRunDetail(ctx, app.ID, detail)
			keep[manifest.Namespace(app.ID)] = true
			continue
		}
		spec := manifest.App{ID: app.ID, Paused: app.Paused, Hosts: app.Hosts, Zones: app.Zones, Env: env,
			Services: parts.Services, Databases: parts.Databases, Secrets: parts.Secrets,
			Pull: a.pullConfig(ctx, serviceImages(app.Services))}
		if err := a.attachShared(ctx, leader.OwnerID, app.ID, &spec); err != nil {
			slog.Info("secrets", "app", app.ID, "err", "lecture impossible")
			_ = a.Store.SetRunStatus(ctx, app.ID, "failed")
			_ = a.Store.SetRunDetail(ctx, app.ID, "Les secrets partagés de l'application n'ont pas pu être lus.")
			keep[manifest.Namespace(app.ID)] = true
			continue
		}
		if hosts, err := a.addresses(ctx, app.ID, publishedOf(app.Services)); err == nil {
			withAddress(&spec, hosts)
		}
		if refusal := manifest.Refusal(spec); refusal != "" {
			_ = a.Store.SetRunStatus(ctx, app.ID, "failed")
			_ = a.Store.SetRunDetail(ctx, app.ID, refusal)
			keep[manifest.Namespace(app.ID)] = true
			continue
		}
		keep[manifest.Namespace(app.ID)] = true
		raw := manifest.Render(spec)
		if !strings.HasSuffix(raw, "\n") {
			raw += "\n"
		}
		hash := digest(raw)
		appKey := "app:" + leader.ClusterID + ":" + app.ID
		if !a.appliedRecently(appKey, hash) {
			if _, err := deploy.SSH(ctx, host, deploy.KubectlApply(raw)); err != nil {
				_ = a.Store.SetRunStatus(ctx, app.ID, "failed")
				if !appRejected(err) {
					pauseDeliver(leader.Address)
					return
				}
				slog.Info("apply", "app", app.ID, "err", err)
				_ = a.Store.SetRunDetail(ctx, app.ID, "Kubernetes a refusé l'application : "+kubectlError(err.Error()))
				continue
			}
			changed = true
			if fresh, first := a.rememberApplied(appKey, hash); fresh {
				a.pruneRoutes(ctx, host, manifest.Namespace(app.ID), raw)
				a.pruneShared(ctx, host, manifest.Namespace(app.ID), raw)
				if !app.Paused {
					_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+manifest.Namespace(app.ID)+" delete deploy,svc paused --ignore-not-found --wait=false")
				}
				if app.RunStatus == "failed" {
					// What made the previous version fail no longer holds.
					_ = a.Store.SetRunDetail(ctx, app.ID, "")
				}
				if app.Paused {
					app.RunStatus = "paused"
				} else if !first || app.RunStatus != "running" {
					// A new version is only shown online once it answers.
					app.RunStatus = "preparing"
					a.markStarting(app.ID)
				}
				_ = a.Store.SetRunStatus(ctx, app.ID, app.RunStatus)
			}
		}
		a.advanceBackups(ctx, host, app)
		a.advanceDatabases(ctx, host, spec, app)
		if app.Paused {
			if app.RunStatus != "paused" {
				_ = a.Store.SetRunStatus(ctx, app.ID, "paused")
			}
			continue
		}
		if app.RunStatus != "running" {
			watch = append(watch, app)
		}
	}
	for _, app := range watch {
		if movingData(app) {
			// Stopped on purpose while its data moves: nothing to check.
			continue
		}
		a.checkHealth(ctx, host, leader.ClusterID, app)
	}
	if !changed && a.sweptRecently(leader.ClusterID) {
		return
	}
	a.forgetGoneNodes(ctx, host, leader)
	out, err := deploy.SSH(ctx, host, "k3s kubectl get ns -l myinfra.io/managed=true -o name")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimPrefix(strings.TrimSpace(line), "namespace/")
		if name == "" || keep[name] {
			continue
		}
		_, _ = deploy.SSH(ctx, host, "k3s kubectl delete ns "+name+" --wait=false")
	}
}

// forgetGoneNodes takes out of Kubernetes the nodes whose machine is no longer
// in the cluster, for instance a machine destroyed at the provider directly.
func (a *API) forgetGoneNodes(ctx context.Context, host deploy.Host, leader store.ClusterLeader) {
	rows, err := a.Store.ListServers(ctx, leader.OwnerID)
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, row := range rows {
		if row.ClusterID == leader.ClusterID {
			known[deploy.NodeName(row.Name)] = true
		}
	}
	if len(known) == 0 {
		return
	}
	out, err := deploy.SSH(ctx, host, "k3s kubectl get nodes -o jsonpath='{.items[*].metadata.name}'")
	if err != nil {
		return
	}
	for _, node := range strings.Fields(out) {
		if known[node] || !nodeNamePattern.MatchString(node) {
			continue
		}
		slog.Info("nœud oublié", "cluster", leader.ClusterID, "node", node)
		_, _ = deploy.SSH(ctx, host, "k3s kubectl delete node "+node+" --wait=false")
	}
}

var nodeNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

func (a *API) sweptRecently(clusterID string) bool {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	if a.swept == nil {
		a.swept = map[string]time.Time{}
	}
	if time.Since(a.swept[clusterID]) < reapplyEvery {
		return true
	}
	a.swept[clusterID] = time.Now()
	return false
}

func (a *API) markStarting(appID string) {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	if a.starting == nil {
		a.starting = map[string]time.Time{}
	}
	a.starting[appID] = time.Now()
}

func (a *API) startedAt(appID string) time.Time {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	return a.starting[appID]
}

func preparingSince(app store.ClusterApp) time.Time {
	if app.PreparingSince != nil {
		return *app.PreparingSince
	}
	return time.Time{}
}

// checkHealth decides whether an application is online: its pods are ready
// and its address answers in HTTPS with a valid certificate.
func (a *API) checkHealth(ctx context.Context, host deploy.Host, clusterID string, app store.ClusterApp) {
	namespace := manifest.Namespace(app.ID)
	out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" get deploy,statefulset -o jsonpath='{range .items[*]}{.status.readyReplicas}/{.spec.replicas} {end}' ; echo ; k3s kubectl -n "+namespace+" get pods -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.state.waiting.reason} {end}{end}'")
	if err != nil {
		return
	}
	lines := strings.SplitN(out, "\n", 2)
	waiting := ""
	if len(lines) == 2 {
		waiting = lines[1]
	}
	verdict := healthOf(lines[0], waiting)
	if verdict == "ready" {
		verdict = "preparing"
		if a.namesAnswer(ctx, app.ID) {
			verdict = "running"
			_ = a.Store.SetRunDetail(ctx, app.ID, "")
		} else if since := preparingSince(app); !since.IsZero() && time.Since(since) > certificateGrace {
			// The pods answer but the address does not: most often the
			// certificate. Say why instead of waiting without a word.
			a.explainCertificates(ctx, host, clusterID, app)
		}
	}
	if verdict == "preparing" {
		since := preparingSince(app)
		if since.IsZero() {
			a.markStarting(app.ID)
			since = a.startedAt(app.ID)
		}
		if !since.IsZero() && time.Since(since) > startupLimit {
			verdict = "failed"
			_ = a.Store.SetRunDetail(ctx, app.ID, fmt.Sprintf("L'application n'est pas prête après %s.", startupLimit))
		}
	} else if verdict == "failed" {
		if detail := stuckDetail(stuckReason(waiting)); detail != "" {
			_ = a.Store.SetRunDetail(ctx, app.ID, detail)
		}
	}
	if verdict != app.RunStatus {
		_ = a.Store.SetRunStatus(ctx, app.ID, verdict)
	}
}

// namesAnswer says whether every name of the application is published and
// answers in HTTPS. A name still waiting for its record counts as not yet.
func (a *API) namesAnswer(ctx context.Context, appID string) bool {
	published, err := a.Store.CustomNames(ctx, appID)
	if err != nil {
		return false
	}
	names, err := a.Store.AppNames(ctx, appID)
	if err != nil {
		return false
	}
	for _, name := range names {
		if !published[name.Hostname] {
			return false
		}
		// The zone's own servers say where the name points now; a resolver may
		// keep an older answer, such as a catch-all, for up to an hour.
		if !answers(ctx, name.Hostname, a.dnsClient().Announced(ctx, name.Zone, name.Hostname)) {
			return false
		}
	}
	return true
}

// stuckReason is the first waiting reason that keeps a container from starting.
func stuckReason(waiting string) string {
	for _, reason := range strings.Fields(waiting) {
		switch reason {
		case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "CreateContainerConfigError":
			return reason
		}
	}
	return ""
}

// stuckDetail says what a waiting reason means and where to look next.
func stuckDetail(reason string) string {
	switch reason {
	case "":
		return ""
	case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
		return "L'image ne peut pas être téléchargée (" + reason + ") : sa version n'existe pas, ou son registre la refuse."
	case "CreateContainerConfigError":
		return "Le conteneur ne peut pas être créé (" + reason + ") : une variable ou un secret qu'il attend manque."
	}
	return "Le conteneur redémarre en boucle (" + reason + ") : ses journaux disent pourquoi."
}

// healthOf reads the ready copies ("1/1 0/1 ") and the waiting reasons of the containers.
func healthOf(replicas, waiting string) string {
	if stuckReason(waiting) != "" {
		return "failed"
	}
	fields := strings.Fields(replicas)
	if len(fields) == 0 {
		return "preparing"
	}
	for _, field := range fields {
		ready, want, ok := strings.Cut(field, "/")
		if !ok || want == "" || want == "0" {
			continue
		}
		if ready != want {
			return "preparing"
		}
	}
	return "ready"
}

// answers says whether the address replies over HTTPS with a certificate the
// browser accepts. A proxy error (502, 503, 504) means the service is not up.
// answers asks the name in HTTPS, with its certificate checked as a browser
// does. With addresses, it connects to them instead of what the resolver of
// this machine says: the first one that takes the connection answers.
func answers(ctx context.Context, name string, addresses []string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+name+"/", nil)
	if err != nil {
		return false
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if len(addresses) > 0 {
		dialer := &net.Dialer{Timeout: 4 * time.Second}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			var last error
			for _, ip := range addresses {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return answered(resp.StatusCode, resp.Body)
}

// traefikNotFound is what the proxy itself sends when no route matches yet.
const traefikNotFound = "404 page not found"

// answered tells an answer of the application from an answer of the proxy:
// a proxy error, or the proxy's own page for a route it does not know yet.
func answered(status int, body io.Reader) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return false
	case http.StatusNotFound:
		head, _ := io.ReadAll(io.LimitReader(body, 64))
		return strings.TrimSpace(string(head)) != traefikNotFound
	}
	return true
}

// appRejected says the cluster answered and refused this application's
// manifest. The other applications can still be delivered.
func appRejected(err error) bool {
	var command *deploy.CommandError
	if !errors.As(err, &command) {
		return false
	}
	text := strings.ToLower(command.Text)
	for _, hint := range []string{"connection to the server", "unable to connect", "connection refused", "no route to host", "i/o timeout"} {
		if strings.Contains(text, hint) {
			return false
		}
	}
	return true
}

// kubectlError keeps the lines where kubectl says what it refused, the
// rest being what it applied without trouble.
func kubectlError(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Error") || strings.Contains(line, "is invalid") || strings.Contains(line, "error:") {
			kept = append(kept, line)
		}
	}
	out := strings.Join(kept, " ")
	if out == "" {
		out = lastLine(text)
	}
	if len(out) > 600 {
		out = out[:600] + "…"
	}
	return out
}

func (a *API) pruneShared(ctx context.Context, host deploy.Host, namespace, raw string) {
	out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" get secret -l myinfra.io/shared=true -o name")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if slash := strings.LastIndex(name, "/"); slash >= 0 {
			name = name[slash+1:]
		}
		if name == "" || !sharedObject.MatchString(name) || strings.Contains(raw, "kind: Secret\nmetadata:\n  name: "+name+"\n") {
			continue
		}
		_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" delete secret "+name+" --wait=false")
	}
}

// publicObject names a Service that publishes ports on the nodes.
var publicObject = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}-public$`)

// pruneRoutes removes the routes, and the Services that publish ports on
// the nodes, that the application no longer has.
func (a *API) pruneRoutes(ctx context.Context, host deploy.Host, namespace, raw string) {
	out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" get ingressroute -o name")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if slash := strings.LastIndex(name, "/"); slash >= 0 {
			name = name[slash+1:]
		}
		if name == "" || strings.Contains(raw, "kind: IngressRoute\nmetadata:\n  name: "+name+"\n") {
			continue
		}
		_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" delete ingressroute "+name+" --wait=false")
	}
	// A Service that publishes ports on the nodes holds them until it goes.
	out, err = deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" get svc -l myinfra.io/public=true -o name")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if slash := strings.LastIndex(name, "/"); slash >= 0 {
			name = name[slash+1:]
		}
		if name == "" || !publicObject.MatchString(name) || strings.Contains(raw, "kind: Service\nmetadata:\n  name: "+name+"\n") {
			continue
		}
		_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+namespace+" delete svc "+name+" --wait=false")
	}
}

// clusterZones are the DNS zones Traefik asks certificates for on a cluster:
// those of its applications, and that of MyInfra when MyInfra runs there —
// a cluster may hold MyInfra alone.
func (a *API) clusterZones(ctx context.Context, clusterID string, apps []store.ClusterApp) []string {
	var zones []string
	for _, app := range apps {
		for _, name := range app.Hosts {
			if zone := cert.Zone(name, app.Zones[name]); zone != "" {
				zones = append(zones, zone)
			}
		}
	}
	if inst, err := a.Store.Installation(ctx); err == nil && inst.Method == "pod" && inst.ClusterID == clusterID {
		if zone := zoneOfURL(inst.PublicURL); zone != "" {
			zones = append(zones, zone)
		}
	}
	if zone := a.transferZone(clusterID); zone != "" {
		zones = append(zones, zone)
	}
	return zones
}

func zoneOfURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return cert.Zone(parsed.Hostname(), "")
}

// applyProxy gives Traefik a certificate resolver for each zone, with the
// certificates MyInfra kept for them.
func (a *API) applyProxy(ctx context.Context, host deploy.Host, leader store.ClusterLeader, zones []string) error {
	if len(zones) == 0 {
		return nil
	}
	a.keepCertificates(ctx, host, leader, zones)
	raw := manifest.Proxy(zones)
	if !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	proxyKey := "proxy:" + leader.ClusterID
	if a.appliedRecently(proxyKey, digest(raw)) {
		return nil
	}
	if _, err := deploy.SSH(ctx, host, deploy.KubectlApply(raw)); err != nil {
		return err
	}
	_, _ = a.rememberApplied(proxyKey, digest(raw))
	return nil
}
