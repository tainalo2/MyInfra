package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"myinfra/internal/argo"
	"myinfra/internal/box"
)

func (a *API) importCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		Kubeconfig string `json:"kubeconfig"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom au cluster.")
		return
	}
	kubeconfig := strings.TrimSpace(body.Kubeconfig)
	if kubeconfig == "" || len(kubeconfig) > 100000 {
		writeError(w, http.StatusBadRequest, "Le kubeconfig est illisible.")
		return
	}
	sealed, err := a.Box.Seal([]byte(kubeconfig))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	id := newID()
	owner := userID(r.Context())
	if err := a.Store.ImportCluster(r.Context(), owner, id, name, sealed.Ciphertext, sealed.Nonce, sealed.WrappedDEK); err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), owner, "opened-cluster-"+id, "cluster.opened", "info", "Cluster importé.", "", id)
	writeJSON(w, http.StatusCreated, clusterView{ID: id, Name: name, Origin: "imported"})
}

func (a *API) argoApps(w http.ResponseWriter, r *http.Request) {
	origin, ciphertext, nonce, wrapped, err := a.Store.ClusterKube(r.Context(), userID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if origin != "imported" {
		writeError(w, http.StatusConflict, "Ce cluster est livré par MyInfra.")
		return
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: ciphertext, Nonce: nonce, WrappedDEK: wrapped})
	if err != nil || len(plain) == 0 {
		writeError(w, http.StatusBadRequest, "Le kubeconfig est illisible.")
		return
	}
	binary, err := exec.LookPath("kubectl")
	if err != nil {
		writeError(w, http.StatusBadRequest, "kubectl est absent du plan de contrôle.")
		return
	}
	dir, err := os.MkdirTemp("", "myinfra-kube-")
	if err != nil {
		a.mapErr(w, err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, plain, 0o600); err != nil {
		a.mapErr(w, err)
		return
	}
	out, err := exec.CommandContext(r.Context(), binary, "--kubeconfig", path, "get", "applications.argoproj.io", "-A", "-o", "json").Output()
	if err != nil {
		writeError(w, http.StatusBadGateway, "Argo CD n'a pas répondu.")
		return
	}
	apps, err := argo.Parse(out)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Argo CD n'a pas répondu.")
		return
	}
	if apps == nil {
		apps = []argo.Application{}
	}
	writeJSON(w, http.StatusOK, apps)
}
