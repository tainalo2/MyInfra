package api

import (
	"strings"
	"testing"

	"myinfra/internal/box"
)

func TestHealthOfReadsReadyCopiesAndCrashes(t *testing.T) {
	cases := []struct {
		replicas, waiting, want string
	}{
		{"1/1 1/1 ", "", "ready"},
		{"/1 ", "", "preparing"},
		{"0/1 ", "ContainerCreating ", "preparing"},
		{"0/1 ", "CrashLoopBackOff ", "failed"},
		{"", "", "preparing"},
		{"0/0 ", "", "ready"},
	}
	for _, c := range cases {
		if got := healthOf(c.replicas, c.waiting); got != c.want {
			t.Fatalf("%q %q: %s", c.replicas, c.waiting, got)
		}
	}
}

func TestReportedStatusesKeepsFailures(t *testing.T) {
	out := "Pulling\nmyinfra-status app1 running\nmyinfra-status app2 failed\nmyinfra-status app2 running\nmyinfra-status app3 bogus\n"
	got := reportedStatuses(out)
	if got["app1"] != "running" || got["app2"] != "failed" || got["app3"] != "" {
		t.Fatalf("%v", got)
	}
}

func TestSensitiveVariablesAreSealedAndMasked(t *testing.T) {
	key := make([]byte, 32)
	b, err := box.New(key)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{Box: b}
	sealed, err := a.sealVars(map[string]string{"MARIADB_PASSWORD": "s3cret", "TZ": "Europe/Paris"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed["MARIADB_PASSWORD"], sealedPrefix) || sealed["TZ"] != "Europe/Paris" {
		t.Fatalf("%v", sealed)
	}
	again, _ := a.sealVars(sealed)
	if again["MARIADB_PASSWORD"] != sealed["MARIADB_PASSWORD"] {
		t.Fatal("double chiffrement")
	}
	opened, err := a.openVars(sealed)
	if err != nil || opened["MARIADB_PASSWORD"] != "s3cret" {
		t.Fatal(opened, err)
	}
	if masked := maskVars(sealed); masked["MARIADB_PASSWORD"] != maskedValue || masked["TZ"] != "Europe/Paris" {
		t.Fatalf("%v", masked)
	}
	if shared := shareableVars(sealed); len(shared) != 1 || shared["TZ"] == "" {
		t.Fatalf("%v", shared)
	}
}

func TestAContainerMustFailSeveralTimesToBeShownFailed(t *testing.T) {
	a := &API{}
	for i := 1; i < failuresBeforeShown; i++ {
		if a.failedAgain("app") {
			t.Fatalf("échec montré au passage %d", i)
		}
	}
	if !a.failedAgain("app") {
		t.Fatal("échec répété non montré")
	}
	a.clearFailures("app")
	if a.failedAgain("app") {
		t.Fatal("compteur non remis à zéro")
	}
}

func TestAnsweredSeparatesTheProxyFromTheApplication(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{200, "", true},
		{302, "", true},
		{404, "404 page not found\n", false},
		{404, "<html>Not found</html>", true},
		{502, "Bad Gateway", false},
		{503, "", false},
	}
	for _, c := range cases {
		if got := answered(c.status, strings.NewReader(c.body)); got != c.want {
			t.Fatalf("%d %q: %v", c.status, c.body, got)
		}
	}
}
