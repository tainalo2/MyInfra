package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/agent"
	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/store"
)

const backupUploadLimit = 1 << 30 // 1 GiB

func (a *API) restoreApplicationBackup(w http.ResponseWriter, r *http.Request) {
	if a.Replica {
		writeCoded(w, http.StatusConflict, "replica", replicaRefused, nil)
		return
	}
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.ID == selfAppID {
		writeError(w, http.StatusNotFound, "Cette application ne peut pas être restaurée ici.")
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
	if !backupRestoreAllowed(app, svc, source) {
		writeError(w, http.StatusBadRequest, "Cette restauration n'est pas possible pour ce service.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backupUploadLimit)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Hour)
	defer cancel()
	owner := ownerID(r.Context())
	command, host, err := a.restoreCommand(ctx, owner, app, svc, source)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := deploy.SSHPipeStdin(ctx, host, command, r.Body); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func backupRestoreAllowed(app store.Application, svc store.Service, source string) bool {
	profile := profileOf(svc)
	switch source {
	case "postgres":
		return app.ClusterID != "" && svc.Database == "ready" && profile.Postgres != nil
	case "garage":
		return app.ClusterID != "" && svc.Backup == "ready" && len(profile.SQLite) > 0
	case "offsite":
		return app.ClusterID != "" && svc.Backup == "ready" && len(profile.SQLite) > 0 && offsiteS3(app.ClusterID, app.ID) != nil
	case "disk":
		if app.ClusterID == "" || svc.Backup == "ready" {
			return false
		}
		return diskBackupAvailable(profile, svc) || svc.Backup == "migrating" || svc.Database == "migrating"
	case "machine":
		return app.ServerID != "" && svc.Disk && profile.Kind != market.Stateless
	default:
		return false
	}
}

func (a *API) restoreCommand(ctx context.Context, owner string, app store.Application, svc store.Service, source string) (command string, host deploy.Host, err error) {
	profile := profileOf(svc)
	switch {
	case app.ClusterID != "":
		host, err = a.clusterHost(ctx, owner, app.ClusterID)
		if err != nil {
			return "", host, err
		}
		ns := manifest.Namespace(app.ID)
		switch source {
		case "garage":
			bucket := storageBucket(app.ID)
			keyID, secret := a.storageKey(app.ID)
			command = restoreS3TarScript(ns, svc.Name, manifest.StorageEndpoint, manifest.StorageRegion, bucket, svc.Name+"/", keyID, secret)
		case "offsite":
			off := offsiteS3(app.ClusterID, app.ID)
			if off == nil {
				return "", host, errString("S3 externe non configuré.")
			}
			command = restoreS3TarScript(ns, svc.Name, off.Endpoint, off.Region, off.Bucket, off.Path+"/"+svc.Name+"/", off.KeyID, off.Secret)
		case "postgres":
			command = restorePostgresScript(ns, svc.Name, databaseName(svc.Name))
		case "disk":
			command, err = restoreDiskScript(ns, svc, profile)
		default:
			return "", host, errString("Source inconnue.")
		}
	case app.ServerID != "" && source == "machine":
		host, err = a.machineHost(ctx, owner, app.ServerID)
		if err != nil {
			return "", host, err
		}
		diskPath := svc.DiskPath
		if diskPath == "" {
			diskPath = "/"
		}
		command = restoreMachineScript(agent.ContainerPrefix(app.ID), diskPath)
	default:
		return "", host, errString("Cette application ne peut pas être restaurée ici.")
	}
	return command, host, nil
}

func restoreS3TarScript(ns, svc, endpoint, region, bucket, prefix, keyID, secret string) string {
	if !safeShell(ns) || !safeShell(svc) || !safeShell(endpoint) || !safeShell(bucket) || !safeShell(prefix) || !safeShell(keyID) || !safeShell(secret) {
		return "false"
	}
	reg := region
	if reg == "" {
		reg = "auto"
	}
	return fmt.Sprintf(`set -e
DIR=/tmp/myinfra-restore-$$
mkdir -p "$DIR"
tar xzf - -C "$DIR"
docker run --rm -v "$DIR:/data:ro" -e AWS_ACCESS_KEY_ID=%[1]s -e AWS_SECRET_ACCESS_KEY=%[2]s -e AWS_DEFAULT_REGION=%[3]s amazon/aws-cli:2.22.12 aws --endpoint-url %[4]s s3 sync /data s3://%[5]s/%[6]s
rm -rf "$DIR"
k3s kubectl -n %[7]s delete pod -l myinfra.io/service=%[8]s --wait=false
k3s kubectl -n %[7]s rollout restart deploy/%[8]s 2>/dev/null || true
`, shellLit(keyID), shellLit(secret), shellLit(reg), shellLit(endpoint), shellLit(bucket), shellLit(prefix), shellLit(ns), shellLit(svc))
}

func restorePostgresScript(ns, svc, db string) string {
	if !safeShell(ns) || !safeShell(svc) || !safeShell(db) {
		return "false"
	}
	return fmt.Sprintf(`set -e
FILE=/tmp/myinfra-pg-$$
cat > "$FILE"
k3s kubectl -n %[1]s scale deploy/%[2]s --replicas=0 --timeout=120s 2>/dev/null || true
k3s kubectl -n %[1]s cp "$FILE" %[3]s-1:/tmp/restore.dump -c postgres
k3s kubectl -n %[1]s exec %[3]s-1 -c postgres -- pg_restore --clean --if-exists -U "$(k3s kubectl -n %[1]s get secret %[3]s-app -o jsonpath='{.data.username}' | base64 -d)" -d "$(k3s kubectl -n %[1]s get secret %[3]s-app -o jsonpath='{.data.dbname}' | base64 -d)" /tmp/restore.dump
rm -f "$FILE"
k3s kubectl -n %[1]s scale deploy/%[2]s --replicas=1 --timeout=120s 2>/dev/null || k3s kubectl -n %[1]s rollout restart deploy/%[2]s
`, shellLit(ns), shellLit(svc), shellLit(db))
}

func restoreDiskScript(ns string, svc store.Service, profile market.Profile) (string, error) {
	if !safeShell(ns) || !safeShell(svc.Name) {
		return "", errString("Service refusé.")
	}
	claimJSON := `"` + svc.Name + `"`
	return fmt.Sprintf(`set -e
k3s kubectl -n %[1]s scale deploy/%[2]s --replicas=0 --timeout=180s 2>/dev/null || true
sleep 3
k3s kubectl -n %[1]s run restore-$$ --rm -i --restart=Never --image=busybox:1.36 --overrides='{"spec":{"containers":[{"name":"r","image":"busybox:1.36","stdin":true,"command":["tar","xzf","-","-C","/data"],"volumeMounts":[{"name":"data","mountPath":"/data"}]}],"volumes":[{"name":"data","persistentVolumeClaim":{"claimName":%[3]s}}]}}'
k3s kubectl -n %[1]s scale deploy/%[2]s --replicas=1 --timeout=180s 2>/dev/null || k3s kubectl -n %[1]s rollout restart deploy/%[2]s
`, shellLit(ns), shellLit(svc.Name), claimJSON), nil
}

func restoreMachineScript(prefix, diskPath string) string {
	if !safeShell(prefix) || !safeShell(diskPath) {
		return "false"
	}
	return fmt.Sprintf(`set -e
CID=$(docker ps -q -f name=%[1]s | head -n1)
test -n "$CID"
docker stop "$CID" || true
docker run --rm -i --volumes-from "$CID" busybox:1.36 tar xzf - -C %[2]s
docker start "$CID" || true
`, shellLit("^"+prefix), shellLit(diskPath))
}
