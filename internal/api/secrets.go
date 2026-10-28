package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"myinfra/internal/box"
	"myinfra/internal/catalog"
	"myinfra/internal/sshk"
	"myinfra/internal/store"
	"myinfra/internal/verify"
)

type secretView struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Provider    string `json:"provider,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"publicKey,omitempty"`
	Hint        string `json:"hint,omitempty"`
	KeyID       string `json:"keyId,omitempty"`
	HasPrivate  bool   `json:"hasPrivate"`
	CreatedAt   string `json:"createdAt"`
}

func toSecretView(sec store.Secret) secretView {
	return secretView{
		ID: sec.ID, Kind: sec.Kind, Name: sec.Name, Provider: sec.Provider,
		Fingerprint: sec.Fingerprint, PublicKey: sec.PublicKey, Hint: sec.Hint, KeyID: sec.KeyID,
		HasPrivate: sec.HasPrivate, CreatedAt: sec.CreatedAt.Format("2006-01-02"),
	}
}

func (a *API) listSecrets(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListSecrets(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]secretView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSecretView(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) generateSSHKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom à la clé.")
		return
	}
	pair, err := sshk.Generate()
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed, err := a.seal([]byte(pair.Private))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed.ID = newID()
	sealed.OwnerID = ownerID(r.Context())
	sealed.Kind = "ssh_key"
	sealed.Name = name
	sealed.Fingerprint = pair.Fingerprint
	sealed.PublicKey = pair.Public
	sealed.HasPrivate = true
	if err := a.Store.InsertSecret(r.Context(), sealed); err != nil {
		a.mapErr(w, err)
		return
	}
	saved, err := a.Store.Secret(r.Context(), sealed.OwnerID, sealed.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSecretView(saved))
}

func (a *API) importSSHKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
		// PrivateKey is optional. With it, MyInfra opens SSH itself on a
		// machine that already trusts this key.
		PrivateKey string `json:"privateKey"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom à la clé.")
		return
	}
	var pair sshk.Pair
	if strings.TrimSpace(body.PrivateKey) != "" {
		parsed, err := sshk.ParsePrivate(body.PrivateKey)
		if errors.Is(err, sshk.ErrPassphrase) {
			writeError(w, http.StatusBadRequest, "Cette clé privée est protégée par une phrase de passe : MyInfra ne pourrait pas l'ouvrir seul.")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "Clé privée illisible.")
			return
		}
		pair = parsed
	}
	if strings.TrimSpace(body.PublicKey) != "" {
		public, fingerprint, err := sshk.ParsePublic(body.PublicKey)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Clé publique illisible.")
			return
		}
		if pair.Private != "" && fingerprint != pair.Fingerprint {
			writeError(w, http.StatusBadRequest, "La clé publique ne correspond pas à la clé privée.")
			return
		}
		pair.Public, pair.Fingerprint = public, fingerprint
	}
	if pair.Public == "" {
		writeError(w, http.StatusBadRequest, "Clé publique illisible.")
		return
	}
	secret := []byte("{}")
	if pair.Private != "" {
		secret = []byte(pair.Private)
	}
	sealed, err := a.seal(secret)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed.ID = newID()
	sealed.OwnerID = ownerID(r.Context())
	sealed.Kind = "ssh_key"
	sealed.Name = name
	sealed.Fingerprint = pair.Fingerprint
	sealed.PublicKey = pair.Public
	sealed.HasPrivate = pair.Private != ""
	if err := a.Store.InsertSecret(r.Context(), sealed); err != nil {
		a.mapErr(w, err)
		return
	}
	saved, err := a.Store.Secret(r.Context(), sealed.OwnerID, sealed.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSecretView(saved))
}

func (a *API) downloadSSHKey(w http.ResponseWriter, r *http.Request) {
	sec, err := a.Store.Secret(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if !sec.HasPrivate {
		writeError(w, http.StatusNotFound, "Cette clé a été importée sans partie privée.")
		return
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"privateKey": string(plain)})
}

func (a *API) deleteSecret(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	sec, err := a.Store.Secret(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if sec.Kind == "provider_token" {
		n, err := a.Store.CountProviderServers(r.Context(), owner, sec.ID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if n > 0 {
			a.mapErr(w, store.ErrProviderBusy)
			return
		}
	}
	if err := a.Store.DeleteSecret(r.Context(), owner, sec.ID); err != nil {
		a.mapErr(w, err)
		return
	}
	forgetZones(owner, sec.ID)
	if sec.Kind == registryKind {
		forgetRegistryLogins()
	}
	w.WriteHeader(http.StatusNoContent)
}

type tokenBody struct {
	Name              string `json:"name"`
	Kind              string `json:"kind"`
	Provider          string `json:"provider"`
	Token             string `json:"token"`
	ApplicationKey    string `json:"applicationKey"`
	ApplicationSecret string `json:"applicationSecret"`
	ConsumerKey       string `json:"consumerKey"`
	ProjectID         string `json:"projectId"`
	KeyID             string `json:"keyId"`
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	var body tokenBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au jeton.")
		return
	}
	if body.Kind != "provider_token" && body.Kind != "dns_token" && body.Kind != "git_token" && body.Kind != registryKind {
		writeError(w, http.StatusBadRequest, "Type de jeton inconnu.")
		return
	}
	payload, hint, err := a.checkToken(r, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed, err := a.seal(raw)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed.ID = newID()
	sealed.OwnerID = ownerID(r.Context())
	sealed.Kind = body.Kind
	sealed.Name = name
	sealed.Provider = body.Provider
	sealed.Hint = hint
	sealed.KeyID = publicKeyID(body.Provider, payload)
	if err := a.Store.InsertSecret(r.Context(), sealed); err != nil {
		a.mapErr(w, err)
		return
	}
	saved, err := a.Store.Secret(r.Context(), sealed.OwnerID, sealed.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSecretView(saved))
}

type storedToken struct {
	Token             string `json:"token,omitempty"`
	ApplicationKey    string `json:"applicationKey,omitempty"`
	ApplicationSecret string `json:"applicationSecret,omitempty"`
	ConsumerKey       string `json:"consumerKey,omitempty"`
	ProjectID         string `json:"projectId,omitempty"`
	KeyID             string `json:"keyId,omitempty"`
}

func (a *API) checkToken(r *http.Request, body tokenBody) (storedToken, string, error) {
	ctx := r.Context()
	if body.Kind == registryKind {
		return a.checkRegistry(ctx, body)
	}
	switch body.Provider {
	case "hetzner":
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton API.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "scaleway":
		token := strings.TrimSpace(body.Token)
		projectID := strings.TrimSpace(body.ProjectID)
		if token == "" {
			return storedToken{}, "", errString("Collez la clé secrète.")
		}
		if strings.HasPrefix(token, "SCW") {
			return storedToken{}, "", errString("Collez la clé secrète. L'access key, qui commence par SCW, ne sert pas ici.")
		}
		if projectID == "" {
			return storedToken{}, "", errString("Indiquez le projet.")
		}
		keyID := strings.TrimSpace(body.KeyID)
		if !strings.HasPrefix(keyID, "SCW") {
			return storedToken{}, "", errString("Indiquez l'access key. Elle commence par SCW.")
		}
		tok := storedToken{Token: token, ProjectID: projectID, KeyID: keyID}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(token), nil
	case "cloudflare":
		if body.Kind != "dns_token" {
			return storedToken{}, "", errString("Cloudflare sert au DNS.")
		}
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "github":
		if body.Kind != "git_token" {
			return storedToken{}, "", errString("GitHub sert à Git.")
		}
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "gitlab":
		if body.Kind != "git_token" {
			return storedToken{}, "", errString("GitLab sert à Git.")
		}
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "digitalocean":
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton API.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "infomaniak":
		id := strings.TrimSpace(body.ApplicationKey)
		secret := strings.TrimSpace(body.ApplicationSecret)
		if id == "" || secret == "" {
			return storedToken{}, "", errString("L'identifiant et le secret Infomaniak sont requis.")
		}
		tok := storedToken{ApplicationKey: id, ApplicationSecret: secret}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(secret), nil
	case "exoscale", "kamatera":
		key := strings.TrimSpace(body.ApplicationKey)
		secret := strings.TrimSpace(body.ApplicationSecret)
		if key == "" || secret == "" {
			return storedToken{}, "", errString("L'identifiant et le secret sont requis.")
		}
		tok := storedToken{ApplicationKey: key, ApplicationSecret: secret}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(secret), nil
	case "gcore", "ionos":
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton API.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "upcloud":
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton API.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "vultr":
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton API.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "akamai":
		if strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Collez le jeton API.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "ovh":
		creds := verify.OVH{
			ApplicationKey:    strings.TrimSpace(body.ApplicationKey),
			ApplicationSecret: strings.TrimSpace(body.ApplicationSecret),
			ConsumerKey:       strings.TrimSpace(body.ConsumerKey),
		}
		if creds.ApplicationKey == "" || creds.ApplicationSecret == "" || creds.ConsumerKey == "" {
			return storedToken{}, "", errString("Les trois clés OVH sont requises.")
		}
		tok := storedToken{
			ApplicationKey: creds.ApplicationKey, ApplicationSecret: creds.ApplicationSecret, ConsumerKey: creds.ConsumerKey,
		}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(creds.ConsumerKey), nil
	case "proxmox":
		if strings.TrimSpace(body.ApplicationKey) == "" || strings.TrimSpace(body.Token) == "" {
			return storedToken{}, "", errString("Indiquez l'adresse et le jeton.")
		}
		tok := storedToken{Token: strings.TrimSpace(body.Token), ApplicationKey: strings.TrimSpace(body.ApplicationKey)}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.Token), nil
	case "openstack":
		if strings.TrimSpace(body.Token) == "" || strings.TrimSpace(body.ApplicationKey) == "" || strings.TrimSpace(body.ApplicationSecret) == "" {
			return storedToken{}, "", errString("Indiquez l'adresse, l'identifiant et le secret.")
		}
		tok := storedToken{
			Token: strings.TrimSpace(body.Token), ApplicationKey: strings.TrimSpace(body.ApplicationKey),
			ApplicationSecret: strings.TrimSpace(body.ApplicationSecret), ProjectID: strings.TrimSpace(body.ProjectID),
		}
		if err := a.probeProvider(ctx, body.Provider, tok); err != nil {
			return storedToken{}, "", err
		}
		return tok, sshk.Hint(body.ApplicationSecret), nil
	default:
		return storedToken{}, "", errString("Provider inconnu.")
	}
}

func publicKeyID(provider string, tok storedToken) string {
	if strings.HasPrefix(provider, "registry-") {
		// The host and the user say which login it is, without the password.
		return tok.KeyID + "@" + tok.ApplicationKey
	}
	if tok.KeyID != "" {
		return tok.KeyID
	}
	if provider == "ovh" || provider == "infomaniak" || provider == "exoscale" || provider == "kamatera" {
		return tok.ApplicationKey
	}
	return ""
}

func (a *API) replaceToken(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	sec, err := a.Store.Secret(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	switch sec.Kind {
	case "provider_token", "dns_token", "git_token", registryKind:
	default:
		writeError(w, http.StatusBadRequest, "Ce compte ne se remplace pas ainsi.")
		return
	}
	var body tokenBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	body.Kind = sec.Kind
	body.Provider = sec.Provider
	payload, hint, err := a.checkToken(r, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed, err := a.seal(raw)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	sealed.ID = sec.ID
	sealed.OwnerID = owner
	sealed.Hint = hint
	sealed.KeyID = publicKeyID(sec.Provider, payload)
	if err := a.Store.UpdateSecret(r.Context(), sealed); err != nil {
		a.mapErr(w, err)
		return
	}
	if sec.Kind == registryKind {
		forgetRegistryLogins()
	}
	forgetZones(owner, sec.ID)
	saved, err := a.Store.Secret(r.Context(), owner, sec.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSecretView(saved))
}

func (a *API) probeSecret(w http.ResponseWriter, r *http.Request) {
	sec, err := a.Store.Secret(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if sec.Kind != "provider_token" && sec.Kind != "dns_token" {
		writeError(w, http.StatusBadRequest, "Ce compte n'est pas un provider.")
		return
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		writeError(w, http.StatusBadRequest, "Jeton illisible.")
		return
	}
	var tok storedToken
	if err := json.Unmarshal(plain, &tok); err != nil {
		writeError(w, http.StatusBadRequest, "Jeton illisible.")
		return
	}
	var probeErr error
	if sec.Kind == "dns_token" {
		_, probeErr = a.dnsClient().Zones(r.Context(), sec.Provider, credsOf(tok))
	} else {
		probeErr = a.probeProvider(r.Context(), sec.Provider, tok)
	}
	if probeErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": probeErr.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) probeProvider(ctx context.Context, provider string, tok storedToken) error {
	switch provider {
	case "hetzner":
		return a.Verify.Hetzner(ctx, tok.Token)
	case "scaleway":
		if err := a.Verify.Scaleway(ctx, tok.Token); err != nil {
			return err
		}
		return a.Verify.ScalewayProject(ctx, tok.Token, tok.ProjectID)
	case "cloudflare":
		return a.Verify.Cloudflare(ctx, tok.Token)
	case "github":
		return a.Verify.GitHub(ctx, tok.Token)
	case "gitlab":
		return a.Verify.GitLab(ctx, tok.Token)
	case "digitalocean":
		return a.Verify.DigitalOcean(ctx, tok.Token)
	case "infomaniak":
		return a.Verify.Infomaniak(ctx, tok.ApplicationKey, tok.ApplicationSecret)
	case "upcloud":
		return a.Verify.UpCloud(ctx, tok.Token)
	case "vultr":
		return a.Verify.Vultr(ctx, tok.Token)
	case "akamai":
		return a.Verify.Akamai(ctx, tok.Token)
	case "exoscale":
		return a.Verify.Exoscale(ctx, tok.ApplicationKey, tok.ApplicationSecret)
	case "gcore":
		return a.Verify.Gcore(ctx, tok.Token)
	case "kamatera":
		return a.Verify.Kamatera(ctx, tok.ApplicationKey, tok.ApplicationSecret)
	case "ionos":
		return a.Verify.Ionos(ctx, tok.Token)
	case "proxmox", "openstack":
		_, err := catalog.Client{HTTP: a.Verify.HTTP, AllowTest: a.Verify.AllowTest}.List(ctx, provider, catalog.Creds{
			Token: tok.Token, ProjectID: tok.ProjectID, ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret,
		}, "")
		return err
	case "ovh":
		return a.Verify.OVH(ctx, verify.OVH{
			ApplicationKey:    tok.ApplicationKey,
			ApplicationSecret: tok.ApplicationSecret,
			ConsumerKey:       tok.ConsumerKey,
		})
	default:
		return errString("Provider inconnu.")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
