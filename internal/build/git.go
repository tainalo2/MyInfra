package build

import (
	"fmt"
	"net/url"
	"strings"
)

// Spec is a Git repository MyInfra builds with docker on a server.
type Spec struct {
	Repo       string
	Branch     string
	Dockerfile string
	Context    string
}

// Parse reads a source string:
//
//	https://github.com/org/repo
//	https://github.com/org/repo@main
//	https://github.com/org/repo@main:docker/Dockerfile
func Parse(raw string) (Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Spec{}, fmt.Errorf("Indiquez l'adresse du dépôt Git.")
	}
	branch := "main"
	dockerfile := "Dockerfile"
	if at := strings.LastIndex(raw, "@"); at > strings.Index(raw, "://")+2 {
		tail := raw[at+1:]
		raw = raw[:at]
		if colon := strings.Index(tail, ":"); colon >= 0 {
			branch = strings.TrimSpace(tail[:colon])
			dockerfile = strings.TrimSpace(tail[colon+1:])
		} else {
			branch = strings.TrimSpace(tail)
		}
	}
	if branch == "" {
		branch = "main"
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Spec{}, fmt.Errorf("L'adresse du dépôt Git est illisible.")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return Spec{}, fmt.Errorf("Utilisez une adresse https:// vers le dépôt.")
	}
	path := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
	if path == "" || !strings.Contains(path, "/") {
		return Spec{}, fmt.Errorf("L'adresse doit pointer vers un dépôt (org/nom).")
	}
	u.Path = path
	u.RawQuery, u.Fragment = "", ""
	return Spec{Repo: u.String(), Branch: branch, Dockerfile: dockerfile, Context: "."}, nil
}

// CloneURL adds credentials for HTTPS clone when token is non-empty.
func CloneURL(repo, token string) string {
	if token == "" {
		return repo + ".git"
	}
	u, err := url.Parse(repo)
	if err != nil {
		return repo
	}
	if strings.Contains(u.Host, "github.com") {
		u.User = url.UserPassword("x-access-token", token)
	} else {
		u.User = url.UserPassword("oauth2", token)
	}
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path += ".git"
	}
	return u.String()
}

// RemoteScript builds on the server and streams a docker save on stdout.
func RemoteScript(spec Spec, cloneURL, tag string) string {
	cloneURL = strings.ReplaceAll(cloneURL, "'", "'\\''")
	tag = strings.ReplaceAll(tag, "'", "'\\''")
	df := strings.ReplaceAll(spec.Dockerfile, "'", "'\\''")
	ctx := strings.ReplaceAll(spec.Context, "'", "'\\''")
	branch := strings.ReplaceAll(spec.Branch, "'", "'\\''")
	return fmt.Sprintf(`set -eu
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
git clone --depth 1 --branch '%s' '%s' "$tmpdir/src"
cd "$tmpdir/src/%s"
docker build -f '%s' -t '%s' .
docker save '%s'`, branch, cloneURL, ctx, df, tag, tag)
}
