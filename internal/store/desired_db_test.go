package store

import (
	"context"
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
