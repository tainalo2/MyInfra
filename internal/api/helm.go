package api

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"myinfra/internal/tools"
)

// helmSource is a chart to render: a name in a repository, or an OCI
// reference, with the values a person gives it.
type helmSource struct {
	Repo    string `json:"repo"`
	Chart   string `json:"chart"`
	Version string `json:"version"`
	Values  string `json:"values"`
}

// helmNamespace is the namespace charts are rendered for: the addresses
// under it become the names of the package's services.
const helmNamespace = "myinfra"

var (
	chartName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,100}$`)
	chartOCI     = regexp.MustCompile(`^oci://[A-Za-z0-9][A-Za-z0-9._/:@-]{0,300}$`)
	chartVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,63}$`)
	chartRepo    = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._~/%-]*)?$`)
)

// errHelmMissing asks the screen to offer Helm's installation.
var errHelmMissing = fmt.Errorf("Helm n'est pas installé.")

// renderChart runs helm template on a chart, in a folder of its own: no
// setting nor login of the machine's Helm is read. It returns the manifests
// and the chart's name.
func renderChart(ctx context.Context, src helmSource) ([]byte, string, error) {
	helm := tools.Helm()
	if helm == "" {
		return nil, "", errHelmMissing
	}
	chart := strings.TrimSpace(src.Chart)
	repo := strings.TrimRight(strings.TrimSpace(src.Repo), "/")
	version := strings.TrimSpace(src.Version)
	base := chart
	switch {
	case chartOCI.MatchString(chart) && !strings.Contains(chart, ".."):
		if repo != "" {
			return nil, "", fmt.Errorf("Une référence oci:// se donne seule, sans dépôt.")
		}
		base = chart[strings.LastIndex(chart, "/")+1:]
		if at := strings.IndexAny(base, ":@"); at > 0 {
			base = base[:at]
		}
	case chartName.MatchString(chart):
		if !chartRepo.MatchString(repo) || strings.Contains(repo, "..") {
			return nil, "", fmt.Errorf("Indiquez l'adresse https:// du dépôt du chart.")
		}
	default:
		return nil, "", fmt.Errorf("Indiquez le nom du chart, ou sa référence oci://.")
	}
	if version != "" && !chartVersion.MatchString(version) {
		return nil, "", fmt.Errorf("La version du chart est illisible.")
	}
	if len(src.Values) > 256<<10 {
		return nil, "", fmt.Errorf("Les valeurs font au plus 256 Kio.")
	}
	if strings.TrimSpace(src.Values) != "" {
		var values map[string]any
		if err := yaml.Unmarshal([]byte(src.Values), &values); err != nil {
			return nil, "", fmt.Errorf("Les valeurs ne sont pas du YAML lisible.")
		}
	}
	dir, err := os.MkdirTemp("", "myinfra-helm-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	valuesFile := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(valuesFile, []byte(src.Values), 0o600); err != nil {
		return nil, "", err
	}
	release := strings.Trim(strings.ToLower(regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(base), "-")), "-")
	if release == "" {
		release = "app"
	}
	if len(release) > 53 {
		release = release[:53]
	}
	args := []string{"template", release, chart, "--namespace", helmNamespace, "--skip-tests", "-f", valuesFile}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	if version != "" {
		args = append(args, "--version", version)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, helm, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HELM_CACHE_HOME="+filepath.Join(dir, "cache"),
		"HELM_CONFIG_HOME="+filepath.Join(dir, "config"),
		"HELM_DATA_HOME="+filepath.Join(dir, "data"),
	)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if len(message) > 400 {
			message = message[len(message)-400:]
		}
		if message == "" {
			message = err.Error()
		}
		return nil, "", fmt.Errorf("Helm n'a pas pu lire le chart : %s", message)
	}
	if out.Len() > 8<<20 {
		return nil, "", fmt.Errorf("Le chart rendu dépasse 8 Mio.")
	}
	return out.Bytes(), base, nil
}
