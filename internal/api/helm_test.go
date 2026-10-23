package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeHelm puts a helm that writes its arguments and its settings folder,
// then prints the Wiki.js manifests.
func fakeHelm(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("script sh")
	}
	home := t.TempDir()
	t.Setenv("MYINFRA_HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs("../pack/testdata/wiki.helm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(home, "seen")
	script := "#!/bin/sh\necho \"$@\" > " + seen + "\necho \"$HELM_CONFIG_HOME\" >> " + seen + "\ncat " + fixture + "\n"
	if err := os.WriteFile(filepath.Join(home, "bin", "helm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return seen
}

func TestRenderChartRunsHelmAlone(t *testing.T) {
	seen := fakeHelm(t)
	out, chart, err := renderChart(context.Background(), helmSource{Repo: "https://charts.js.wiki/", Chart: "wiki", Version: "2.3.1", Values: "postgresql:\n  persistence:\n    size: 4Gi\n"})
	if err != nil {
		t.Fatal(err)
	}
	if chart != "wiki" || !strings.Contains(string(out), "kind: StatefulSet") {
		t.Fatal(chart)
	}
	raw, _ := os.ReadFile(seen)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, want := range []string{"template wiki wiki", "--namespace myinfra", "--skip-tests", "--repo https://charts.js.wiki", "--version 2.3.1", "-f "} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("missing %s in %s", want, lines[0])
		}
	}
	// Helm reads no setting of the machine: its folders are made for the run.
	if len(lines) < 2 || !strings.Contains(lines[1], "myinfra-helm-") {
		t.Fatal(lines)
	}
	oci, chart, err := renderChart(context.Background(), helmSource{Chart: "oci://ghcr.io/acme/charts/shop:1.2.0"})
	if err != nil || chart != "shop" || len(oci) == 0 {
		t.Fatal(chart, err)
	}
	for _, bad := range []helmSource{
		{Chart: "wiki"},
		{Chart: "wiki", Repo: "http://charts.js.wiki"},
		{Chart: "wiki; rm -rf /", Repo: "https://charts.js.wiki"},
		{Chart: "wiki", Repo: "https://charts.js.wiki", Version: "--set=x"},
		{Chart: "oci://ghcr.io/acme/shop", Repo: "https://charts.js.wiki"},
		{Chart: "wiki", Repo: "https://charts.js.wiki", Values: "a: [unclosed"},
	} {
		if _, _, err := renderChart(context.Background(), bad); err == nil {
			t.Fatalf("%+v is accepted", bad)
		}
	}
}

func TestRenderChartWithoutHelm(t *testing.T) {
	t.Setenv("MYINFRA_HOME", t.TempDir())
	t.Setenv("PATH", "/nonexistent")
	if _, _, err := renderChart(context.Background(), helmSource{Chart: "wiki", Repo: "https://charts.js.wiki"}); !errors.Is(err, errHelmMissing) {
		t.Fatal(err)
	}
}
