package deploy

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestKubectlApplyKeepsTheManifestOutOfTheShell(t *testing.T) {
	manifest := "kind: ConfigMap\nEOF\ntouch /tmp/pwned\n"
	command := KubectlApply(manifest)
	if strings.Contains(command, "touch") || strings.Contains(command, "\nEOF\n") {
		t.Fatal(command)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(manifest))
	if !strings.Contains(command, encoded) || !strings.Contains(command, "base64 -d") {
		t.Fatal(command)
	}
}
