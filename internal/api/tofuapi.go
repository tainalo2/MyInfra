package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"myinfra/internal/catalog"
	"myinfra/internal/dns"
)

func (a *API) serverTofu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	row, err := a.Store.Server(ctx, ownerID(ctx), r.PathValue("id"))
	if err != nil || row.ProviderServerID == "" {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	files, err := provisionClient(a).StackFiles(row.Provider, row.ProviderServerID)
	if err != nil {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	if r.URL.Query().Get("export") == "1" {
		writeZip(w, row.Name+".zip", files)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (a *API) saveServerTofu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := ownerID(ctx)
	row, err := a.Store.Server(ctx, owner, r.PathValue("id"))
	if err != nil || row.ProviderServerID == "" {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	var body struct {
		Files map[string]string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Files) == 0 {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	sec, tok, err := a.openToken(ctx, owner, row.ProviderID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	machine, err := provisionClient(a).ApplyStack(ctx, sec.Provider, catalog.Creds{
		Token: tok.Token, ProjectID: tok.ProjectID,
		ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, row.ProviderServerID, body.Files)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": machine.ID, "ip": machine.IPv4})
}

func (a *API) dnsTofu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host := strings.ToLower(r.PathValue("host"))
	_, provider, err := a.Store.DomainName(ctx, ownerID(ctx), host)
	if err != nil || provider == "" {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	files, err := a.dnsClient().RecordFiles(provider, host)
	if err != nil {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	if r.URL.Query().Get("export") == "1" {
		writeZip(w, host+".zip", files)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (a *API) saveDNSTofu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := ownerID(ctx)
	host := strings.ToLower(r.PathValue("host"))
	name, provider, err := a.Store.DomainName(ctx, owner, host)
	if err != nil || provider == "" || name.SecretID == "" {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	var body struct {
		Files map[string]string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Files) == 0 {
		a.mapErr(w, errString("Aucun fichier OpenTofu."))
		return
	}
	_, tok, err := a.dnsToken(ctx, owner, name.SecretID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	err = a.dnsClient().ApplyRecord(ctx, provider, dns.Creds{
		Token: tok.Token, ApplicationKey: tok.ApplicationKey, ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}, host, body.Files)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeZip(w http.ResponseWriter, name string, files map[string]string) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for file, body := range files {
		entry, err := zw.Create(file)
		if err != nil {
			return
		}
		_, _ = entry.Write([]byte(body))
	}
	_ = zw.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	_, _ = w.Write(buf.Bytes())
}
