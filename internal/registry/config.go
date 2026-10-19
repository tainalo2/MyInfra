package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Image is what an image declares about itself: the TCP ports it exposes and
// the folders it keeps. Both come from EXPOSE and VOLUME in its Dockerfile.
type Image struct {
	Ports   []int
	Volumes []string
}

const manifestTypes = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// Inspect reads the configuration of a public image, linux/amd64 first.
func Inspect(ctx context.Context, image string) (Image, error) {
	ref, err := Parse(image)
	if err != nil {
		return Image{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second}
	base := "https://" + registryHost(ref.Host) + "/v2/" + ref.Repo
	token := ""
	var doc struct {
		MediaType string `json:"mediaType"`
		Config    struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if token, err = fetchJSON(ctx, client, base+"/manifests/"+ref.Tag, manifestTypes, token, &doc); err != nil {
		return Image{}, err
	}
	if doc.Config.Digest == "" && len(doc.Manifests) > 0 {
		digest := doc.Manifests[0].Digest
		for _, item := range doc.Manifests {
			if item.Platform.OS == "linux" && item.Platform.Architecture == "amd64" {
				digest = item.Digest
				break
			}
		}
		doc.Manifests = nil
		if token, err = fetchJSON(ctx, client, base+"/manifests/"+digest, manifestTypes, token, &doc); err != nil {
			return Image{}, err
		}
	}
	if doc.Config.Digest == "" {
		return Image{}, fmt.Errorf("registre illisible")
	}
	var config struct {
		Config struct {
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
			Volumes      map[string]struct{} `json:"Volumes"`
		} `json:"config"`
	}
	if _, err := fetchJSON(ctx, client, base+"/blobs/"+doc.Config.Digest, "application/json", token, &config); err != nil {
		return Image{}, err
	}
	var out Image
	for key := range config.Config.ExposedPorts {
		number, proto, _ := strings.Cut(key, "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if port, err := strconv.Atoi(number); err == nil && port > 0 && port < 65536 {
			out.Ports = append(out.Ports, port)
		}
	}
	for path := range config.Config.Volumes {
		out.Volumes = append(out.Volumes, path)
	}
	sort.Ints(out.Ports)
	sort.Strings(out.Volumes)
	return out, nil
}

// Port picks the port a web service most likely answers on.
func (img Image) Port() int {
	for _, preferred := range []int{80, 8080, 3000, 8000} {
		for _, port := range img.Ports {
			if port == preferred {
				return port
			}
		}
	}
	for _, port := range img.Ports {
		if port != 443 {
			return port
		}
	}
	return 0
}

func fetchJSON(ctx context.Context, client *http.Client, rawURL, accept, token string, into any) (string, error) {
	get := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "MyInfra")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("registre indisponible")
		}
		return resp, nil
	}
	resp, err := get(token)
	if err != nil {
		return token, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		next, authErr := tokenFrom(ctx, client, resp.Header.Get("WWW-Authenticate"))
		resp.Body.Close()
		if authErr != nil {
			return token, authErr
		}
		token = next
		if resp, err = get(token); err != nil {
			return token, err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return token, fmt.Errorf("image introuvable")
	}
	if resp.StatusCode != http.StatusOK {
		return token, fmt.Errorf("registre indisponible")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(into); err != nil {
		return token, fmt.Errorf("registre illisible")
	}
	return token, nil
}
