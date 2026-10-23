package api

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"myinfra/internal/pack"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

func TestPlanServicesNamesWhatGoes(t *testing.T) {
	current := []store.Service{
		{ID: "1", Name: "web", Disk: true, DiskPath: "/data", DiskGi: 1, Replicas: 1, Spec: spec.Service{Volumes: []spec.Volume{{Path: "/a"}, {Path: "/b"}}}},
		{ID: "2", Name: "db", Disk: true, DiskPath: "/var/lib/db", DiskGi: 1, Replicas: 3},
		{ID: "3", Name: "kept", Disk: true, DiskPath: "/data", DiskGi: 1, Replicas: 1, Database: "ready"},
	}
	next := []store.Service{
		{ID: "n1", Name: "web", Disk: true, DiskPath: "/data", DiskGi: 1, Replicas: 1, Spec: spec.Service{Volumes: []spec.Volume{{Path: "/a"}}}},
		{ID: "n3", Name: "kept", Disk: true, DiskPath: "/data", DiskGi: 1, Replicas: 1},
	}
	change, err := planServices(current, next, make([]pack.Service, len(next)))
	if err != nil {
		t.Fatal(err)
	}
	want := []claimGone{{name: "web.v2"}, {prefix: "data-db-"}}
	if !reflect.DeepEqual(change.claims, want) || !reflect.DeepEqual(change.gone, []string{"db"}) || next[0].ID != "1" || next[1].Database != "ready" {
		t.Fatalf("%+v %+v", change, next)
	}
	if _, err := planServices(current, next[:1], make([]pack.Service, 1)); err == nil {
		t.Fatal("a replicated database is removed")
	}
	middle := []store.Service{{Name: "web", Disk: true, DiskPath: "/data", DiskGi: 1, Replicas: 1, Spec: spec.Service{Volumes: []spec.Volume{{Path: "/b"}}}}, next[1]}
	if _, err := planServices(current, middle, make([]pack.Service, 2)); err == nil {
		t.Fatal("a disk in the middle goes")
	}
}

func TestConvertReadsACompose(t *testing.T) {
	data, err := os.ReadFile("../pack/testdata/nextcloud.compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	a := &API{}
	rec := packageCall(t, a, a.convertPackage, http.MethodPost, "/api/packages/convert", "", map[string]string{"text": string(data), "name": "Nextcloud"})
	var out struct {
		Format   string         `json:"format"`
		YAML     string         `json:"yaml"`
		Warnings []pack.Warning `json:"warnings"`
		Problems []pack.Problem `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if out.Format != "compose" || len(out.Problems) != 0 || len(out.Warnings) == 0 || !strings.Contains(out.YAML, "name: Nextcloud") || !strings.Contains(out.YAML, "public: true") {
		t.Fatalf("%+v", out)
	}
	bad := packageCall(t, a, a.convertPackage, http.MethodPost, "/api/packages/convert", "", map[string]string{"text": "hello: world"})
	if bad.Code != http.StatusBadRequest {
		t.Fatal(bad.Code, bad.Body.String())
	}
}
