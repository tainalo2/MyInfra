package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"myinfra/internal/dns"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"myinfra/internal/box"
	"myinfra/internal/catalog"
	"myinfra/internal/cert"
	"myinfra/internal/deploy"
	"myinfra/internal/provision"
	"myinfra/internal/store"
	"myinfra/internal/transfer"
)

type transferState struct {
	mu      sync.Mutex
	running bool
	stop    bool
	abort   context.CancelFunc
	status  string
	step    string
	url     string
	method  string
	err     string
	// wait is what the waiting step waits for: starting or certificate.
	wait string
	// cluster receives MyInfra during a transfer into pods.
	cluster string
	// drift is true while the name of the new copy points elsewhere; note
	// says why the waiting lasts.
	drift bool
	note  string
	// keep leaves this copy running as a synced copy once the transfer ends.
	keep bool
}

func (a *API) transferPreview(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("providerId")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un provider.")
		return
	}
	sec, _, err := a.transferAccount(r, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cat, err := a.offers(r, id, r.URL.Query().Get("location"), r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	canHost := true
	reason := ""
	writeJSON(w, http.StatusOK, map[string]any{
		"accountId": id, "provider": sec.Provider, "name": sec.Name,
		"canHost": canHost, "reason": reason,
		"catalog": cat,
	})
}

func (a *API) transferStatus(w http.ResponseWriter, r *http.Request) {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	status := a.transfer.status
	if status == "" {
		status = "idle"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"step":   a.transfer.step,
		"url":    a.transfer.url,
		"method": a.transfer.method,
		"error":  a.transfer.err,
		"wait":   a.transfer.wait,
		"note":   a.transfer.note,
		"keep":   a.transfer.keep,
	})
}

func (a *API) startTransfer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string `json:"providerId"`
		OfferID    string `json:"offerId"`
		Location   string `json:"location"`
		ProjectID  string `json:"projectId"`
		Period     string `json:"period"`
		StorageID  string `json:"storageId"`
		StorageGB  int    `json:"storageGb"`
		// Keep leaves this copy running, synced with the new one.
		Keep bool `json:"keep"`
	}
	if err := readJSON(w, r, &body); err != nil || body.ProviderID == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un provider.")
		return
	}
	a.transfer.mu.Lock()
	if a.transfer.running {
		a.transfer.mu.Unlock()
		writeError(w, http.StatusConflict, "Un transfert est déjà en cours.")
		return
	}
	a.transfer.running = true
	a.transfer.stop = false
	a.transfer.abort = nil
	a.transfer.mu.Unlock()
	refuse := func(status int, message string) {
		a.setTransfer(false, "idle", "", "", "")
		writeError(w, status, message)
	}

	sec, tok, err := a.transferAccount(r, body.ProviderID)
	if err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	if needsProject(sec.Provider) && strings.TrimSpace(body.ProjectID) == "" {
		refuse(http.StatusBadRequest, "Choisissez un projet.")
		return
	}
	cat, err := a.offers(r, body.ProviderID, body.Location, body.ProjectID)
	if err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	plan, err := cat.Plan(strings.TrimSpace(body.OfferID), body.Period, strings.TrimSpace(body.StorageID), body.StorageGB)
	if err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	plan.ProjectID = strings.TrimSpace(body.ProjectID)
	cat.Location = plan.Location
	offer := catalog.Offer{ID: plan.OfferID, Name: plan.Name}
	disk := rootGB(plan)
	if testCredentials(a.Verify.AllowTest, sec.Provider, tok) {
		refuse(http.StatusBadRequest, "Un jeton de test ne peut pas ouvrir de machine.")
		return
	}
	if devMaster(a.Master) {
		refuse(http.StatusBadRequest, "La clé de développement ne peut pas partir sur un serveur.")
		return
	}
	if strings.TrimSpace(a.Image) == "" {
		refuse(http.StatusBadRequest, "Cette copie n'a pas d'image publiée.")
		return
	}
	registryHost, registryUser, registryPassword := a.imageRegistry(r.Context(), userID(r.Context()))
	userData, token, err := a.startupConfig(registryHost, registryUser, registryPassword)
	if err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	owner := userID(r.Context())
	a.markTransferMethod("machine")
	a.setTransferKeep(body.Keep)
	a.setTransfer(true, "creating", "Ouverture de la machine", "", "")
	go a.runTransfer(owner, sec, tok, cat, offer, userData, token, disk, plan.ProjectID)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "creating", "step": "Ouverture de la machine"})
}

func (a *API) runTransfer(owner string, sec store.Secret, tok storedToken, cat catalog.Catalog, offer catalog.Offer, userData, token string, rootGB int, projectID string) {
	defer func() {
		if rec := recover(); rec != nil {
			a.setTransfer(false, "failed", "Une erreur est survenue.", a.transferURL(), "Une erreur est survenue.")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if a.armTransfer(cancel) {
		a.endCancelledTransfer("")
		return
	}
	defer a.disarmTransfer()
	client := provision.Client{
		HTTP: a.Verify.HTTP, HetznerBase: a.Verify.HetznerBase, DigitalOceanBase: a.Verify.DigitalOceanBase,
		ScalewayBase: a.Verify.ScalewayBase, OVHBase: a.Verify.OVHBase, InfomaniakBase: a.Verify.InfomaniakBase,
		UpCloudBase: a.Verify.UpCloudBase, VultrBase: a.Verify.VultrBase, AkamaiBase: a.Verify.AkamaiBase,
		ExoscaleBase: a.Verify.ExoscaleBase, GcoreBase: a.Verify.GcoreBase, KamateraBase: a.Verify.KamateraBase, IonosBase: a.Verify.IonosBase,
	}
	creds := catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}
	name := "myinfra-" + randomHex(3)
	machine, err := client.Create(ctx, sec.Provider, creds, provision.Spec{
		Name: name, Location: cat.Location, OfferID: offer.ID, OfferName: offer.Name, UserData: userData, RootGB: rootGB, ProjectID: projectID,
	})
	if a.transferFailed(err, "Ouverture de la machine", "", errText(err)) {
		return
	}
	kept := false
	defer func() {
		if !kept {
			_ = client.Destroy(context.Background(), sec.Provider, creds, machine.ID)
		}
	}()
	base := "http://" + machine.IPv4
	if a.transferFailed(nil, "", base, "") {
		return
	}
	a.setTransfer(true, "waiting", "La machine démarre", base, "")
	if err := waitHealth(ctx, base, a.setTransferWait); a.transferFailed(err, "La machine démarre", base, errText(err)) {
		return
	}
	if a.transferFailed(nil, "", base, "") {
		return
	}
	a.setTransfer(true, "copying", "Copie des données", base, "")
	snap, err := a.Store.ExportSnapshot(ctx)
	if a.transferFailed(err, "Copie des données", base, "La copie des données a échoué.") {
		return
	}
	snap.Installation = store.Installation{
		Provider: sec.Provider, AccountID: sec.ID, ServerID: machine.ID, PublicURL: base,
	}
	snap.Peers = a.peersForNewCopy()
	if err := pushSnapshot(ctx, base, token, snap); a.transferFailed(err, "Copie des données", base, errText(err)) {
		return
	}
	a.finishTransfer(ctx, client, owner, base, &kept)
}

func (a *API) startPodTransfer(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("id")
	a.transfer.mu.Lock()
	if a.transfer.running {
		a.transfer.mu.Unlock()
		writeError(w, http.StatusConflict, "Un transfert est déjà en cours.")
		return
	}
	a.transfer.running = true
	a.transfer.stop = false
	a.transfer.abort = nil
	a.transfer.mu.Unlock()
	refuse := func(status int, message string) {
		a.setTransfer(false, "idle", "", "", "")
		writeError(w, status, message)
	}
	if devMaster(a.Master) {
		refuse(http.StatusBadRequest, "La clé de développement ne peut pas partir sur un serveur.")
		return
	}
	if strings.TrimSpace(a.Image) == "" {
		refuse(http.StatusBadRequest, "Cette copie n'a pas d'image publiée.")
		return
	}
	var body struct {
		Zone string `json:"zone"`
		// Subdomain names the new copy, myinfra when left out: two copies
		// moving to the same zone each need their own name.
		Subdomain string `json:"subdomain"`
		// Keep leaves this copy running, synced with the new one.
		Keep bool `json:"keep"`
	}
	if err := readJSON(w, r, &body); err != nil {
		refuse(http.StatusBadRequest, "Choisissez un domaine.")
		return
	}
	owner := userID(r.Context())
	zone := strings.ToLower(strings.Trim(strings.TrimSpace(body.Zone), "."))
	if zone == "" {
		zones, err := a.zoneViews(r.Context(), owner)
		if err != nil || len(zones) != 1 {
			refuse(http.StatusBadRequest, "Choisissez un domaine.")
			return
		}
		zone = zones[0].Name
	}
	cluster, err := a.Store.Cluster(r.Context(), owner, clusterID)
	if err != nil {
		refuse(http.StatusNotFound, "Ce cluster est introuvable.")
		return
	}
	if cluster.Origin != "created" {
		refuse(http.StatusBadRequest, "Ce cluster importé ne peut pas recevoir MyInfra.")
		return
	}
	subdomain := strings.TrimSpace(body.Subdomain)
	if subdomain == "" {
		subdomain = "myinfra"
	}
	hostName, err := dns.Join(subdomain, zone)
	if err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	if cert.Zone(hostName, "") != zone {
		refuse(http.StatusBadRequest, "Choisissez un domaine.")
		return
	}
	taken, err := a.Store.HostnameTaken(r.Context(), hostName)
	if err != nil {
		refuse(http.StatusBadRequest, "Une erreur est survenue.")
		return
	}
	if taken {
		refuse(http.StatusConflict, "Ce nom est déjà utilisé.")
		return
	}
	host, err := a.clusterHost(r.Context(), owner, clusterID)
	if err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	// A MyInfra already in the cluster lives in the same namespace: a second
	// one would take over its configuration, and a failure would remove it.
	if present, err := podNamespaceExists(r.Context(), host); err != nil {
		refuse(http.StatusBadGateway, "Le cluster ne répond pas.")
		return
	} else if present {
		refuse(http.StatusConflict, "MyInfra tourne déjà dans ce cluster. Retirez-le d'abord, ou choisissez un autre cluster.")
		return
	}
	// A name that already answers elsewhere belongs to something else, often
	// another copy of MyInfra: taking it would fight over its record.
	if elsewhere := a.nameElsewhere(r.Context(), zone, hostName, clusterID); elsewhere != "" {
		a.setTransfer(false, "idle", "", "", "")
		writeCoded(w, http.StatusConflict, "transfer_name_elsewhere", "Le nom "+hostName+" pointe déjà vers "+elsewhere+". Choisissez un autre nom.", map[string]string{"name": hostName, "addresses": elsewhere})
		return
	}
	publicURL := "https://" + hostName
	a.markTransferMethod("pod")
	a.setTransferKeep(body.Keep)
	a.transfer.mu.Lock()
	a.transfer.cluster = clusterID
	a.transfer.mu.Unlock()
	a.setTransfer(true, "creating", "Préparation du pod", publicURL, "")
	go a.runPodTransfer(owner, clusterID, hostName, publicURL)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "creating", "step": "Préparation du pod", "url": publicURL})
}

func (a *API) runPodTransfer(owner, clusterID, hostName, publicURL string) {
	defer func() {
		if rec := recover(); rec != nil {
			a.setTransfer(false, "failed", "Une erreur est survenue.", publicURL, "Une erreur est survenue.")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if a.armTransfer(cancel) {
		a.endCancelledTransfer(publicURL)
		return
	}
	defer a.disarmTransfer()
	host, err := a.clusterHost(ctx, owner, clusterID)
	if err != nil {
		a.setTransfer(false, "failed", "Préparation du pod", publicURL, err.Error())
		return
	}
	zone := cert.Zone(hostName, "")
	account, err := a.accountForZone(ctx, owner, zone)
	if err != nil {
		a.setTransfer(false, "failed", "Publication du nom", publicURL, err.Error())
		return
	}
	address, err := a.Store.DestinationAddress(ctx, "cluster", "", clusterID)
	if err != nil || address == "" {
		a.setTransfer(false, "failed", "Publication du nom", publicURL, "La machine n'a pas encore d'adresse.")
		return
	}
	sec, tok, err := a.dnsToken(ctx, owner, account)
	if err != nil {
		a.setTransfer(false, "failed", "Publication du nom", publicURL, err.Error())
		return
	}
	if err := a.dnsClient().EnsureAddress(ctx, sec.Provider, credsOf(tok), zone, hostName, address); err != nil {
		a.setTransfer(false, "failed", "Publication du nom", publicURL, err.Error())
		return
	}
	password := randomHex(18)
	token := randomToken()
	registryHost, registryUser, registryPassword := a.imageRegistry(ctx, owner)
	manifest, err := transfer.PodManifest(transfer.PodSpec{
		Image: a.Image, MasterKey: base64.StdEncoding.EncodeToString(a.Master), DatabasePassword: password,
		BootstrapToken: token, PublicURL: publicURL, Host: hostName,
		RegistryHost: registryHost, RegistryUser: registryUser, RegistryPassword: registryPassword,
	})
	if err != nil {
		a.setTransfer(false, "failed", "Préparation du pod", publicURL, "Une erreur est survenue.")
		return
	}
	// Checked again just before: only a namespace this transfer creates is
	// removed when it fails.
	if present, err := podNamespaceExists(ctx, host); err != nil || present {
		a.setTransfer(false, "failed", "Préparation du pod", publicURL, "MyInfra tourne déjà dans ce cluster. Retirez-le d'abord, ou choisissez un autre cluster.")
		return
	}
	kept := false
	defer func() {
		if !kept {
			clean, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			_, _ = deploy.SSH(clean, host, "k3s kubectl delete ns "+transfer.PodNamespace+" --wait=false")
			_ = a.dnsClient().DropAddress(clean, sec.Provider, credsOf(tok), zone, hostName)
		}
	}()
	if a.transferFailed(nil, "", publicURL, "") {
		return
	}
	a.setTransfer(true, "waiting", "Démarrage du pod", publicURL, "")
	// Traefik asks a certificate for the name of MyInfra only with a resolver
	// for its zone: on a cluster without application, it has none yet.
	if err := a.podProxy(ctx, host, owner, clusterID, zone); a.transferFailed(err, "Démarrage du pod", publicURL, "Le proxy du cluster n'a pas pu être préparé.") {
		return
	}
	if _, err := deploy.SSH(ctx, host, deploy.KubectlApply(manifest)); a.transferFailed(err, "Démarrage du pod", publicURL, "Le pod n'a pas été créé.") {
		return
	}
	stopWatch := a.watchTransferName(ctx, host, sec.Provider, credsOf(tok), zone, hostName, address)
	err = waitHealth(ctx, publicURL, a.setTransferWait)
	stopWatch()
	if a.transferFailed(err, "Démarrage du pod", publicURL, errText(err)) {
		return
	}
	if a.transferFailed(nil, "", publicURL, "") {
		return
	}
	a.setTransfer(true, "copying", "Copie des données", publicURL, "")
	snap, err := a.Store.ExportSnapshot(ctx)
	if a.transferFailed(err, "Copie des données", publicURL, "La copie des données a échoué.") {
		return
	}
	snap.Installation = store.Installation{Method: "pod", ClusterID: clusterID, PublicURL: publicURL}
	snap.Peers = a.peersForNewCopy()
	if err := pushSnapshot(ctx, publicURL, token, snap); a.transferFailed(err, "Copie des données", publicURL, errText(err)) {
		return
	}
	a.finishTransfer(ctx, provision.Client{}, owner, publicURL, &kept)
}

func (a *API) finishTransfer(ctx context.Context, client provision.Client, owner, base string, kept *bool) {
	if a.transferFailed(nil, "", base, "") {
		return
	}
	if a.transferKeep() {
		// The new copy is online: nothing of it is cleaned up any more.
		*kept = true
		a.keepAsCopy(ctx, base)
		return
	}
	a.setTransfer(true, "destroying", "Arrêt de cette copie", base, "")
	current, err := a.Store.Installation(ctx)
	if err != nil {
		a.setTransfer(false, "failed", "Arrêt de cette copie", base, "La copie est en ligne. Cette copie est encore là.")
		*kept = true
		return
	}
	*kept = true
	if err := a.removePrevious(ctx, client, owner, current); err != nil {
		a.setTransfer(false, "failed", "Arrêt de cette copie", base, "La copie est en ligne sur "+base+". Cette copie est encore là.")
		return
	}
	a.setTransfer(true, "done", "MyInfra est en ligne", base, "")
	if a.Shutdown != nil {
		time.Sleep(5 * time.Second)
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = a.Shutdown(shutdownCtx)
	}
}

func (a *API) removePrevious(ctx context.Context, client provision.Client, owner string, current store.Installation) error {
	if current.ServerID != "" {
		if err := a.destroyInstallation(ctx, client, owner, current); err != nil {
			return err
		}
	}
	if current.Method == "pod" && current.ClusterID != "" {
		return a.removePod(ctx, owner, current)
	}
	return nil
}

func (a *API) removePod(ctx context.Context, owner string, current store.Installation) error {
	if parsed, err := url.Parse(current.PublicURL); err == nil && parsed.Hostname() != "" {
		zone := cert.Zone(parsed.Hostname(), "")
		if account, accErr := a.accountForZone(ctx, owner, zone); accErr == nil {
			if sec, tok, tokErr := a.dnsToken(ctx, owner, account); tokErr == nil {
				_ = a.dnsClient().DropAddress(ctx, sec.Provider, credsOf(tok), zone, parsed.Hostname())
			}
		}
	}
	host, err := a.clusterHost(ctx, owner, current.ClusterID)
	if err != nil {
		return err
	}
	_, err = deploy.SSH(ctx, host, "k3s kubectl delete ns "+transfer.PodNamespace+" --wait=false")
	return err
}

func (a *API) destroyInstallation(ctx context.Context, client provision.Client, owner string, current store.Installation) error {
	sec, err := a.Store.Secret(ctx, owner, current.AccountID)
	if err != nil {
		return err
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		return err
	}
	var tok storedToken
	if err := json.Unmarshal(plain, &tok); err != nil {
		return err
	}
	return client.Destroy(ctx, current.Provider, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, current.ServerID)
}

func (a *API) bootstrap(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(a.Bootstrap) == "" {
		http.NotFound(w, r)
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(got) > 256 || !tokenMatch(got, a.Bootstrap) {
		writeError(w, http.StatusUnauthorized, "Jeton refusé.")
		return
	}
	n, err := a.Store.CountUsers(r.Context())
	if err != nil || n > 0 {
		writeError(w, http.StatusConflict, "Déjà initialisé.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	var snap store.Snapshot
	if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
		writeError(w, http.StatusBadRequest, "Copie illisible.")
		return
	}
	if err := a.Store.ImportSnapshot(r.Context(), snap); err != nil {
		writeError(w, http.StatusBadRequest, "La copie des données a échoué.")
		return
	}
	if snap.Installation.PublicURL != "" {
		a.PublicURL = snap.Installation.PublicURL
		a.SecureCookie = strings.HasPrefix(a.PublicURL, "https://")
	}
	// The copy that moved this one stays beside it: each lists the other.
	for _, peer := range snap.Peers {
		if err := a.Store.AddPeer(r.Context(), peer); err != nil {
			slog.Info("copie ignorée", "err", err)
		}
	}
	a.Bootstrap = ""
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) transferAccount(r *http.Request, id string) (store.Secret, storedToken, error) {
	return a.openToken(r.Context(), userID(r.Context()), id)
}

func (a *API) imageRegistry(ctx context.Context, owner string) (string, string, string) {
	image := strings.TrimSpace(a.Image)
	host, _, ok := strings.Cut(image, "/")
	if !ok || !strings.Contains(host, ".scw.cloud") || strings.Contains(host, ":") {
		return "", "", ""
	}
	rows, err := a.Store.ListSecrets(ctx, owner)
	if err != nil {
		return "", "", ""
	}
	for _, sec := range rows {
		if sec.Kind != "provider_token" || sec.Provider != "scaleway" {
			continue
		}
		_, tok, err := a.openToken(ctx, owner, sec.ID)
		if err != nil || strings.TrimSpace(tok.Token) == "" {
			continue
		}
		return host, "nologin", strings.TrimSpace(tok.Token)
	}
	return "", "", ""
}

func (a *API) startupConfig(registryHost, registryUser, registryPassword string) (string, string, error) {
	password := randomToken()
	token := randomToken()
	config, err := transfer.CloudConfig(a.Image, base64.StdEncoding.EncodeToString(a.Master), password, token, registryHost, registryUser, registryPassword)
	if err != nil {
		return "", "", fmt.Errorf("Une erreur est survenue.")
	}
	return config, token, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (a *API) armTransfer(cancel context.CancelFunc) bool {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	a.transfer.abort = cancel
	return a.transfer.stop
}

func (a *API) disarmTransfer() {
	a.transfer.mu.Lock()
	a.transfer.abort = nil
	a.transfer.mu.Unlock()
}

func (a *API) transferCancelled() bool {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	return a.transfer.stop
}

func (a *API) stopTransfer() error {
	a.transfer.mu.Lock()
	if !a.transfer.running || a.transfer.status == "destroying" || a.transfer.status == "done" {
		a.transfer.mu.Unlock()
		return store.ErrStopped
	}
	a.transfer.stop = true
	abort := a.transfer.abort
	a.transfer.mu.Unlock()
	if abort != nil {
		abort()
	}
	return nil
}

func (a *API) endCancelledTransfer(url string) {
	if url == "" {
		url = a.transferURL()
	}
	a.setTransfer(false, "cancelled", "Annulé.", url, "")
}

func (a *API) transferFailed(err error, step, url, detail string) bool {
	if a.transferCancelled() {
		a.endCancelledTransfer(url)
		return true
	}
	if err != nil {
		a.setTransfer(false, "failed", step, url, detail)
		return true
	}
	return false
}

func (a *API) setTransfer(running bool, status, step, url, failure string) {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	a.transfer.running = running
	a.transfer.status = status
	a.transfer.step = step
	if url != "" {
		a.transfer.url = url
	}
	if status == "idle" {
		a.transfer.method = ""
		a.transfer.url = ""
	}
	if !running {
		a.transfer.stop = false
	}
	a.transfer.err = failure
	if status != "waiting" {
		a.transfer.wait = ""
	}
}

func (a *API) setTransferKeep(keep bool) {
	a.transfer.mu.Lock()
	a.transfer.keep = keep
	a.transfer.mu.Unlock()
}

func (a *API) transferKeep() bool {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	return a.transfer.keep
}

// peersForNewCopy is this copy's address for the new one to sync with, when
// this copy stays and the new one can reach it. A copy on a laptop is not
// reachable: it syncs by itself with the new one, both ways.
func (a *API) peersForNewCopy() []string {
	if !a.transferKeep() {
		return nil
	}
	parsed, err := url.Parse(a.PublicURL)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	host := parsed.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()) {
		return nil
	}
	return []string{strings.TrimRight(a.PublicURL, "/")}
}

// keepAsCopy ends a transfer that keeps this copy: it syncs with the new one,
// which drives the machines from now on, and starts again as a replica.
func (a *API) keepAsCopy(ctx context.Context, base string) {
	a.transfer.mu.Lock()
	method, cluster := a.transfer.method, a.transfer.cluster
	a.transfer.mu.Unlock()
	role := store.CopyRole{Replica: true, MainMethod: method, MainURL: base}
	if method == "pod" {
		role.MainClusterID = cluster
	}
	if err := a.Store.AddPeer(ctx, base); err != nil {
		a.setTransfer(false, "failed", "Arrêt de cette copie", base, "La copie est en ligne, mais elle n'a pas pu être ajoutée aux copies.")
		return
	}
	if err := a.Store.SetCopyRole(ctx, role); err != nil {
		a.setTransfer(false, "failed", "Arrêt de cette copie", base, "La copie est en ligne sur "+base+", mais cette copie pilote encore les machines.")
		return
	}
	a.setTransfer(true, "done", "MyInfra est en ligne", base, "")
	// Started again, this copy reads its new role and drives nothing.
	go func() {
		time.Sleep(5 * time.Second)
		if a.Restart != nil {
			a.Restart()
			return
		}
		if a.Shutdown != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = a.Shutdown(shutdownCtx)
		}
	}()
}

func (a *API) markTransferMethod(method string) {
	a.transfer.mu.Lock()
	a.transfer.method = method
	a.transfer.mu.Unlock()
}

func (a *API) transferURL() string {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	return a.transfer.url
}

func testCredentials(allow bool, provider string, tok storedToken) bool {
	if !allow {
		return false
	}
	switch provider {
	case "ovh":
		return strings.HasPrefix(tok.ApplicationKey, "test-") && strings.HasPrefix(tok.ApplicationSecret, "test-") && strings.HasPrefix(tok.ConsumerKey, "test-")
	case "infomaniak", "exoscale", "kamatera":
		return strings.HasPrefix(tok.ApplicationKey, "test-") && strings.HasPrefix(tok.ApplicationSecret, "test-")
	default:
		return strings.HasPrefix(tok.Token, "test-")
	}
}

func tokenMatch(got, want string) bool {
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func devMaster(key []byte) bool {
	sum := sha256.Sum256([]byte("myinfra-dev-master-key"))
	return subtle.ConstantTimeCompare(key, sum[:]) == 1
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// waitHealth waits for the new copy to answer over a trusted connection. It
// tells what it waits for: the copy itself, or, once the copy answers, the
// certificate of its address.
func waitHealth(ctx context.Context, base string, waiting func(string)) error {
	client := &http.Client{Timeout: 5 * time.Second}
	// Only to tell that the copy answers while its certificate is missing:
	// nothing is sent to it over this connection.
	loose := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	deadline := time.Now().Add(10 * time.Minute)
	target := strings.TrimRight(base, "/") + "/api/health"
	state := "starting"
	for {
		if healthy(ctx, client, target) {
			return nil
		}
		// A reading cut by the end of the wait says nothing new.
		if ctx.Err() == nil {
			state = "starting"
			if strings.HasPrefix(target, "https://") && healthy(ctx, loose, target) {
				state = "certificate"
			}
			if waiting != nil {
				waiting(state)
			}
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return waitFailure(state)
		}
		select {
		case <-ctx.Done():
			return waitFailure(state)
		case <-time.After(5 * time.Second):
		}
	}
}

func waitFailure(state string) error {
	if state == "certificate" {
		return fmt.Errorf("La nouvelle copie répond, mais son certificat HTTPS n'a pas été délivré.")
	}
	return fmt.Errorf("La nouvelle copie ne répond pas.")
}

func healthy(ctx context.Context, client *http.Client, target string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<10))
	res.Body.Close()
	return res.StatusCode < 300
}

// nameElsewhere lists the addresses a name already answers with outside the
// cluster, empty when the name is free or already on the cluster.
func (a *API) nameElsewhere(ctx context.Context, zone, host, clusterID string) string {
	lookup, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	found, err := dns.Announced(lookup, zone, host)
	if err != nil || len(found) == 0 {
		return ""
	}
	// A catch-all record of the zone answers for every free name too.
	if wildcard, err := dns.Announced(lookup, zone, "myinfra-probe-"+randomHex(6)+"."+zone); err == nil && sameAddresses(found, wildcard) {
		return ""
	}
	mine, err := a.Store.DestinationAddresses(ctx, "cluster", "", clusterID)
	if err != nil {
		return ""
	}
	var foreign []string
	for _, ip := range found {
		if !slices.Contains(mine, ip) {
			foreign = append(foreign, ip)
		}
	}
	return strings.Join(foreign, ", ")
}

func sameAddresses(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, ip := range a {
		if !slices.Contains(b, ip) {
			return false
		}
	}
	return true
}

// transferZone is the zone of MyInfra while it moves into pods of clusterID.
func (a *API) transferZone(clusterID string) string {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	if !a.transfer.running || a.transfer.method != "pod" || a.transfer.cluster != clusterID {
		return ""
	}
	return zoneOfURL(a.transfer.url)
}

// setTransferWait says what the transfer waits for, shown under its step. A
// name that points elsewhere outweighs what the copy answers.
func (a *API) setTransferWait(state string) {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	if a.transfer.drift && state != "" {
		state = "dns"
	}
	a.transfer.wait = state
}

func (a *API) setTransferNote(drift bool, note string) {
	a.transfer.mu.Lock()
	defer a.transfer.mu.Unlock()
	a.transfer.drift = drift
	a.transfer.note = note
	if drift {
		a.transfer.wait = "dns"
	}
}

// watchTransferName keeps the name of the new copy on its cluster while the
// copy waits for its certificate, and tells why Let's Encrypt gives none.
// Another copy of MyInfra may write the same name meanwhile: Let's Encrypt
// would then check a machine that is not the cluster. Once the name is put
// back, Traefik is asked again, since it does not retry by itself.
func (a *API) watchTransferName(ctx context.Context, host deploy.Host, provider string, creds dns.Creds, zone, name, address string) func() {
	watch, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		repaired := false
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watch.Done():
				return
			case <-ticker.C:
			}
			note := ""
			drift := false
			if found, err := dns.Announced(watch, zone, name); err == nil && (len(found) != 1 || found[0] != address) {
				drift = true
				note = "Le nom " + name + " pointe vers " + strings.Join(found, ", ") + " au lieu de " + address + " seulement : une autre copie de MyInfra l'a sans doute modifié. Il est republié."
				if err := a.dnsClient().EnsureAddress(watch, provider, creds, zone, name, address); err == nil {
					repaired = true
				}
			}
			if !drift {
				if out, err := deploy.SSH(watch, host, readCertificateErrors); err == nil {
					trouble, ok := parseCertificateErrors(out, []string{name})[name]
					if ok {
						note = trouble.Detail
					}
					// Asking again before Let's Encrypt allows it would only
					// push the date further.
					if ok && repaired && (trouble.Retry.IsZero() || time.Now().After(trouble.Retry)) {
						if _, err := deploy.SSH(watch, host, "k3s kubectl -n kube-system rollout restart deploy/traefik"); err == nil {
							repaired = false
						}
					}
				}
			}
			a.setTransferNote(drift, note)
		}
	}()
	return func() {
		stop()
		<-done
		a.setTransferNote(false, "")
	}
}

// podProxy gives Traefik the resolver of the zone of MyInfra, beside those of
// the applications of the cluster.
func (a *API) podProxy(ctx context.Context, host deploy.Host, owner, clusterID, zone string) error {
	apps, err := a.Store.ClusterApps(ctx, clusterID)
	if err != nil {
		return err
	}
	zones := append(a.clusterZones(ctx, clusterID, apps), zone)
	return a.applyProxy(ctx, host, store.ClusterLeader{ClusterID: clusterID, OwnerID: owner}, zones)
}

// podNamespaceExists tells whether a MyInfra already runs in the cluster.
func podNamespaceExists(ctx context.Context, host deploy.Host) (bool, error) {
	out, err := deploy.SSH(ctx, host, "k3s kubectl get ns "+transfer.PodNamespace+" -o name --ignore-not-found")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func pushSnapshot(ctx context.Context, base, token string, snap store.Snapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("La copie des données a échoué.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/bootstrap", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("La copie des données a échoué.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("La nouvelle copie ne répond pas.")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &payload) == nil && payload.Error != "" {
			return errors.New(payload.Error)
		}
		// Not an answer of MyInfra: the proxy, or a copy that was already set up.
		return fmt.Errorf("La nouvelle copie a refusé les données (HTTP %d).", res.StatusCode)
	}
	return nil
}
