package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"myinfra/internal/spec"
)

func TestServiceSpecAndPackagesAreKeptAndSynced(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const leftName, rightName = "myinfra_spec_a", "myinfra_spec_b"
	resetDB(t, admin, leftName)
	resetDB(t, admin, rightName)
	left, err := Open(ctx, dsn(leftName))
	if err != nil {
		t.Fatal(err)
	}
	right, err := Open(ctx, dsn(rightName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		left.Close()
		right.Close()
		dropDB(t, admin, leftName)
		dropDB(t, admin, rightName)
	}()
	if err := left.CreateFirstUser(ctx, "u1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := left.CreateCluster(ctx, "u1", "c1", "grappe"); err != nil {
		t.Fatal(err)
	}
	want := spec.Service{
		Command:   []string{"/bin/app", "serve"},
		Volumes:   []spec.Volume{{Path: "/cache", SizeGi: 2}},
		Files:     []spec.File{{Path: "/etc/app.ini", Content: "a = 1\n"}},
		Env:       map[string]string{"LEVEL": "debug"},
		Resources: &spec.Resources{Memory: "256Mi"},
		DependsOn: []string{"db"},
	}
	err = left.CreateApplication(ctx, NewApplication{
		ID: "a1", OwnerID: "u1", Name: "wiki", SourceKind: "image", Source: "acme/wiki:1",
		DestinationKind: "cluster", ClusterID: "c1", Replicas: 1, Variables: map[string]string{},
		DefaultID: "d0", DefaultHost: "wiki.apps.localhost",
		Services: []Service{
			{ID: "v1", Name: "web", Image: "acme/wiki:1", Published: true, Replicas: 1, Spec: want},
			{ID: "v2", Name: "db", Image: "postgres:16", Replicas: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func(db *Store) map[string]spec.Service {
		apps, err := db.ClusterApps(ctx, "c1")
		if err != nil || len(apps) != 1 {
			t.Fatalf("%+v %v", apps, err)
		}
		out := map[string]spec.Service{}
		for _, svc := range apps[0].Services {
			out[svc.Name] = svc.Spec
		}
		return out
	}
	got := read(left)
	if !reflect.DeepEqual(got["web"], want) || !got["db"].Empty() {
		t.Fatalf("%+v", got)
	}
	if err := left.CreatePackage(ctx, "u1", "p1", "Wiki", []byte(`{"type":"myinfra-package","name":"Wiki"}`)); err != nil {
		t.Fatal(err)
	}
	if err := left.CreatePackage(ctx, "u1", "p2", " wiki ", []byte(`{}`)); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("same name: %v", err)
	}
	converge(t, ctx, left, right)
	if got, err := right.Package(ctx, "u1", "p1"); err != nil || got.Name != "Wiki" || !strings.Contains(string(got.Body), "myinfra-package") {
		t.Fatalf("%+v %v", got, err)
	}
	if got := read(right); !reflect.DeepEqual(got["web"], want) || !got["db"].Empty() {
		t.Fatalf("%+v", got)
	}
}
