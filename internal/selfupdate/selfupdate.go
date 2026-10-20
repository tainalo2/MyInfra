// Package selfupdate finds the releases of MyInfra and replaces a running
// executable by a newer one, checked against the sums the release publishes.
package selfupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is where releases are published. MYINFRA_UPDATE_REPO points at a fork.
func Repo() string {
	if repo := strings.TrimSpace(os.Getenv("MYINFRA_UPDATE_REPO")); repo != "" {
		return repo
	}
	return "tainalo2/MyInfra"
}

// Enabled is false when the person turned the check off.
func Enabled() bool {
	return os.Getenv("MYINFRA_UPDATE_CHECK") != "0"
}

// API is the GitHub API; tests point it elsewhere.
var API = "https://api.github.com"

type Release struct {
	Version    string            `json:"version"`
	Tag        string            `json:"tag"`
	URL        string            `json:"url"`
	Notes      string            `json:"notes"`
	Published  time.Time         `json:"published"`
	Prerelease bool              `json:"prerelease"`
	Assets     map[string]string `json:"-"`
}

// Latest is the newest release: a stable one, or also a prerelease when the
// running copy is one.
func Latest(ctx context.Context, current string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, API+"/repos/"+Repo()+"/releases?per_page=20", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MyInfra")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("GitHub ne répond pas")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub répond %d", resp.StatusCode)
	}
	var listed []struct {
		Tag        string    `json:"tag_name"`
		URL        string    `json:"html_url"`
		Body       string    `json:"body"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Published  time.Time `json:"published_at"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&listed); err != nil {
		return Release{}, fmt.Errorf("réponse de GitHub illisible")
	}
	withPre := strings.Contains(current, "-")
	var best Release
	for _, item := range listed {
		version := strings.TrimPrefix(item.Tag, "v")
		if item.Draft || (item.Prerelease && !withPre) || !Valid(version) {
			continue
		}
		if best.Version != "" && !Newer(version, best.Version) {
			continue
		}
		best = Release{Version: version, Tag: item.Tag, URL: item.URL, Notes: item.Body, Published: item.Published, Prerelease: item.Prerelease, Assets: map[string]string{}}
		for _, asset := range item.Assets {
			best.Assets[asset.Name] = asset.URL
		}
	}
	if best.Version == "" {
		return Release{}, fmt.Errorf("aucune version publiée")
	}
	return best, nil
}

type semver struct {
	nums [3]int
	pre  string
}

func parse(version string) (semver, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	core, pre, _ := strings.Cut(version, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var out semver
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return semver{}, false
		}
		out.nums[i] = n
	}
	out.pre = pre
	return out, true
}

// Valid is true for a released version such as 1.4.0 or 1.4.0-rc.1.
func Valid(version string) bool {
	_, ok := parse(version)
	return ok
}

// Newer is true when version a comes after b. A development copy is never
// older than a release: it is not updated.
func Newer(a, b string) bool {
	x, xok := parse(a)
	y, yok := parse(b)
	if !xok || !yok {
		return false
	}
	for i := range x.nums {
		if x.nums[i] != y.nums[i] {
			return x.nums[i] > y.nums[i]
		}
	}
	switch {
	case x.pre == y.pre:
		return false
	case x.pre == "":
		return true
	case y.pre == "":
		return false
	default:
		return x.pre > y.pre
	}
}

// AssetName is the executable of a release for a system.
func AssetName(version, goos, goarch string) string {
	name := "myinfra_" + version + "_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Download fetches the executable of this system next to exe, checks it
// against SHA256SUMS, and returns its path. Nothing is replaced yet.
func Download(ctx context.Context, rel Release, exe string) (string, error) {
	name := AssetName(rel.Version, runtime.GOOS, runtime.GOARCH)
	binURL, sumsURL := rel.Assets[name], rel.Assets["SHA256SUMS"]
	if binURL == "" || sumsURL == "" {
		return "", fmt.Errorf("La version %s ne contient pas d'exécutable pour ce système.", rel.Version)
	}
	sums, err := fetch(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	want := sumFor(string(sums), name)
	if want == "" {
		return "", fmt.Errorf("SHA256SUMS ne cite pas %s.", name)
	}
	next := exe + ".new"
	if err := saveChecked(ctx, binURL, next, want); err != nil {
		return "", err
	}
	return next, nil
}

func fetch(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	resp, err := get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MyInfra")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Le téléchargement a échoué.")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Le téléchargement a échoué (%d).", resp.StatusCode)
	}
	return resp, nil
}

func sumFor(sums, name string) string {
	scanner := bufio.NewScanner(strings.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func saveChecked(ctx context.Context, rawURL, path, want string) error {
	resp, err := get(ctx, rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, 1<<30))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("Le téléchargement a échoué.")
	}
	if hex.EncodeToString(hash.Sum(nil)) != want {
		_ = os.Remove(path)
		return fmt.Errorf("Le fichier téléchargé ne correspond pas à SHA256SUMS : il a été écarté.")
	}
	return nil
}

// Check runs the new executable to read its version: it must start on this
// system and be the release it claims to be.
func Check(ctx context.Context, next, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, next, "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != version {
		return fmt.Errorf("Le nouvel exécutable ne démarre pas sur ce système.")
	}
	return nil
}

// Postgres is the major version of the PostgreSQL a release embeds, from its
// release.json. The releases before that file all embed PostgreSQL 16.
func Postgres(ctx context.Context, rel Release) (string, error) {
	url := rel.Assets["release.json"]
	if url == "" {
		return "16", nil
	}
	raw, err := fetch(ctx, url, 64<<10)
	if err != nil {
		return "", err
	}
	var meta struct {
		Postgres string `json:"postgres"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.Postgres == "" {
		return "", fmt.Errorf("release.json illisible")
	}
	return meta.Postgres, nil
}

// SamePostgres refuses a release whose PostgreSQL needs a migration of the data.
func SamePostgres(rel Release, have, want string) error {
	if have == "" || have == want {
		return nil
	}
	return fmt.Errorf("La version %s embarque PostgreSQL %s au lieu de %s : vos données demandent une migration. Exportez-les depuis Transfert, puis importez-les dans la nouvelle version.", rel.Version, want, have)
}

// Swap puts next in the place of exe and keeps the previous one as exe.old.
// A running executable can be renamed on every system, Windows included.
func Swap(exe, next string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("L'exécutable ne peut pas être remplacé : %s n'est pas modifiable.", filepath.Dir(exe))
	}
	if err := os.Rename(next, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("L'exécutable ne peut pas être remplacé.")
	}
	return nil
}
