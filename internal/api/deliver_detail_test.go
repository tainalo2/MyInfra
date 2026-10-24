package api

import (
	"strings"
	"testing"
)

func TestKubectlErrorKeepsTheRefusal(t *testing.T) {
	text := "namespace/mi41 unchanged\nservice/excalidraw configured\nThe Deployment \"excalidraw\" is invalid: spec.replicas: Invalid value: -1\n"
	if got := kubectlError(text); got != `The Deployment "excalidraw" is invalid: spec.replicas: Invalid value: -1` {
		t.Fatal(got)
	}
	if got := kubectlError("unexpected\nlast words\n"); got != "last words" {
		t.Fatal(got)
	}
}

func TestStuckReason(t *testing.T) {
	if got := stuckReason(" ContainerCreating CrashLoopBackOff "); got != "CrashLoopBackOff" {
		t.Fatal(got)
	}
	if got := stuckReason("ContainerCreating"); got != "" {
		t.Fatal(got)
	}
}

func TestStuckDetailPointsToTheCause(t *testing.T) {
	if got := stuckDetail("ImagePullBackOff"); !strings.Contains(got, "téléchargée") {
		t.Fatal(got)
	}
	if got := stuckDetail("CrashLoopBackOff"); !strings.Contains(got, "journaux") {
		t.Fatal(got)
	}
	if stuckDetail("") != "" {
		t.Fatal("no reason, no detail")
	}
}
