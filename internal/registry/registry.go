package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type Ref struct {
	Host string
	Repo string
	Tag  string
	Name string
}

func Parse(image string) (Ref, error) {
	image = strings.TrimSpace(image)
	if image == "" || strings.Contains(image, " ") || strings.Contains(image, "..") {
		return Ref{}, fmt.Errorf("image illisible")
	}
	if at := strings.LastIndex(image, "@"); at > 0 {
		image = image[:at]
	}
	name := image
	tag := "latest"
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		name = image[:colon]
		tag = image[colon+1:]
	}
	if !tagPattern.MatchString(tag) || name == "" {
		return Ref{}, fmt.Errorf("image illisible")
	}
	host := "docker.io"
	repo := name
	if first, rest, ok := strings.Cut(name, "/"); ok && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost") {
		host = first
		repo = rest
	}
	if host == "docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	if repo == "" || strings.HasPrefix(repo, "/") {
		return Ref{}, fmt.Errorf("image illisible")
	}
	return Ref{Host: host, Repo: repo, Tag: tag, Name: name}, nil
}

func WithTag(image, tag string) (string, error) {
	if !tagPattern.MatchString(tag) {
		return "", fmt.Errorf("version illisible")
	}
	ref, err := Parse(image)
	if err != nil {
		return "", err
	}
	return ref.Name + ":" + tag, nil
}

func Tag(image string) string {
	ref, err := Parse(image)
	if err != nil {
		return ""
	}
	return ref.Tag
}

// tagsFresh is how long the tags of a repository are kept: they change
// rarely, and asking the registry takes one to several seconds.
const tagsFresh = 15 * time.Minute

type tagsHit struct {
	at   time.Time // when tags were last read with success
	tags []string
	err  error         // last failure, when no tags were ever read
	done chan struct{} // reading in flight
}

var (
	tagsMu    sync.Mutex
	tagsCache = map[string]*tagsHit{}
)

// Tags lists the versions of an image's repository. A stale list is served at
// once and read again behind; pages asking at the same time share one reading.
func Tags(ctx context.Context, image string) ([]string, error) {
	ref, err := Parse(image)
	if err != nil {
		return nil, err
	}
	key := ref.Host + "/" + ref.Repo
	tagsMu.Lock()
	hit := tagsCache[key]
	if hit == nil {
		hit = &tagsHit{}
		tagsCache[key] = hit
	}
	if hit.done == nil && (hit.at.IsZero() || time.Since(hit.at) >= tagsFresh) {
		hit.done = make(chan struct{})
		go readInto(hit, ref)
	}
	if !hit.at.IsZero() {
		tags := hit.tags
		tagsMu.Unlock()
		return tags, nil
	}
	done := hit.done
	tagsMu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	tagsMu.Lock()
	defer tagsMu.Unlock()
	if hit.at.IsZero() {
		return nil, hit.err
	}
	return hit.tags, nil
}

func readInto(hit *tagsHit, ref Ref) {
	tags, err := readTags(ref)
	tagsMu.Lock()
	if err == nil {
		hit.tags, hit.at, hit.err = tags, time.Now(), nil
	} else if hit.at.IsZero() {
		hit.err = err
	}
	done := hit.done
	hit.done = nil
	tagsMu.Unlock()
	close(done)
}

func readTags(ref Ref) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second}
	raw, err := listTags(ctx, client, "https://"+registryHost(ref.Host), ref.Repo)
	if err != nil {
		return nil, err
	}
	return Choose(raw), nil
}

func registryHost(host string) string {
	if host == "docker.io" || host == "index.docker.io" {
		return "registry-1.docker.io"
	}
	return host
}

func listTags(ctx context.Context, client *http.Client, base, repo string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	next := base + "/v2/" + repo + "/tags/list?n=100"
	for page := 0; page < 4 && next != ""; page++ {
		body, link, err := getTags(ctx, client, next)
		if err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, err
		}
		for _, tag := range body {
			if tag == "" || seen[tag] || !tagPattern.MatchString(tag) {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
		next = link
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("aucune version")
	}
	return out, nil
}

func getTags(ctx context.Context, client *http.Client, rawURL string) ([]string, string, error) {
	resp, err := do(ctx, client, rawURL, "")
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		auth, authErr := authFrom(ctx, client, rawURL, resp.Header.Get("WWW-Authenticate"))
		resp.Body.Close()
		if authErr != nil {
			return nil, "", authErr
		}
		resp, err = do(ctx, client, rawURL, auth)
		if err != nil {
			return nil, "", err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("image introuvable")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registre indisponible")
	}
	var payload struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, "", fmt.Errorf("registre illisible")
	}
	return payload.Tags, nextLink(resp.Header.Get("Link"), rawURL), nil
}

func do(ctx context.Context, client *http.Client, rawURL, auth string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MyInfra")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registre indisponible")
	}
	return resp, nil
}

// Credentials gives the login MyInfra holds for a registry host, if any. A
// registry without one is read anonymously, as public images are.
var Credentials func(host string) (user, password string, ok bool)

// loginFor finds the login of the registry rawURL points at.
func loginFor(rawURL string) (string, string, bool) {
	if Credentials == nil {
		return "", "", false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}
	host := parsed.Host
	if host == "registry-1.docker.io" || host == "index.docker.io" {
		host = "docker.io"
	}
	return Credentials(host)
}

func basic(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// authFrom answers the challenge of a registry: Basic with the login held for
// it, or Bearer with a token asked with that login, anonymously without one.
func authFrom(ctx context.Context, client *http.Client, rawURL, header string) (string, error) {
	user, password, login := loginFor(rawURL)
	if strings.HasPrefix(header, "Basic") {
		if !login {
			return "", fmt.Errorf("Ce registre demande un identifiant.")
		}
		return basic(user, password), nil
	}
	tokenAuth := ""
	if login {
		tokenAuth = basic(user, password)
	}
	token, err := tokenFrom(ctx, client, header, tokenAuth)
	if err != nil {
		return "", err
	}
	return "Bearer " + token, nil
}

func tokenFrom(ctx context.Context, client *http.Client, header, auth string) (string, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", fmt.Errorf("registre indisponible")
	}
	realm := quoted(header, "realm")
	service := quoted(header, "service")
	scope := quoted(header, "scope")
	if realm == "" {
		return "", fmt.Errorf("registre indisponible")
	}
	query := url.Values{}
	if service != "" {
		query.Set("service", service)
	}
	if scope != "" {
		query.Set("scope", scope)
	}
	target := realm
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	resp, err := do(ctx, client, target, auth)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("Identifiant refusé par le registre.")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registre indisponible")
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", fmt.Errorf("registre illisible")
	}
	if payload.Token == "" {
		payload.Token = payload.AccessToken
	}
	if payload.Token == "" {
		return "", fmt.Errorf("registre indisponible")
	}
	return payload.Token, nil
}

func quoted(header, key string) string {
	needle := key + `="`
	at := strings.Index(header, needle)
	if at < 0 {
		return ""
	}
	rest := header[at+len(needle):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func nextLink(header, current string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		ref, err := url.Parse(part[start+1 : end])
		parent, parentErr := url.Parse(current)
		if err != nil || parentErr != nil {
			return ""
		}
		return parent.ResolveReference(ref).String()
	}
	return ""
}

func Choose(tags []string) []string {
	seen := map[string]bool{}
	var kept []string
	for _, tag := range tags {
		if tag == "" || seen[tag] || !tagPattern.MatchString(tag) || skipTag(tag) {
			continue
		}
		seen[tag] = true
		kept = append(kept, tag)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return lessTag(kept[i], kept[j])
	})
	if len(kept) > 40 {
		kept = kept[:40]
	}
	return kept
}

func skipTag(tag string) bool {
	if strings.HasPrefix(tag, "sha256-") || strings.HasPrefix(tag, "sha-") {
		return true
	}
	if len(tag) >= 40 && isHex(tag) {
		return true
	}
	if tag == "latest" || tag == "stable" || tag == "lts" {
		return false
	}
	for _, char := range tag {
		if char >= '0' && char <= '9' {
			return false
		}
	}
	return true
}

func isHex(tag string) bool {
	for _, char := range tag {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func lessTag(a, b string) bool {
	if a == "latest" {
		return true
	}
	if b == "latest" {
		return false
	}
	av, aok := parseVer(a)
	bv, bok := parseVer(b)
	if aok && bok {
		if cmp := compareVer(av, bv); cmp != 0 {
			return cmp > 0
		}
	}
	if aok != bok {
		return aok
	}
	return a < b
}

type version struct {
	major  int
	minor  int
	patch  int
	flavor string
}

func parseVer(tag string) (version, bool) {
	raw := tag
	if strings.HasPrefix(raw, "v") && len(raw) > 1 && raw[1] >= '0' && raw[1] <= '9' {
		raw = raw[1:]
	}
	read := func(value string) (int, string, bool) {
		if value == "" || value[0] < '0' || value[0] > '9' {
			return 0, value, false
		}
		n := 0
		i := 0
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			n = n*10 + int(value[i]-'0')
			i++
			if n > 1_000_000 {
				return 0, value, false
			}
		}
		return n, value[i:], true
	}
	major, rest, ok := read(raw)
	if !ok {
		return version{}, false
	}
	out := version{major: major}
	if strings.HasPrefix(rest, ".") {
		minor, next, mok := read(rest[1:])
		if !mok {
			return version{}, false
		}
		out.minor = minor
		rest = next
		if strings.HasPrefix(rest, ".") {
			patch, next, pok := read(rest[1:])
			if !pok {
				return version{}, false
			}
			out.patch = patch
			rest = next
		}
	}
	out.flavor = rest
	return out, true
}

func compareVer(a, b version) int {
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	return a.patch - b.patch
}

func Suggest(current string, tags []string) string {
	cur, err := Parse(current)
	if err != nil {
		return ""
	}
	currentVer, ok := parseVer(cur.Tag)
	best := ""
	var bestVer version
	for _, tag := range tags {
		if tag == cur.Tag || tag == "latest" {
			continue
		}
		next, nok := parseVer(tag)
		if !nok || next.flavor != currentVer.flavor {
			continue
		}
		if ok && compareVer(next, currentVer) <= 0 {
			continue
		}
		if !ok {
			continue
		}
		if best == "" || compareVer(next, bestVer) > 0 {
			best = tag
			bestVer = next
		}
	}
	if best != "" {
		return best
	}
	if cur.Tag == "latest" {
		return ""
	}
	hasLatest := false
	hasCurrent := false
	for _, tag := range tags {
		if tag == "latest" {
			hasLatest = true
		}
		if tag == cur.Tag {
			hasCurrent = true
		}
	}
	if hasLatest && !hasCurrent {
		return "latest"
	}
	return ""
}

// Check tries a login on a registry, the way a pull would use it.
func Check(ctx context.Context, host, user, password string) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second}
	base := "https://" + registryHost(host) + "/v2/"
	resp, err := do(ctx, client, base, basic(user, password))
	if err != nil {
		return fmt.Errorf("Ce registre ne répond pas à l'adresse %s.", host)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized && strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer"):
		_, err := tokenFrom(ctx, client, resp.Header.Get("WWW-Authenticate"), basic(user, password))
		return err
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("Identifiant refusé par le registre.")
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("Ce n'est pas un registre d'images : %s.", host)
	}
	return fmt.Errorf("registre indisponible")
}
