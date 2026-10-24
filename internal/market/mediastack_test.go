package market

import (
	"strings"
	"testing"
)

// The media stack publishes each application on its own names and mounts
// one shared disk where downloads and the library meet.
func TestMediaStackSharesOneDisk(t *testing.T) {
	p, ok := PackageOf("media-stack")
	if !ok {
		t.Fatal("article absent")
	}
	services, env, err := Expand("media-stack", "media.example.org", map[string]string{"admin": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Main() != "jellyseerr" {
		t.Fatal(p.Main())
	}
	public, shared := 0, 0
	for _, svc := range services {
		if svc.Public {
			public++
		}
		for _, volume := range svc.Volumes {
			if volume.Disk == "media" && volume.SizeGi == 20 && volume.Path == "/data" {
				shared++
			}
		}
	}
	if public != 6 || shared != 5 {
		t.Fatal(public, shared)
	}
	for _, key := range []string{"SONARR__AUTH__APIKEY", "RADARR__AUTH__APIKEY", "PROWLARR__AUTH__APIKEY", "ADMIN_PASSWORD"} {
		if len(env[key]) < 32 {
			t.Fatal(key, env[key])
		}
	}
	if env["ADMIN_USER"] != "alice" || !strings.Contains(services[1].Env["JELLYFIN_PublishedServerUrl"], "{{url:jellyfin}}") {
		t.Fatal(env["ADMIN_USER"], services[1].Env)
	}
}
