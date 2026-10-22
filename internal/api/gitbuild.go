package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"myinfra/internal/box"
	"myinfra/internal/build"
	"myinfra/internal/deploy"
	"myinfra/internal/imagefile"
	"myinfra/internal/store"
)

const gitSpecVar = "_myinfra_git_spec"

func (a *API) materializeGitApp(ctx context.Context, owner string, serverID, clusterID string, name string, source string, services []serviceBody) ([]serviceBody, string, map[string]string, error) {
	spec, err := build.Parse(source)
	if err != nil {
		return nil, "", nil, errString(err.Error())
	}
	host, err := a.buildHost(ctx, owner, serverID, clusterID)
	if err != nil {
		return nil, "", nil, err
	}
	token, err := a.gitToken(ctx, owner, spec.Repo)
	if err != nil {
		return nil, "", nil, err
	}
	ref, err := a.buildGitImage(ctx, owner, host, name, spec, token)
	if err != nil {
		return nil, "", nil, err
	}
	vars := map[string]string{gitSpecVar: source}
	if len(services) == 0 {
		services = []serviceBody{{Name: name, Image: ref, Published: true, Replicas: 1}}
	} else {
		for i := range services {
			if strings.TrimSpace(services[i].Image) == "" {
				services[i].Image = ref
			}
		}
	}
	return services, ref, vars, nil
}

func (a *API) buildHost(ctx context.Context, owner, serverID, clusterID string) (deploy.Host, error) {
	if serverID != "" {
		srv, err := a.Store.Server(ctx, owner, serverID)
		if err != nil {
			return deploy.Host{}, err
		}
		keys, err := a.Store.ServerKeyIDs(ctx, srv.ID)
		if err != nil {
			return deploy.Host{}, err
		}
		key := a.privateKey(ctx, owner, keys)
		if key == "" || srv.Address == "" {
			return deploy.Host{}, errString("Aucune clé SSH pour construire l'image sur cette machine.")
		}
		return deploy.Host{Address: srv.Address, PrivateKey: key}, nil
	}
	leaders, err := a.Store.ClusterLeaders(ctx)
	if err != nil {
		return deploy.Host{}, err
	}
	for _, leader := range leaders {
		if leader.ClusterID != clusterID {
			continue
		}
		keys, err := a.Store.ServerKeyIDs(ctx, leader.ServerID)
		if err != nil {
			continue
		}
		key := a.privateKey(ctx, owner, keys)
		if key != "" && leader.Address != "" {
			return deploy.Host{Address: leader.Address, PrivateKey: key}, nil
		}
	}
	return deploy.Host{}, errString("Aucun nœud du cluster n'est prêt pour construire l'image.")
}

func (a *API) gitToken(ctx context.Context, owner, repo string) (string, error) {
	host := strings.ToLower(repo)
	provider := ""
	if strings.Contains(host, "github.com") {
		provider = "github"
	} else if strings.Contains(host, "gitlab.com") {
		provider = "gitlab"
	} else {
		return "", nil
	}
	rows, err := a.Store.ListSecrets(ctx, owner)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Kind != "git_token" || row.Provider != provider {
			continue
		}
		sec, err := a.Store.Secret(ctx, owner, row.ID)
		if err != nil {
			continue
		}
		plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
		if err != nil {
			continue
		}
		var tok storedToken
		if json.Unmarshal(plain, &tok) == nil && tok.Token != "" {
			return tok.Token, nil
		}
	}
	return "", errString("Ajoutez un jeton " + provider + " dans Réglages → Git.")
}

func (a *API) buildGitImage(ctx context.Context, owner string, host deploy.Host, appName string, spec build.Spec, token string) (string, error) {
	tag := "myinfra-build-" + strings.ToLower(appName) + ":latest"
	clone := build.CloneURL(spec.Repo, token)
	script := build.RemoteScript(spec, clone, tag)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	pipeR, pipeW := io.Pipe()
	go func() {
		err := deploy.SSHPipe(ctx, host, script, pipeW)
		pipeW.CloseWithError(err)
	}()
	archive, err := imagefile.Read(pipeR, os.TempDir(), imageLimit)
	if err != nil {
		return "", errString("La construction Docker a échoué : " + err.Error())
	}
	defer archive.Close()
	ref, err := localRef(appName, "git", archive)
	if err != nil {
		return "", err
	}
	file, size, err := archive.WriteAs(ref, os.TempDir())
	if err != nil {
		return "", err
	}
	defer os.Remove(file)
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img := store.LocalImage{ID: newID(), OwnerID: owner, Ref: ref, Source: "git", SourceURL: spec.Repo,
		Size: size, Digest: archive.Digest, Platform: archive.Platform, Config: archive.Config}
	if _, err := a.Store.LocalImageByRef(ctx, ref); err == nil {
		forgetLoaded(ref)
		return ref, nil
	}
	if err := a.Store.SaveLocalImage(ctx, img, f); err != nil {
		return "", err
	}
	forgetLoaded(ref)
	return ref, nil
}

func (a *API) gitWebhook(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	secret := webhookSecret(r)
	app, err := a.Store.ApplicationByID(r.Context(), appID)
	if err != nil || !secretEqual(secret, gitWebhookSecret(a.Master, app.ID)) {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	if app.Variables[gitSpecVar] == "" {
		writeError(w, http.StatusBadRequest, "Cette application ne vient pas de Git.")
		return
	}
	spec, err := build.Parse(app.Variables[gitSpecVar])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	host, err := a.buildHost(r.Context(), app.OwnerID, app.ServerID, app.ClusterID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	token, err := a.gitToken(r.Context(), app.OwnerID, spec.Repo)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	ref, err := a.buildGitImage(r.Context(), app.OwnerID, host, app.Name, spec, token)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	images := map[string]string{}
	for _, svc := range app.Services {
		images[svc.Name] = ref
	}
	if err := a.Store.SetServiceImages(r.Context(), app.ID, images); err != nil {
		a.mapErr(w, err)
		return
	}
	_ = a.Store.SetRunStatus(r.Context(), app.ID, "preparing")
	writeJSON(w, http.StatusAccepted, map[string]string{"image": ref})
}

func gitWebhookSecret(master []byte, appID string) string {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("git-webhook:"))
	mac.Write([]byte(appID))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

func webhookSecret(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get("X-Myinfra-Webhook")); header != "" {
		return header
	}
	if rest, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if token := strings.TrimSpace(rest); token != "" {
			return token
		}
	}
	return r.PathValue("secret")
}

func secretEqual(got, want string) bool {
	if len(got) != len(want) || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (a *API) gitWebhookURL(appID string) string {
	if len(a.Master) == 0 {
		return ""
	}
	base := strings.TrimRight(a.PublicURL, "/")
	return base + "/api/webhooks/git/" + url.PathEscape(appID)
}
