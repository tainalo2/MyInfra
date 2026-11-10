package build

import (
	"strings"
	"testing"
)

func TestParseGitSource(t *testing.T) {
	spec, err := Parse("https://github.com/acme/app@develop:deploy/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Branch != "develop" || spec.Dockerfile != "deploy/Dockerfile" || !strings.Contains(spec.Repo, "github.com/acme/app") {
		t.Fatalf("%+v", spec)
	}
	clone := CloneURL(spec.Repo, "tok")
	if !strings.Contains(clone, "x-access-token") {
		t.Fatal(clone)
	}
	script := RemoteScript(spec, clone, "tag")
	if !strings.Contains(script, "1>&2") {
		t.Fatalf("build output must be redirected to stderr so docker save stdout stays clean: %s", script)
	}
}
