package api

import (
	"fmt"
	"testing"

	"myinfra/internal/deploy"
)

func TestMarkDeployCancelled(t *testing.T) {
	steps := markDeployCancelled([]deploy.Step{
		{ID: "machine", Status: "done", Detail: "1.2.3.4"},
		{ID: "ssh", Status: "running", Detail: "Attente"},
		{ID: "keys", Status: "pending"},
	})
	if steps[0].Status != "done" || steps[0].Detail != "1.2.3.4" {
		t.Fatalf("étape faite modifiée: %+v", steps[0])
	}
	if steps[1].Status != "failed" || steps[1].Detail != "Annulé." || steps[2].Status != "failed" {
		t.Fatalf("%+v", steps)
	}
}

func TestCurrentStep(t *testing.T) {
	raw := []byte(`[{"label":"Ouverture","status":"done"},{"label":"Liaison","status":"running"}]`)
	if got := currentStep(raw); got != "Liaison" {
		t.Fatalf("%q", got)
	}
	raw = []byte(`[{"name":"web","status":"pending"},{"name":"db","status":"pending"}]`)
	if got := currentStep(raw); got != "web" {
		t.Fatalf("%q", got)
	}
}

func TestTemplateCourse(t *testing.T) {
	raw := []byte(`{"clusterName":"atelier","steps":[{"name":"machine","status":"done"},{"name":"application","status":"running"}]}`)
	name, step := templateCourse(raw)
	if name != "atelier" || step != "application" {
		t.Fatalf("%q %q", name, step)
	}
	name, step = templateCourse([]byte(`{"steps":[{"name":"seule","status":"pending"}]}`))
	if name != "seule" || step != "seule" {
		t.Fatalf("%q %q", name, step)
	}
}

func TestAppRejectedOnlyWhenTheClusterAnswered(t *testing.T) {
	if !appRejected(&deploy.CommandError{Text: `The Deployment "web" is invalid`}) {
		t.Fatal("manifeste refusé")
	}
	if appRejected(&deploy.CommandError{Text: "The connection to the server 127.0.0.1:6443 was refused"}) {
		t.Fatal("api indisponible")
	}
	if appRejected(fmt.Errorf("dial tcp: i/o timeout")) {
		t.Fatal("machine injoignable")
	}
}

func TestMarkDeployInterrupted(t *testing.T) {
	steps := markDeployInterrupted([]deploy.Step{
		{ID: "machine", Status: "done", Detail: "1.2.3.4"},
		{ID: "kubernetes", Status: "running"},
		{ID: "wireguard", Status: "pending"},
	})
	if steps[0].Status != "done" || steps[1].Status != "failed" || steps[2].Status != "pending" {
		t.Fatalf("steps %+v", steps)
	}
	if !deploy.Done(deploy.Resume(steps), "machine") || deploy.Resume(steps)[1].Status != "pending" {
		t.Fatal("relance")
	}
}
