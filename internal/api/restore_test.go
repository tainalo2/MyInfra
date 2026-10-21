package api

import (
	"testing"

	"myinfra/internal/store"
)

func TestBackupRestoreAllowedPostgres(t *testing.T) {
	app := store.Application{ID: "a1", ClusterID: "c1", Services: []store.Service{{Name: "vikunja", Image: "vikunja/vikunja:latest", Disk: true, Database: "ready"}}}
	if !backupRestoreAllowed(app, app.Services[0], "postgres") {
		t.Fatal("postgres")
	}
	if backupRestoreAllowed(app, app.Services[0], "disk") {
		t.Fatal("disk interdit")
	}
}

func TestBackupRestoreGarageNotDisk(t *testing.T) {
	app := store.Application{ID: "a1", ClusterID: "c1", Services: []store.Service{{Name: "kuma", Image: "louislam/uptime-kuma:2", Disk: true, Backup: "ready"}}}
	if !backupRestoreAllowed(app, app.Services[0], "garage") {
		t.Fatal("garage")
	}
	if backupRestoreAllowed(app, app.Services[0], "disk") {
		t.Fatal("disque vide une fois la sauvegarde prête")
	}
}
