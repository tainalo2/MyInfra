package argo

import "testing"

func TestParseReadsSyncAndHealth(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"site","namespace":"argocd"},"status":{"sync":{"status":"Synced"},"health":{"status":"Healthy"}}}]}`)
	apps, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Name != "site" || apps[0].Sync != "Synced" || apps[0].Health != "Healthy" {
		t.Fatalf("%+v", apps)
	}
}
