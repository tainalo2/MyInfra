package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"myinfra/internal/pack"
	"myinfra/internal/store"
)

// sourceLimit is what read_source returns of a file: a compose file, its
// env files, a README fit; a whole book does not.
const sourceLimit = 200 << 10

func readSourceTool() mcpTool {
	return mcpTool{
		Name: "read_source", Title: "Lire un fichier en ligne", Role: store.RoleDeploy,
		Description: "Lit un fichier texte public en https (docker-compose, fichier d'environnement, README, values.yaml), ou liste un dossier d'un dépôt GitHub (github.com/o/r, github.com/o/r/tree/main/docs). Un lien vers la page d'un fichier sur GitHub ou GitLab lit le fichier. Pour adapter un projet : son compose de production, ses fichiers d'environnement, sa documentation d'installation.",
		Schema:      object([]string{"url"}, map[string]any{"url": text("Adresse https du fichier, ou d'un dépôt ou dossier GitHub.")}),
		Routes:      []string{"POST /api/packages/source"},
		Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
			args, err := decodeArgs[struct{ URL string }](raw)
			if err != nil {
				return "", err
			}
			var out struct {
				Text string `json:"text"`
			}
			if err := c.send(http.MethodPost, "/api/packages/source", map[string]string{"url": args.URL}, &out); err != nil {
				return "", err
			}
			return out.Text, nil
		},
	}
}

// readPackageSource reads a public file, or lists a GitHub folder, for the
// assistant adapting a project into a package.
func (a *API) readPackageSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	text, err := readSource(r.Context(), strings.TrimSpace(body.URL))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

// packageGuide is the guide to the package format, as the MCP resource.
func (a *API) packageGuide(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"text": pack.Guide})
}

func readSource(ctx context.Context, link string) (string, error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", failf("Donnez une adresse https://.")
	}
	if api, ok := githubFolder(u); ok {
		return listGitHub(ctx, api)
	}
	u, _ = url.Parse(rawLink(link))
	body, kind, err := publicGet(ctx, u.String(), "")
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(kind, "text/html") {
		return "", failf("%s est une page web, pas un fichier : donnez le lien du fichier brut, ou d'un dossier GitHub pour en voir les fichiers.", link)
	}
	if !utf8.Valid(body) {
		return "", failf("%s n'est pas un fichier texte.", link)
	}
	text := string(body)
	if len(body) > sourceLimit {
		text = strings.ToValidUTF8(text[:sourceLimit], "") + "\n\n[… fichier coupé : il dépasse 200 Kio]"
	}
	return text, nil
}

// githubFolder reads github.com/o/r and github.com/o/r/tree/ref/path as the
// GitHub API that lists them.
func githubFolder(u *url.URL) (string, bool) {
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) == 2:
		return fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/", parts[0], parts[1]), true
	case len(parts) >= 4 && parts[2] == "tree":
		path := strings.Join(parts[4:], "/")
		return fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", parts[0], parts[1], path, url.QueryEscape(parts[3])), true
	}
	return "", false
}

func listGitHub(ctx context.Context, api string) (string, error) {
	body, _, err := publicGet(ctx, api, "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	var entries []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Size     int    `json:"size"`
		HTMLURL  string `json:"html_url"`
		Download string `json:"download_url"`
	}
	if json.Unmarshal(body, &entries) != nil {
		return "", failf("Ce n'est pas un dossier : donnez le lien du fichier.")
	}
	var lines []string
	for _, e := range entries {
		if e.Type == "dir" {
			lines = append(lines, fmt.Sprintf("dossier %s/ : %s", e.Name, e.HTMLURL))
		} else {
			lines = append(lines, fmt.Sprintf("fichier %s (%d o) : %s", e.Name, e.Size, e.Download))
		}
	}
	if len(lines) == 0 {
		return "Le dossier est vide.", nil
	}
	return strings.Join(lines, "\n"), nil
}

// publicClient reaches only public addresses: the tool reads what anyone
// may read, never the machine's own network.
var publicClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || !publicIP(ip) {
					return fmt.Errorf("adresse non publique : %s", host)
				}
				return nil
			},
		}).DialContext,
	},
}

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast())
}

func publicGet(ctx context.Context, link, accept string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", failf("Adresse illisible : %s.", link)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := publicClient.Do(req)
	if err != nil {
		return nil, "", failf("%s est injoignable.", link)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", failf("%s répond %s.", link, res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, sourceLimit+1))
	if err != nil {
		return nil, "", failf("%s est injoignable.", link)
	}
	return body, res.Header.Get("Content-Type"), nil
}
