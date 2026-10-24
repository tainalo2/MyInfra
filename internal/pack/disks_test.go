package pack

import (
	"strings"
	"testing"
)

const mediaPackage = `type: myinfra-package
version: 1
name: Médias
disks:
  - name: media
    sizeGi: 200
services:
  - name: jellyfin
    image: jellyfin/jellyfin:10.10.7
    public: true
    port: 8096
    volumes:
      - path: /config
        sizeGi: 8
      - path: /data
        disk: media
  - name: sonarr
    image: linuxserver/sonarr:4.0.20
    port: 8989
    volumes:
      - path: /data
        disk: media
      - path: /config
        sizeGi: 4
`

func TestSharedDiskIsDeclaredOnceAndMountedByName(t *testing.T) {
	p, problems := Parse([]byte(mediaPackage))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	services, _, err := Expand(p, "media.example.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The shared volume carries the disk's size from here on.
	for _, svc := range services {
		for _, volume := range svc.Volumes {
			if volume.Disk == "media" && volume.SizeGi != 200 {
				t.Fatal(svc.Name, volume)
			}
		}
	}
	// The first volume of its own is the main disk, never the shared one.
	disk, rest := services[1].Disk()
	if disk == nil || disk.Path != "/config" || len(rest.Volumes) != 1 || rest.Volumes[0].Disk != "media" {
		t.Fatal(disk, rest.Volumes)
	}
	// A package rebuilt from the store carries the size on each volume.
	carried := Package{Type: Type, Version: Version, Name: "x", Services: services}
	if problems := Check(carried); len(problems) > 0 {
		t.Fatal(problems)
	}
}

func TestSharedDiskRules(t *testing.T) {
	for label, change := range map[string][2]string{
		"inconnu": {"disk: media\n", "disk: films\n"},
		"taille":  {"      - path: /data\n        disk: media\n  - name: sonarr", "      - path: /data\n        disk: media\n        sizeGi: 5\n  - name: sonarr"},
		"copies":  {"    port: 8989\n", "    port: 8989\n    replicas: 2\n"},
		"nom":     {"  - name: media\n", "  - name: Media\n"},
		"inutile": {"disks:\n", "disks:\n  - name: spare\n    sizeGi: 1\n"},
	} {
		text := strings.Replace(mediaPackage, change[0], change[1], 1)
		if text == mediaPackage {
			t.Fatalf("%s : le changement ne s'applique pas", label)
		}
		if _, problems := Parse([]byte(text)); len(problems) == 0 {
			t.Fatalf("%s accepté", label)
		}
	}
}

func TestComposeSharesAVolumeMountedTwice(t *testing.T) {
	p, warnings, err := Compose([]byte(`services:
  jellyfin:
    image: jellyfin/jellyfin:10.10.7
    ports: ["8096:8096"]
    volumes: ["./config:/config", "media:/data"]
  sonarr:
    image: linuxserver/sonarr:4.0.20
    volumes: ["media:/data", "sonarr-config:/config"]
volumes:
  media: {}
  sonarr-config: {}
`), "media")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Disks) != 1 || p.Disks[0].Name != "media" {
		t.Fatal(p.Disks, warnings)
	}
	shared := 0
	for _, svc := range p.Services {
		for _, volume := range svc.Volumes {
			if volume.Disk == "media" {
				shared++
			}
		}
	}
	if shared != 2 {
		t.Fatal(shared)
	}
	if problems := Check(p); len(problems) > 0 {
		t.Fatal(problems)
	}
}
