package api

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"myinfra/internal/agent"
	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/store"
)

type backupSourceView struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Restore   bool   `json:"restore,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type backupServiceView struct {
	Name    string             `json:"name"`
	Sources []backupSourceView `json:"sources"`
}

type backupListView struct {
	Services []backupServiceView `json:"services"`
}

func (a *API) listApplicationBackups(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.ID == selfAppID {
		writeJSON(w, http.StatusOK, backupListView{})
		return
	}
	out := backupListView{Services: make([]backupServiceView, 0, len(app.Services))}
	for _, svc := range app.Services {
		out.Services = append(out.Services, backupServiceView{
			Name:    svc.Name,
			Sources: a.backupSources(app, svc),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) backupSources(app store.Application, svc store.Service) []backupSourceView {
	profile := market.ProfileOf(svc.Image, svc.Disk)
	var out []backupSourceView
	add := func(id, label, detail string, available bool) {
		if !available {
			return
		}
		out = append(out, backupSourceView{
			ID: id, Label: label, Detail: detail, Available: true,
			Restore: backupRestoreAllowed(app, svc, id),
		})
	}
	if app.ClusterID != "" {
		if svc.Database == "ready" && profile.Postgres != nil {
			add("postgres", "PostgreSQL", "", true)
		}
		if len(profile.SQLite) > 0 && svc.Backup == "ready" {
			add("garage", "Stockage du cluster", "", true)
			if offsiteS3(app.ClusterID, app.ID) != nil {
				add("offsite", "S3 externe", "", true)
			}
		}
		if svc.Backup == "migrating" {
			add("disk", "Disque du nœud", "copie en cours vers le stockage", true)
		}
		if svc.Database == "migrating" {
			add("disk", "Disque du nœud", "migration PostgreSQL en cours", true)
		}
	}
	if svc.Disk && diskBackupAvailable(profile, svc) {
		if !hasSource(out, "disk") {
			add("disk", "Disque en cours", "", true)
		}
	}
	if app.ServerID != "" && svc.Disk && profile.Kind != market.Stateless {
		add("machine", "Machine seule", "", true)
	}
	if len(out) == 0 && svc.Disk && profile.Kind != market.Stateless {
		add("disk", "Disque", "fichiers non reconnus par MyInfra", true)
	}
	return out
}

func hasSource(rows []backupSourceView, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func diskBackupAvailable(profile market.Profile, svc store.Service) bool {
	if !svc.Disk {
		return false
	}
	if svc.Backup == "migrating" || svc.Database == "migrating" {
		return true
	}
	if profile.Kind == market.Unknown {
		return true
	}
	if profile.Kind == market.SingleWriter && svc.Backup == "" {
		return true
	}
	return false
}

func (a *API) downloadApplicationBackup(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.ID == selfAppID {
		writeError(w, http.StatusNotFound, "Cette application n'a pas de sauvegarde téléchargeable.")
		return
	}
	svcName := r.PathValue("service")
	if !backupServiceName.MatchString(svcName) {
		writeError(w, http.StatusBadRequest, "Service inconnu.")
		return
	}
	var svc store.Service
	for _, row := range app.Services {
		if row.Name == svcName {
			svc = row
			break
		}
	}
	if svc.Name == "" {
		writeError(w, http.StatusNotFound, "Service inconnu.")
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	allowed := a.backupSources(app, svc)
	ok := false
	for _, row := range allowed {
		if row.ID == source && row.Available {
			ok = true
			break
		}
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "Cette sauvegarde n'est pas disponible pour ce service.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Hour)
	defer cancel()
	owner := ownerID(r.Context())
	filename, command, host, contentType, err := a.backupCommand(ctx, owner, app, svc, source)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	_ = deploy.SSHPipe(ctx, host, command, w)
}

var backupServiceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

func (a *API) backupCommand(ctx context.Context, owner string, app store.Application, svc store.Service, source string) (filename, command string, host deploy.Host, contentType string, err error) {
	contentType = "application/gzip"
	profile := market.ProfileOf(svc.Image, svc.Disk)
	switch {
	case app.ClusterID != "":
		host, err = a.clusterHost(ctx, owner, app.ClusterID)
		if err != nil {
			return "", "", host, contentType, err
		}
		ns := manifest.Namespace(app.ID)
		switch source {
		case "garage":
			bucket := storageBucket(app.ID)
			keyID, secret := a.storageKey(app.ID)
			prefix := svc.Name + "/"
			command = awsTarCommand(manifest.StorageEndpoint, manifest.StorageRegion, bucket, prefix, keyID, secret)
			filename = safeFilename(app.Name, svc.Name, "garage.tar.gz")
		case "offsite":
			off := offsiteS3(app.ClusterID, app.ID)
			if off == nil {
				return "", "", host, contentType, errString("S3 externe non configuré.")
			}
			prefix := off.Path + "/" + svc.Name + "/"
			command = awsTarCommand(off.Endpoint, off.Region, off.Bucket, prefix, off.KeyID, off.Secret)
			filename = safeFilename(app.Name, svc.Name, "offsite.tar.gz")
		case "postgres":
			contentType = "application/octet-stream"
			db := databaseName(svc.Name)
			command = fmt.Sprintf(
				"NS=%s DB=%s k3s kubectl -n \"$NS\" exec %s-1 -c postgres -- pg_dump -Fc -U \"$(k3s kubectl -n \"$NS\" get secret %s-app -o jsonpath='{.data.username}' | base64 -d)\" \"$(k3s kubectl -n \"$NS\" get secret %s-app -o jsonpath='{.data.dbname}' | base64 -d)\"",
				shellLit(ns), shellLit(db), shellLit(db), shellLit(db), shellLit(db),
			)
			filename = safeFilename(app.Name, svc.Name, "postgres.dump")
		case "disk", "machine":
			command, err = clusterDiskTar(ns, svc, profile)
			if err != nil {
				return "", "", host, contentType, err
			}
			filename = safeFilename(app.Name, svc.Name, "disk.tar.gz")
		default:
			return "", "", host, contentType, errString("Source inconnue.")
		}
	case app.ServerID != "" && source == "machine":
		host, err = a.machineHost(ctx, owner, app.ServerID)
		if err != nil {
			return "", "", host, contentType, err
		}
		diskPath := svc.DiskPath
		if diskPath == "" {
			diskPath = "/"
		}
		command = fmt.Sprintf(
			"CID=$(docker ps -q -f name=%s) && test -n \"$CID\" && docker exec \"$CID\" tar czf - -C %s .",
			shellLit("^"+agent.ContainerPrefix(app.ID)), shellLit(diskPath),
		)
		filename = safeFilename(app.Name, svc.Name, "machine.tar.gz")
	default:
		return "", "", host, contentType, errString("Cette application n'a pas de sauvegarde ici.")
	}
	return filename, command, host, contentType, nil
}

func clusterDiskTar(ns string, svc store.Service, profile market.Profile) (string, error) {
	diskPath := strings.TrimSpace(svc.DiskPath)
	if diskPath == "" && len(profile.SQLite) > 0 {
		diskPath = path.Dir(profile.SQLite[0])
	}
	if diskPath == "" {
		diskPath = "/"
	}
	if !safeShell(diskPath) || !safeShell(svc.Name) || !safeShell(ns) {
		return "", errString("Chemin de données refusé.")
	}
	parent := path.Dir(diskPath)
	base := path.Base(diskPath)
	return fmt.Sprintf(
		`NS=%s SVC=%s POD=$(k3s kubectl -n "$NS" get pod -l myinfra.io/service="$SVC" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null) && test -n "$POD" && (k3s kubectl -n "$NS" exec "$POD" -c "$SVC" -- tar czf - -C %s %s 2>/dev/null || k3s kubectl -n "$NS" exec "$POD" -- tar czf - -C %s %s)`,
		shellLit(ns), shellLit(svc.Name), shellLit(parent), shellLit(base), shellLit(parent), shellLit(base),
	), nil
}

func awsTarCommand(endpoint, region, bucket, prefix, keyID, secret string) string {
	if !safeShell(endpoint) || !safeShell(bucket) || !safeShell(prefix) || !safeShell(keyID) || !safeShell(secret) {
		return "false"
	}
	reg := region
	if reg == "" {
		reg = "auto"
	}
	return fmt.Sprintf(
		`k3s kubectl run backup-dl-%d --rm -i --restart=Never -n %s --image=amazon/aws-cli:2.22.12 --command -- env AWS_ACCESS_KEY_ID=%s AWS_SECRET_ACCESS_KEY=%s AWS_DEFAULT_REGION=%s sh -c 'aws --endpoint-url %s s3 sync s3://%s/%s /tmp/exp && tar czf - -C /tmp/exp .'`,
		time.Now().UnixNano()%1_000_000_000,
		manifest.StorageNamespace,
		shellLit(keyID), shellLit(secret), shellLit(reg), shellLit(endpoint), shellLit(bucket), shellLit(prefix),
	)
}

func shellLit(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func safeFilename(app, svc, suffix string) string {
	clean := func(s string) string {
		s = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '-'
		}, strings.TrimSpace(s))
		if s == "" {
			return "app"
		}
		return s
	}
	return clean(app) + "-" + clean(svc) + "-" + suffix
}
