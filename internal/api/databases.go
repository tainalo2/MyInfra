package api

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/store"
)

// A service that keeps its data in PostgreSQL when told to runs as many copies
// as wanted once its data lives in a replicated PostgreSQL and its files in
// the cluster's store. Moving there stops it for the time of the move.

var (
	databaseMu sync.Mutex
	// operatorReady holds the clusters where CloudNativePG runs.
	operatorReady = map[string]time.Time{}
)

const movingDetail = "Passage à une base PostgreSQL répliquée : l'application est arrêtée le temps de déplacer ses données."

func databaseName(svc string) string { return svc + "-db" }
func filesSecret(svc string) string  { return svc + "-files" }

// startDatabase asks a service to move to PostgreSQL. The delivery builds the
// database, stops the service, moves the data, then starts its copies.
func (a *API) startDatabase(ctx context.Context, owner string, app store.Application, copies int) error {
	for _, svc := range app.Services {
		profile := profileOf(svc)
		if profile.Kind != market.Database || profile.Postgres == nil {
			continue
		}
		if err := a.Store.SetServiceReplicas(ctx, app.ID, map[string]int{svc.Name: copies}); err != nil {
			return err
		}
		if svc.Database == "" {
			if err := a.Store.SetServiceDatabase(ctx, app.ID, svc.Name, "migrating"); err != nil {
				return err
			}
			_ = a.Store.SetRunDetail(ctx, app.ID, movingDetail)
		}
	}
	return nil
}

// serviceDatabase adds to the manifest the database and the object storage of
// a service, and points the service at them once its data has moved.
func (a *API) serviceDatabase(ctx context.Context, host deploy.Host, owner, clusterID, appID string, svc store.Service, out *manifest.Service, parts *appParts) error {
	profile := market.ProfileOf(svc.Image, true)
	if profile.Postgres == nil {
		return fmt.Errorf("MyInfra ne sait pas brancher cette image sur PostgreSQL.")
	}
	pg := profile.Postgres
	if err := a.ensureStorage(ctx, host, owner, clusterID); err != nil {
		return err
	}
	if err := a.ensureDatabaseOperator(ctx, host, clusterID); err != nil {
		return err
	}
	bucket, keyID, secret, err := a.ensureBucket(ctx, host, clusterID, appID)
	if err != nil {
		return err
	}
	db := manifest.Database{Name: databaseName(svc.Name), Database: pg.Database, Instances: 2, SizeGi: svc.DiskGi}
	if off := offsiteCNPG(clusterID, databaseName(svc.Name)); off != nil {
		db.Offsite = off
		db.BackupSchedule = cnpgBackupCron()
	}
	parts.Databases = append(parts.Databases, db)
	parts.Secrets = append(parts.Secrets, manifest.SharedSecret{Name: filesSecret(svc.Name), Data: map[string]string{"accessKey": keyID, "secretKey": secret}})
	if svc.Database == "migrating" {
		// Stopped on its disk until the data has moved.
		out.Stopped = true
		out.Replicas = 1
		return nil
	}
	out.Disk = false
	out.Env, out.SecretEnv = databaseEnv(pg, svc.Name, bucket)
	return nil
}

// databaseEnv is what a service reads to use its database and the store.
func databaseEnv(pg *market.PostgresEnv, svc, bucket string) (map[string]string, map[string]manifest.SecretKey) {
	env := map[string]string{
		pg.Host:       databaseName(svc) + "-rw",
		pg.S3Endpoint: manifest.StorageEndpoint,
		pg.S3Bucket:   bucket,
		pg.S3Region:   manifest.StorageRegion,
	}
	for key, value := range pg.Fixed {
		env[key] = value
	}
	for key, value := range pg.S3Fixed {
		env[key] = value
	}
	app := databaseName(svc) + "-app"
	secret := map[string]manifest.SecretKey{
		pg.User:        {Secret: app, Key: "username"},
		pg.Password:    {Secret: app, Key: "password"},
		pg.Name:        {Secret: app, Key: "dbname"},
		pg.S3AccessKey: {Secret: filesSecret(svc), Key: "accessKey"},
		pg.S3SecretKey: {Secret: filesSecret(svc), Key: "secretKey"},
	}
	return env, secret
}

// ensureDatabaseOperator installs CloudNativePG once per cluster.
func (a *API) ensureDatabaseOperator(ctx context.Context, host deploy.Host, clusterID string) error {
	databaseMu.Lock()
	ready := time.Since(operatorReady[clusterID]) < 10*time.Minute
	databaseMu.Unlock()
	if ready {
		return nil
	}
	if _, err := deploy.SSH(ctx, host, "k3s kubectl get deploy -n cnpg-system cnpg-controller-manager >/dev/null 2>&1 || k3s kubectl apply --server-side -f "+manifest.DatabaseOperator); err != nil {
		return fmt.Errorf("La gestion des bases PostgreSQL n'a pas pu être installée.")
	}
	if _, err := deploy.SSHInput(ctx, host, "k3s kubectl -n cnpg-system patch deploy cnpg-controller-manager --type merge --patch-file /dev/stdin", []byte(manifest.DatabaseOperatorPatch)); err != nil {
		return fmt.Errorf("La gestion des bases PostgreSQL n'a pas pu être réglée.")
	}
	if _, err := deploy.SSH(ctx, host, "k3s kubectl -n cnpg-system rollout status deploy/cnpg-controller-manager --timeout=60s"); err != nil {
		return fmt.Errorf("La gestion des bases PostgreSQL démarre.")
	}
	databaseMu.Lock()
	operatorReady[clusterID] = time.Now()
	databaseMu.Unlock()
	return nil
}

// advanceDatabases moves the data once the database answers, then starts the
// copies; it puts a service back on its disk if the move fails.
func (a *API) advanceDatabases(ctx context.Context, host deploy.Host, spec manifest.App, app store.ClusterApp) {
	ns := manifest.Namespace(app.ID)
	for _, svc := range app.Services {
		switch svc.Database {
		case "migrating":
			a.moveData(ctx, host, spec, app, svc)
		case "ready":
			// The old disk stays: it is the way back if anything is wrong.
			_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" delete job migrate-"+svc.Name+" --ignore-not-found --wait=false")
			a.restartWaiting(ctx, host, ns, svc.Name)
		}
	}
}

func (a *API) moveData(ctx context.Context, host deploy.Host, spec manifest.App, app store.ClusterApp, svc store.Service) {
	ns := manifest.Namespace(app.ID)
	profile := market.ProfileOf(svc.Image, true)
	if profile.Postgres == nil || len(profile.Postgres.Dump) == 0 || len(profile.Postgres.Restore) == 0 || len(profile.Postgres.Tables) == 0 {
		return
	}
	_ = a.Store.SetRunDetail(ctx, app.ID, movingDetail)
	out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" get cluster "+databaseName(svc.Name)+" -o jsonpath='{.status.readyInstances}' 2>/dev/null || true")
	if err != nil {
		return
	}
	if ready, _ := strconv.Atoi(strings.TrimSpace(out)); ready < 2 {
		return
	}
	state, err := deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" get job migrate-"+svc.Name+" -o jsonpath='{.status.succeeded}/{.status.failed}' 2>/dev/null || echo none")
	if err != nil {
		return
	}
	state = strings.TrimSpace(state)
	switch {
	case state == "none":
		var current *manifest.Service
		for i := range spec.Services {
			if spec.Services[i].Name == svc.Name {
				current = &spec.Services[i]
			}
		}
		if current == nil {
			return
		}
		env, secret := databaseEnv(profile.Postgres, svc.Name, storageBucket(app.ID))
		job := manifest.MigrationJob(spec, manifest.Migration{
			Service: *current, Dump: profile.Postgres.Dump, Restore: profile.Postgres.Restore, Tables: profile.Postgres.Tables, RestoreInput: profile.Postgres.RestoreInput, ToolEnv: profile.Postgres.ToolEnv,
			Database: databaseName(svc.Name), Env: env, SecretEnv: secret,
		})
		if _, err := deploy.SSHInput(ctx, host, "k3s kubectl apply -f -", []byte(job)); err != nil {
			slog.Info("migration", "app", app.ID, "err", err)
		}
	case strings.HasPrefix(state, "1/"):
		_ = a.Store.SetServiceDatabase(ctx, app.ID, svc.Name, "ready")
		_ = a.Store.SetRunDetail(ctx, app.ID, "")
	case strings.HasSuffix(state, "/1"):
		logs, _ := deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" logs job/migrate-"+svc.Name+" --all-containers --tail=5 2>&1 || true")
		slog.Info("migration", "app", app.ID, "logs", logs)
		// Back on its disk, as before: nothing was removed.
		_ = a.Store.SetServiceDatabase(ctx, app.ID, svc.Name, "")
		_ = a.Store.SetRunDetail(ctx, app.ID, "Le passage à PostgreSQL a échoué : l'application est repartie sur son disque, ses données sont intactes. "+lastLine(logs))
		// The database goes too: a new attempt starts from an empty one, so
		// that its check cannot count rows left by this one.
		_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" delete job migrate-"+svc.Name+" --ignore-not-found --wait=false; k3s kubectl -n "+ns+" delete cluster "+databaseName(svc.Name)+" --ignore-not-found --wait=false")
	}
}

// restartWaiting starts again at once the copies Kubernetes holds back after
// they stopped while their database moved to another node: it waits longer
// after each stop, up to five minutes, even once the database answers.
func (a *API) restartWaiting(ctx context.Context, host deploy.Host, ns, svc string) {
	phase, err := deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" get cluster "+databaseName(svc)+" -o jsonpath='{.status.phase}'")
	if err != nil || !strings.Contains(phase, "healthy") {
		return
	}
	out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" get pods -l myinfra.io/service="+svc+" -o jsonpath='{range .items[*]}{.metadata.name} {.status.containerStatuses[0].state.waiting.reason}{\"\\n\"}{end}'")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		if name, reason, ok := strings.Cut(strings.TrimSpace(line), " "); ok && reason == "CrashLoopBackOff" {
			_, _ = deploy.SSH(ctx, host, "k3s kubectl -n "+ns+" delete pod "+name+" --wait=false")
		}
	}
}

func movingData(app store.ClusterApp) bool {
	for _, svc := range app.Services {
		if svc.Database == "migrating" {
			return true
		}
	}
	return false
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}
