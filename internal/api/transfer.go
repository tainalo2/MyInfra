package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	writeJSON(w, http.StatusOK, map[string]string{
		"status": status,
		"step":   a.transfer.step,
		"url":    a.transfer.url,
		"method": a.transfer.method,
		"error":  a.transfer.err,
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
	if err := waitHealth(ctx, base); a.transferFailed(err, "La machine démarre", base, errText(err)) {
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
	hostName := "myinfra." + zone
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
	if _, err := a.clusterHost(r.Context(), owner, clusterID); err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	publicURL := "https://" + hostName
	a.markTransferMethod("pod")
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
	if _, err := deploy.SSH(ctx, host, deploy.KubectlApply(manifest)); a.transferFailed(err, "Démarrage du pod", publicURL, "Le pod n'a pas été créé.") {
		return
	}
	if err := waitHealth(ctx, publicURL); a.transferFailed(err, "Démarrage du pod", publicURL, errText(err)) {
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
	if err := pushSnapshot(ctx, publicURL, token, snap); a.transferFailed(err, "Copie des données", publicURL, errText(err)) {
		return
	}
	a.finishTransfer(ctx, provision.Client{}, owner, publicURL, &kept)
}

func (a *API) finishTransfer(ctx context.Context, client provision.Client, owner, base string, kept *bool) {
	if a.transferFailed(nil, "", base, "") {
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

func waitHealth(ctx context.Context, base string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(10 * time.Minute)
	target := strings.TrimRight(base, "/") + "/api/health"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err == nil {
			res, err := client.Do(req)
			if err == nil {
				io.Copy(io.Discard, io.LimitReader(res.Body, 1<<10))
				res.Body.Close()
				if res.StatusCode < 300 {
					return nil
				}
			}
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("La nouvelle copie ne répond pas.")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("La nouvelle copie ne répond pas.")
		case <-time.After(5 * time.Second):
		}
	}
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
		return fmt.Errorf("La copie des données a échoué.")
	}
	return nil
}
