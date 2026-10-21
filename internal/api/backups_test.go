package api

import (
	"testing"

	"myinfra/internal/store"
)

func TestBackupSourcesSQLiteReady(t *testing.T) {
	a := &API{}
	app := store.Application{ID: "a1", ClusterID: "c1", Services: []store.Service{{Name: "kuma", Image: "louislam/uptime-kuma:2", Disk: true, Backup: "ready"}}}
	sources := a.backupSources(app, app.Services[0])
	var garage bool
	for _, s := range sources {
		if s.ID == "garage" && s.Available {
			garage = true
		}
	}
	if !garage {
		t.Fatalf("%+v", sources)
	}
}

func TestBackupSourcesPostgres(t *testing.T) {
	a := &API{}
	app := store.Application{ID: "a1", ClusterID: "c1", Services: []store.Service{{Name: "vikunja", Image: "vikunja/vikunja:latest", Disk: true, Database: "ready"}}}
	sources := a.backupSources(app, app.Services[0])
	if len(sources) != 1 || sources[0].ID != "postgres" {
		t.Fatalf("%+v", sources)
	}
}

func TestBackupSourcesAloneMachine(t *testing.T) {
	a := &API{}
	app := store.Application{ID: "a1", ServerID: "s1", Services: []store.Service{{Name: "web", Image: "nginx:latest", Disk: true}}}
	sources := a.backupSources(app, app.Services[0])
	var machine bool
	for _, s := range sources {
		if s.ID == "machine" && s.Available {
			machine = true
		}
	}
	if !machine {
		t.Fatalf("%+v", sources)
	}
}
