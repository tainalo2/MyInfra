package api

import (
	"fmt"
	"net/http"

	"myinfra/internal/agent"
	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
)

// restartApplication starts every copy of an application again, without
// changing anything. In a cluster the copies are replaced one after the
// other, so an application with several copies keeps answering; on a machine
// alone its containers restart.
func (a *API) restartApplication(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == selfAppID {
		writeError(w, http.StatusConflict, "MyInfra reste en ligne pour piloter le cluster.")
		return
	}
	owner := ownerID(r.Context())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.Paused {
		writeError(w, http.StatusConflict, "Cette application est en pause : reprenez-la plutôt.")
		return
	}
	var host deploy.Host
	var command string
	switch {
	case app.ClusterID != "":
		host, err = a.clusterHost(r.Context(), owner, app.ClusterID)
		command = fmt.Sprintf("k3s kubectl rollout restart deployment,statefulset -n '%s'", manifest.Namespace(app.ID))
	case app.ServerID != "":
		host, err = a.machineHost(r.Context(), owner, app.ServerID)
		command = fmt.Sprintf("ids=$(docker ps -q --filter 'name=^%s'); [ -z \"$ids\" ] || docker restart $ids", agent.ContainerPrefix(app.ID))
	default:
		writeError(w, http.StatusConflict, "Cette application ne tourne nulle part.")
		return
	}
	if err != nil {
		a.writePodErr(w, err)
		return
	}
	if _, err := deploy.SSH(r.Context(), host, command); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.note(r.Context(), owner, newID(), "app.restart", "info", app.Name+" redémarre.", app.ServerID, app.ClusterID)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
}
