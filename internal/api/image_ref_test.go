package api

import "testing"

func TestAcceptableImageRejectsAPageAddress(t *testing.T) {
	if acceptableImage("https://hub.docker.com/r/ghostfolio/ghostfolio") {
		t.Fatal("adresse de page")
	}
	if acceptableImage("ghostfolio/ghostfolio:2.168.0/../../etc") {
		t.Fatal("chemin")
	}
	if !acceptableImage("ghostfolio/ghostfolio:2.168.0") || !acceptableImage("postgres:16") {
		t.Fatal("chemin docker hub")
	}
}

func TestNormalizeServicesKeepsWhereTheServiceListens(t *testing.T) {
	body := []serviceBody{{Name: "metube", Image: "alexta69/metube:latest", Published: true, Disk: true, Listen: 8081, DiskPath: "/downloads", DiskGi: 16}}
	out, _, err := normalizeServices("metube", "image", "alexta69/metube:latest", 1, map[string]string{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Listen != 8081 || out[0].DiskPath != "/downloads" || out[0].DiskGi != 16 {
		t.Fatalf("service %+v", out[0])
	}
	body[0].Listen = 70000
	if _, _, err := normalizeServices("metube", "image", "alexta69/metube:latest", 1, map[string]string{}, body); err == nil {
		t.Fatal("port hors limite")
	}
}
