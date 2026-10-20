package registry

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Digest reads what a tag points at now. A tag such as postgres:16 or latest
// moves when its publisher builds it again, without changing its name.
func Digest(ctx context.Context, image string) (string, error) {
	ref, err := Parse(image)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second}
	return headDigest(ctx, client, "https://"+registryHost(ref.Host)+"/v2/"+ref.Repo+"/manifests/"+ref.Tag)
}

func headDigest(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	head := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", manifestTypes)
		req.Header.Set("User-Agent", "MyInfra")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("registre indisponible")
		}
		resp.Body.Close()
		return resp, nil
	}
	resp, err := head("")
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		auth, authErr := authFrom(ctx, client, rawURL, resp.Header.Get("WWW-Authenticate"))
		if authErr != nil {
			return "", authErr
		}
		if resp, err = head(auth); err != nil {
			return "", err
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("image introuvable")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registre indisponible")
	}
	digest := strings.TrimSpace(resp.Header.Get("Docker-Content-Digest"))
	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("registre illisible")
	}
	return digest, nil
}

// Pinned is the image fixed to one build of its tag: the nodes pull exactly
// that build, and an image whose tag did not change is still delivered again.
func Pinned(image, digest string) (string, error) {
	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("empreinte illisible")
	}
	ref, err := Parse(image)
	if err != nil {
		return "", err
	}
	return ref.Name + ":" + ref.Tag + "@" + digest, nil
}

// DigestOf is the build an image is pinned to, empty when it follows its tag.
func DigestOf(image string) string {
	if at := strings.LastIndex(image, "@"); at > 0 && digestPattern.MatchString(image[at+1:]) {
		return image[at+1:]
	}
	return ""
}

// Unpinned is the image as people write it, without the build it is fixed to.
func Unpinned(image string) string {
	if DigestOf(image) != "" {
		return image[:strings.LastIndex(image, "@")]
	}
	return image
}

// Level says how far a new version goes from the current one: major, minor
// or patch, from the numbers of the two tags. A tag without numbers, such as
// latest, counts as minor.
func Level(current, next string) string {
	from, fok := parseVer(Tag(current))
	to, tok := parseVer(Tag(next))
	switch {
	case !fok || !tok:
		return "minor"
	case to.major != from.major:
		return "major"
	case to.minor != from.minor:
		return "minor"
	default:
		return "patch"
	}
}
