package api

import (
	"net/http"

	"myinfra/internal/tools"
)

func (a *API) hostTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toolState())
}

func (a *API) installTool(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != "ansible" && name != "tofu" {
		writeError(w, http.StatusNotFound, "Outil inconnu.")
		return
	}
	if err := tools.Install(r.Context(), name); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toolState())
}

func toolState() map[string]bool {
	return map[string]bool{
		"ansible":     tools.Ansible() != "",
		"tofu":        tools.Tofu() != "",
		"tofuEnabled": true,
	}
}
