package store

import (
	"context"
	"strings"
	"testing"
)

// A machine alone receives the published names of its applications, for the
// first service as for the others: without them the proxy has no route.
func TestDesiredAppsCarryThePublishedNames(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_desired"
	resetDB(t, admin, name)
	db, err := Open(ctx, dsn(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		dropDB(t, admin, name)
	}()
	if err := db.CreateFirstUser(ctx, "u1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateServer(ctx, "u1", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	err = db.CreateApplication(ctx, NewApplication{
		ID: "a1", OwnerID: "u1", Name: "statut", SourceKind: "image", Source: "louislam/uptime-kuma:1",
		DestinationKind: "server", ServerID: "s1", Replicas: 1, Variables: map[string]string{},
		DefaultID: "d0", DefaultHost: "statut.apps.localhost",
		Names:    []Name{{ID: "d1", Hostname: "statut.exemple.fr", Zone: "exemple.fr"}},
		Services: []Service{{ID: "v1", Name: "statut", Image: "louislam/uptime-kuma:1", Published: true, Replicas: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE domains SET status = 'ready' WHERE id = 'd1'`); err != nil {
		t.Fatal(err)
	}
	apps, err := db.DesiredApps(ctx, "tok-solo-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || len(apps[0].Hosts) != 1 || apps[0].Hosts[0] != "statut.exemple.fr" || apps[0].Zones["statut.exemple.fr"] != "exemple.fr" {
		t.Fatalf("%+v", apps)
	}
}

// Each public service receives its own names; the main one also those
// without a service, and each answers on its own address.
func TestEachPublicServiceHasItsNames(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_service_names"
	resetDB(t, admin, name)
	db, err := Open(ctx, dsn(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		dropDB(t, admin, name)
	}()
	if err := db.CreateFirstUser(ctx, "u1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateServer(ctx, "u1", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	err = db.CreateApplication(ctx, NewApplication{
		ID: "a1", OwnerID: "u1", Name: "media", SourceKind: "image", Source: "jellyseerr",
		DestinationKind: "server", ServerID: "s1", Replicas: 1, Variables: map[string]string{},
		DefaultID: "d0", DefaultHost: "media.apps.localhost",
		ServiceDefaults: []Name{{ID: "d2", Hostname: "sonarr-media.apps.localhost", Service: "sonarr"}},
		Names: []Name{
			{ID: "n1", Hostname: "media.exemple.fr", Zone: "exemple.fr"},
			{ID: "n2", Hostname: "series.exemple.fr", Zone: "exemple.fr", Service: "sonarr"},
		},
		Services: []Service{
			{ID: "v1", Name: "jellyseerr", Image: "jellyseerr", Published: true, Replicas: 1},
			{ID: "v2", Name: "sonarr", Image: "sonarr", Published: true, Replicas: 1},
			{ID: "v3", Name: "qbit", Image: "qbit", Replicas: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	addresses, err := db.Addresses(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if addresses[""] != "media.apps.localhost" || addresses["sonarr"] != "sonarr-media.apps.localhost" {
		t.Fatal(addresses)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE domains SET status = 'ready' WHERE id IN ('n1', 'n2')`); err != nil {
		t.Fatal(err)
	}
	if addresses, _ = db.Addresses(ctx, "a1"); addresses[""] != "media.exemple.fr" || addresses["sonarr"] != "series.exemple.fr" {
		t.Fatal(addresses)
	}
	if main, _ := db.Address(ctx, "a1"); main != "media.exemple.fr" {
		t.Fatal(main)
	}
	apps, err := db.DesiredApps(ctx, "tok-solo-123")
	if err != nil {
		t.Fatal(err)
	}
	hosts := map[string]string{}
	for _, app := range apps {
		hosts[app.Service] = strings.Join(app.Hosts, ",")
	}
	if hosts["jellyseerr"] != "media.exemple.fr" || hosts["sonarr"] != "series.exemple.fr" || hosts["qbit"] != "" {
		t.Fatal(hosts)
	}
	app, err := db.Application(ctx, "u1", "a1")
	if err != nil || app.Hostname != "media.exemple.fr" {
		t.Fatal(app.Hostname, err)
	}
}
