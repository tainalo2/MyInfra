package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/registry"
	"myinfra/internal/selfupdate"
	"myinfra/internal/store"
)

var (
	releaseMu sync.Mutex
	latest    selfupdate.Release
	updating  string // the version MyInfra is being updated to
)

type myinfraUpdateDetail struct {
	URL        string    `json:"url"`
	Notes      string    `json:"notes"`
	Published  time.Time `json:"published"`
	Prerelease bool      `json:"prerelease"`
}

func (a *API) checkMyInfra(ctx context.Context) error {
	if !selfupdate.Enabled() {
		return a.Store.SaveUpdates(ctx, "myinfra", nil, nil)
	}
	rel, err := selfupdate.Latest(ctx, a.Version)
	if err != nil {
		return err
	}
	releaseMu.Lock()
	latest = rel
	releaseMu.Unlock()
	var found []store.Update
	if selfupdate.Newer(rel.Version, a.Version) {
		raw, _ := json.Marshal(myinfraUpdateDetail{URL: rel.URL, Notes: clip(rel.Notes, 4000), Published: rel.Published, Prerelease: rel.Prerelease})
		level := "minor"
		if selfupdate.Valid(a.Version) {
			level = registry.Level("x:"+a.Version, "x:"+rel.Version)
		}
		found = append(found, store.Update{TargetID: "myinfra", Current: a.Version, Available: rel.Version, Level: level, Detail: raw})
	}
	return a.Store.SaveUpdates(ctx, "myinfra", found, nil)
}

// updateMethod is how this copy of MyInfra can replace itself: the local
// executable, the compose file of a machine it was moved to, or its pods.
func (a *API) updateMethod(ctx context.Context) string {
	if a.Local {
		return "local"
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		if inst, err := a.Store.Installation(ctx); err == nil && inst.Method == "pod" && inst.ClusterID != "" {
			return "pod"
		}
		return "manual"
	}
	if inst, err := a.Store.Installation(ctx); err == nil && inst.Method == "machine" && inst.ServerID != "" {
		return "compose"
	}
	return "manual"
}

type versionView struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
	Method    string `json:"method"`
	CanUpdate bool   `json:"canUpdate"`
	Reason    string `json:"reason,omitempty"`
	Updating  string `json:"updating,omitempty"`
	Image     string `json:"image,omitempty"`
}

func (a *API) versionView() versionView {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	releaseMu.Lock()
	rel, busy := latest, updating
	releaseMu.Unlock()
	view := versionView{Current: a.Version, Method: a.updateMethod(ctx), Updating: busy}
	if rel.Version != "" {
		view.Latest, view.URL = rel.Version, rel.URL
	}
	if a.Image != "" && rel.Version != "" {
		if next, err := registry.WithTag(a.Image, rel.Version); err == nil {
			view.Image = next
		}
	}
	view.CanUpdate, view.Reason = a.canUpdate(view, rel)
	return view
}

func (a *API) canUpdate(view versionView, rel selfupdate.Release) (bool, string) {
	switch {
	case rel.Version == "" || !selfupdate.Newer(rel.Version, a.Version):
		return false, ""
	case !selfupdate.Valid(a.Version):
		return false, "Version de développement : construisez la nouvelle version depuis les sources."
	case a.Replica:
		return false, "Cette copie de MyInfra ne pilote pas les machines."
	case view.Method == "manual":
		return false, "MyInfra a été lancé à la main : mettez à jour l'image de son fichier compose."
	case view.Method != "local" && a.Image == "":
		return false, "L'image de cette copie est inconnue."
	}
	return true, ""
}

func (a *API) getVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.versionView())
}

// updateMyInfra replaces this copy of MyInfra by the latest release.
func (a *API) updateMyInfra(w http.ResponseWriter, r *http.Request) {
	view := a.versionView()
	if !view.CanUpdate {
		reason := view.Reason
		if reason == "" {
			reason = "MyInfra est à jour."
		}
		writeError(w, http.StatusConflict, reason)
		return
	}
	releaseMu.Lock()
	if updating != "" {
		releaseMu.Unlock()
		writeError(w, http.StatusConflict, "La mise à jour de MyInfra est déjà en cours.")
		return
	}
	rel := latest
	updating = rel.Version
	releaseMu.Unlock()
	owner := userID(r.Context())
	var err error
	switch view.Method {
	case "local":
		err = a.updateLocal(r.Context(), rel)
	case "compose":
		err = a.updateCompose(r.Context(), owner, view.Image)
	case "pod":
		err = a.updatePods(r.Context(), owner, view.Image)
	}
	if err != nil {
		releaseMu.Lock()
		updating = ""
		releaseMu.Unlock()
		a.note(context.Background(), owner, newID(), "myinfra.update", "bad", "Mise à jour de MyInfra vers "+rel.Version+" impossible : "+err.Error(), "", "")
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.note(context.Background(), owner, newID(), "myinfra.update", "info", "Mise à jour de MyInfra vers "+rel.Version+" lancée.", "", "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "updating", "version": rel.Version})
}

// updateLocal swaps the executable and starts it again in place of this one.
func (a *API) updateLocal(ctx context.Context, rel selfupdate.Release) error {
	if a.Restart == nil {
		return fmt.Errorf("Cette copie ne peut pas redémarrer d'elle-même.")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("L'exécutable en cours est introuvable.")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if a.Postgres != "" {
		want, err := selfupdate.Postgres(ctx, rel)
		if err != nil {
			return err
		}
		if err := selfupdate.SamePostgres(rel, a.Postgres, want); err != nil {
			return err
		}
	}
	next, err := selfupdate.Download(ctx, rel, exe)
	if err != nil {
		return err
	}
	if err := selfupdate.Check(ctx, next, rel.Version); err != nil {
		_ = os.Remove(next)
		return err
	}
	if err := selfupdate.Swap(exe, next); err != nil {
		_ = os.Remove(next)
		return err
	}
	slog.Info("mise à jour", "de", a.Version, "vers", rel.Version)
	// The answer leaves before the server stops.
	go func() {
		time.Sleep(time.Second)
		a.Restart()
	}()
	return nil
}

// updateCompose changes the image of the machine MyInfra was moved to. The
// script runs on the machine itself, since this container is replaced: it
// puts the previous image back when the new one does not answer.
func (a *API) updateCompose(ctx context.Context, owner, image string) error {
	inst, err := a.Store.Installation(ctx)
	if err != nil {
		return err
	}
	host, err := a.serverHost(ctx, owner, inst.ServerID)
	if err != nil {
		return err
	}
	if !safeShell(image) || !safeShell(a.Image) {
		return fmt.Errorf("Image illisible.")
	}
	script := fmt.Sprintf(`set -u
cd /opt/myinfra
cp compose.yaml compose.yaml.previous
sed -i 's|image: %s$|image: %s|' compose.yaml
grep -q 'image: %s$' compose.yaml || exit 1
cat > update.sh <<'SCRIPT'
cd /opt/myinfra
if docker compose pull myinfra && docker compose up -d myinfra; then
  for i in $(seq 1 60); do
    if curl -fsS -m 3 http://127.0.0.1/api/health >/dev/null 2>&1; then exit 0; fi
    sleep 2
  done
fi
cp compose.yaml.previous compose.yaml
docker compose up -d myinfra
SCRIPT
nohup sh update.sh > update.log 2>&1 &
`, a.Image, image, image)
	if _, err := deploy.SSH(ctx, host, script); err != nil {
		return fmt.Errorf("La machine de MyInfra ne répond pas en SSH.")
	}
	return nil
}

// updatePods rolls the two copies of MyInfra in its cluster one after the
// other. The command runs on a node, since this pod is replaced: it rolls
// back when the new copies do not become ready.
func (a *API) updatePods(ctx context.Context, owner, image string) error {
	inst, err := a.Store.Installation(ctx)
	if err != nil {
		return err
	}
	host, err := a.clusterHost(ctx, owner, inst.ClusterID)
	if err != nil {
		return err
	}
	if !safeShell(image) {
		return fmt.Errorf("Image illisible.")
	}
	script := fmt.Sprintf(`k3s kubectl -n myinfra set image deployment/myinfra myinfra=%s >/dev/null || exit 1
nohup sh -c 'k3s kubectl -n myinfra rollout status deployment/myinfra --timeout=300s || k3s kubectl -n myinfra rollout undo deployment/myinfra' > /var/log/myinfra-update.log 2>&1 &
`, image)
	if _, err := deploy.SSH(ctx, host, script); err != nil {
		return fmt.Errorf("Le cluster de MyInfra ne répond pas en SSH.")
	}
	return nil
}

// serverHost is how MyInfra reaches a server over SSH.
func (a *API) serverHost(ctx context.Context, owner, id string) (deploy.Host, error) {
	server, err := a.Store.Server(ctx, owner, id)
	if err != nil {
		return deploy.Host{}, err
	}
	if server.Address == "" {
		return deploy.Host{}, fmt.Errorf("Ce serveur n'a pas d'adresse.")
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, server.ID)
	if err != nil {
		return deploy.Host{}, err
	}
	key := a.privateKey(ctx, owner, keyIDs)
	if key == "" {
		return deploy.Host{}, fmt.Errorf("Aucune clé SSH de ce serveur n'est disponible.")
	}
	return deploy.Host{Address: server.Address, PrivateKey: key}, nil
}

// safeShell keeps a value that could break the quoting of a script out of it.
func safeShell(value string) bool {
	return value != "" && !strings.ContainsAny(value, "'\"|\\$`\n\r ;&<>")
}
