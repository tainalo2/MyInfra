package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"myinfra/internal/agent"
	"myinfra/internal/box"
	"myinfra/internal/imagefile"
	"myinfra/internal/registry"
	"myinfra/internal/sshk"
)

// A registry login lets MyInfra read the versions of private images and the
// servers pull them: a pull secret in each namespace of a cluster, docker login
// on a machine alone. Public images need none.

const registryKind = "registry_token"

// registryLoginCheck is registry.Check. A test replaces it so a login can be
// accepted without calling the registry.
var registryLoginCheck = registry.Check

// registryHosts are the registries known by name; any other is given by address.
var registryHosts = map[string]string{
	"registry-dockerhub": "docker.io",
	"registry-ghcr":      "ghcr.io",
	"registry-gitlab":    "registry.gitlab.com",
	"registry-quay":      "quay.io",
}

var registryAddress = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?(?::[0-9]{1,5})?$`)

// registryHostOf reads the address a person gave: a host, with a port maybe,
// pasted with or without https:// and a path.
func registryHostOf(provider, address string) (string, error) {
	if host, ok := registryHosts[provider]; ok {
		return host, nil
	}
	if provider != "registry-custom" {
		return "", errString("Registre inconnu.")
	}
	address = strings.ToLower(strings.TrimSpace(address))
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		address = parsed.Host
	}
	address, _, _ = strings.Cut(address, "/")
	if !registryAddress.MatchString(address) || !strings.ContainsAny(address, ".:") {
		return "", errString("Indiquez l'adresse du registre, par exemple registry.exemple.fr.")
	}
	return address, nil
}

// registryLoginParts reads the public half stored for a registry login:
// identifiant@adresse. The password is not in it.
func registryLoginParts(keyID string) (user, host string) {
	user, host, ok := strings.Cut(keyID, "@")
	if !ok || user == "" || host == "" {
		return "", ""
	}
	return user, host
}

// checkRegistry tries the login before it is kept.
func (a *API) checkRegistry(ctx context.Context, body tokenBody) (storedToken, string, error) {
	host, err := registryHostOf(body.Provider, body.ApplicationKey)
	if err != nil {
		return storedToken{}, "", err
	}
	user := strings.TrimSpace(body.KeyID)
	secret := strings.TrimSpace(body.Token)
	if user == "" || secret == "" {
		return storedToken{}, "", errString("Indiquez l'identifiant et le mot de passe ou jeton.")
	}
	if err := registryLoginCheck(ctx, host, user, secret); err != nil {
		return storedToken{}, "", err
	}
	forgetRegistryLogins()
	return storedToken{Token: secret, KeyID: user, ApplicationKey: host}, sshk.Hint(secret), nil
}

type registryLogin struct {
	User     string
	Password string
}

var (
	registryMu     sync.Mutex
	registryCache  map[string]registryLogin
	registryCached time.Time
)

func forgetRegistryLogins() {
	registryMu.Lock()
	registryCache = nil
	registryMu.Unlock()
}

// registryLogins are the logins held, by registry host, read again each minute.
func (a *API) registryLogins(ctx context.Context) map[string]registryLogin {
	registryMu.Lock()
	defer registryMu.Unlock()
	if registryCache != nil && time.Since(registryCached) < time.Minute {
		return registryCache
	}
	out := map[string]registryLogin{}
	rows, err := a.Store.SecretsOfKind(ctx, registryKind)
	if err != nil {
		slog.Info("registres", "err", err)
		return out
	}
	for _, row := range rows {
		plain, err := a.Box.Open(box.Sealed{Ciphertext: row.Ciphertext, Nonce: row.Nonce, WrappedDEK: row.WrappedDEK})
		if err != nil {
			continue
		}
		var tok storedToken
		if json.Unmarshal(plain, &tok) != nil || tok.ApplicationKey == "" {
			continue
		}
		out[tok.ApplicationKey] = registryLogin{User: tok.KeyID, Password: tok.Token}
	}
	registryCache, registryCached = out, time.Now()
	return out
}

// RegistryLogin answers registry.Credentials.
func (a *API) RegistryLogin(host string) (string, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	login, ok := a.registryLogins(ctx)[host]
	return login.User, login.Password, ok
}

// imageHost is the registry an image comes from, docker.io for a bare name.
func imageHost(image string) string {
	if imagefile.IsLocal(image) {
		return ""
	}
	ref, err := registry.Parse(image)
	if err != nil {
		return ""
	}
	return ref.Host
}

// pullConfig is the docker config JSON for the private registries of images.
func (a *API) pullConfig(ctx context.Context, images []string) string {
	logins := a.registryLogins(ctx)
	auths := map[string]map[string]string{}
	for _, image := range images {
		host := imageHost(image)
		login, ok := logins[host]
		if !ok {
			continue
		}
		key := host
		if host == "docker.io" {
			// The name kubelet and Docker look Docker Hub up by.
			key = "https://index.docker.io/v1/"
		}
		auths[key] = map[string]string{
			"username": login.User,
			"password": login.Password,
			"auth":     base64.StdEncoding.EncodeToString([]byte(login.User + ":" + login.Password)),
		}
	}
	if len(auths) == 0 {
		return ""
	}
	raw, err := json.Marshal(map[string]any{"auths": auths})
	if err != nil {
		return ""
	}
	return string(raw)
}

// agentLogin is the docker login a machine alone needs for an image.
func (a *API) agentLogin(ctx context.Context, image string) *agent.Login {
	host := imageHost(image)
	login, ok := a.registryLogins(ctx)[host]
	if !ok {
		return nil
	}
	return &agent.Login{Host: host, User: login.User, Password: login.Password}
}
