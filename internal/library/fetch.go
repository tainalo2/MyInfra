package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultRepo is the official library, the one MyInfra knows.
const DefaultRepo = "tainalo2/MyInfra-templates-and-packages-library"

// API is the GitHub API; tests point it elsewhere.
var API = "https://api.github.com"

// Repo is where the library is published. MYINFRA_LIBRARY_REPO points at a
// fork, to try a library before it is released.
func Repo() string {
	if repo := strings.TrimSpace(os.Getenv("MYINFRA_LIBRARY_REPO")); repo != "" {
		return repo
	}
	return DefaultRepo
}

// Latest is the newest release of the library, its version and where its
// files are.
func Latest(ctx context.Context, repo string) (string, map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := get(ctx, API+"/repos/"+repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return "", nil, fmt.Errorf("réponse de GitHub illisible")
	}
	assets := map[string]string{}
	for _, asset := range rel.Assets {
		assets[asset.Name] = asset.URL
	}
	return strings.TrimPrefix(rel.Tag, "v"), assets, nil
}

// Download fetches the files of a release. Nothing is checked yet: Open
// does.
func Download(ctx context.Context, release string, assets map[string]string) (Bundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	b := Bundle{Release: release}
	for _, file := range []struct {
		name  string
		limit int64
		into  *[]byte
	}{
		{SumsName, 1 << 20, &b.Sums},
		{SignatureName, 1 << 10, &b.Signature},
		{ArchiveName, MaxArchive, &b.Archive},
	} {
		url := assets[file.name]
		if url == "" {
			return b, fmt.Errorf("la version %s ne contient pas %s", release, file.name)
		}
		resp, err := get(ctx, url, "")
		if err != nil {
			return b, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, file.limit+1))
		resp.Body.Close()
		if err != nil {
			return b, fmt.Errorf("le téléchargement a échoué")
		}
		if int64(len(raw)) > file.limit {
			return b, fmt.Errorf("%s est trop grand", file.name)
		}
		*file.into = raw
	}
	return b, nil
}

func get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "MyInfra")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub ne répond pas")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub répond %d", resp.StatusCode)
	}
	return resp, nil
}
