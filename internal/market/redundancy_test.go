package market

import "testing"

func TestProfileOf(t *testing.T) {
	cases := []struct {
		image string
		disk  bool
		want  Kind
	}{
		{"louislam/uptime-kuma:2.5.6", true, SingleWriter},
		{"docker.io/louislam/uptime-kuma@sha256:abc", true, SingleWriter},
		{"vikunja/vikunja:latest", true, Database},
		{"excalidraw/excalidraw:latest", false, Stateless},
		{"registry.example:5000/team/app:1", true, Unknown},
	}
	for _, c := range cases {
		if got := ProfileOf(c.image, c.disk).Kind; got != c.want {
			t.Fatalf("%s: %s", c.image, got)
		}
	}
	// A database profile without its move would start a migration that never ends.
	for repo, profile := range redundancy {
		if profile.Kind == Database && (profile.Postgres == nil || len(profile.Postgres.Dump) == 0 || len(profile.Postgres.Restore) == 0 || len(profile.Postgres.Tables) == 0) {
			t.Fatalf("%s : migration incomplète", repo)
		}
	}
	if repository("registry.example:5000/team/app:1") != "registry.example:5000/team/app" {
		t.Fatal(repository("registry.example:5000/team/app:1"))
	}
}
