package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"myinfra/internal/catalog"
	"myinfra/internal/proc"
	"myinfra/internal/tools"
)

// Chaque machine est un répertoire de fichiers .tf. Le jeton est une variable,
// fournie seulement au moment d'exécuter OpenTofu.

func (c Client) tofuRoot() (string, error) {
	dir := c.TofuDir
	if dir == "" {
		return "", fmt.Errorf("OpenTofu n'est pas configuré.")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".plugins"), 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (c Client) stackDir(provider, id string) (string, error) {
	root, err := c.tofuRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, provider+"-"+stackID(id)), nil
}

func sameProvider(provider string, files map[string]string) error {
	want := map[string]string{
		"hetzner": "hetznercloud/hcloud", "scaleway": "scaleway/scaleway", "digitalocean": "digitalocean/digitalocean",
		"ovh": "ovh/ovh", "upcloud": "UpCloudLtd/upcloud", "vultr": "vultr/vultr", "akamai": "linode/linode",
		"exoscale": "exoscale/exoscale", "gcore": "G-Core/gcore", "kamatera": "Kamatera/kamatera",
		"ionos": "ionos-cloud/ionoscloud", "openstack": "terraform-provider-openstack/openstack",
		"infomaniak": "terraform-provider-openstack/openstack", "proxmox": "bpg/proxmox",
	}[provider]
	if want == "" {
		return fmt.Errorf("Provider inconnu.")
	}
	var body strings.Builder
	for _, file := range files {
		body.WriteString(file)
	}
	if !strings.Contains(body.String(), want) {
		return fmt.Errorf("Le provider de cette fiche ne change pas.")
	}
	return nil
}

func configFile(name string) bool {
	return strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".tf.json")
}

var purchaseKey = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

func stackID(id string) string {
	return strings.ReplaceAll(id, "/", "_")
}

func (c Client) createStack(ctx context.Context, provider string, creds catalog.Creds, spec Spec) (Machine, error) {
	root, err := c.tofuRoot()
	if err != nil {
		return Machine{}, err
	}
	work := filepath.Join(root, "pending-"+randomPass()[:8])
	if purchaseKey.MatchString(spec.Key) {
		// A purchase cut off before its end left its state here: applying the
		// same stack again finds that machine.
		work = filepath.Join(root, "pending-"+spec.Key)
	}
	spec, err = c.prepare(ctx, provider, creds, spec)
	if err != nil {
		return Machine{}, err
	}
	hcl, env, err := renderStack(provider, creds, spec, c)
	if err != nil {
		return Machine{}, err
	}
	if err := writeStack(work, map[string]string{"main.tf.json": hcl}); err != nil {
		return Machine{}, err
	}
	values, err := c.applyStack(ctx, work, env)
	if err != nil {
		// Without a state file nothing was bought: tofu state list fails then.
		_, statErr := os.Stat(filepath.Join(work, "terraform.tfstate"))
		if out, listErr := c.tofu(context.Background(), work, env, "state", "list"); os.IsNotExist(statErr) || (listErr == nil && strings.TrimSpace(string(out)) == "") {
			_ = os.RemoveAll(work)
		}
		return Machine{}, err
	}
	id := values["id"]
	if id == "" {
		_ = os.RemoveAll(work)
		return Machine{}, fmt.Errorf("OpenTofu n'a pas renvoyé l'identifiant.")
	}
	dest := filepath.Join(root, provider+"-"+stackID(id))
	_ = os.RemoveAll(dest)
	if err := os.Rename(work, dest); err != nil {
		return Machine{}, err
	}
	return Machine{ID: id, IPv4: values["ip"]}, nil
}

// prepare looks up what some providers only accept as their own identifiers:
// the region, the flavor, the image, the security group. OpenTofu then
// receives values it cannot get wrong.
func (c Client) prepare(ctx context.Context, provider string, creds catalog.Creds, spec Spec) (Spec, error) {
	var err error
	switch provider {
	case "ovh":
		var project, region, flavor, image string
		project, region, flavor, image, err = c.ovhTarget(ctx, creds, spec)
		if err != nil {
			return spec, err
		}
		spec.ProjectID, spec.Location, spec.OfferID, spec.ImageID = project, region, flavor, image
		if len(spec.SSHKeys) > 0 {
			spec.KeyName, err = c.ovhKey(ctx, creds, project, spec.SSHKeys[0])
			if err != nil {
				return spec, err
			}
		}
	case "upcloud":
		size, tier, image, err := c.upcloudTarget(ctx, creds.Token, spec)
		if err != nil {
			return spec, err
		}
		spec.RootGB, spec.DiskTier, spec.ImageID = size, tier, image
	case "exoscale":
		kind, image, group, err := c.exoscaleTarget(ctx, creds, spec)
		if err != nil {
			return spec, err
		}
		spec.OfferID, spec.ImageID, spec.GroupID = kind, image, group
	}
	return spec, nil
}

func (c Client) destroyStack(ctx context.Context, provider string, creds catalog.Creds, id string) error {
	dir, err := c.stackDir(provider, id)
	if err != nil {
		return err
	}
	hcl, env, addr, err := renderImport(provider, creds, id, c)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		if err := writeStack(dir, map[string]string{"main.tf.json": hcl}); err != nil {
			return err
		}
		if _, err := c.tofu(ctx, dir, env, "init", "-no-color"); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
		if _, err := c.tofu(ctx, dir, env, "import", "-no-color", addr, id); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
	}
	if _, err := c.tofu(ctx, dir, env, "destroy", "-auto-approve", "-no-color"); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ForgetStack drops the state kept for a machine its provider no longer has.
func (c Client) ForgetStack(provider, id string) {
	if provider == "" || strings.TrimSpace(id) == "" {
		return
	}
	if dir, err := c.stackDir(provider, id); err == nil {
		_ = os.RemoveAll(dir)
	}
}

func (c Client) HasTofuState(provider, id string) bool {
	dir, err := c.stackDir(provider, id)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "main.tf.json"))
	if err != nil {
		_, err = os.Stat(filepath.Join(dir, "main.tf"))
	}
	return err == nil
}

func (c Client) StackFiles(provider, id string) (map[string]string, error) {
	dir, err := c.stackDir(provider, id)
	if err != nil {
		return nil, err
	}
	return readStack(dir)
}

func (c Client) ApplyStack(ctx context.Context, provider string, creds catalog.Creds, id string, files map[string]string) (Machine, error) {
	dir, err := c.stackDir(provider, id)
	if err != nil {
		return Machine{}, err
	}
	previous, _ := readStack(dir)
	if err := sameProvider(provider, files); err != nil {
		return Machine{}, err
	}
	if err := writeStack(dir, files); err != nil {
		return Machine{}, err
	}
	_, env, _, err := renderImport(provider, creds, id, c)
	if err != nil {
		_ = writeStack(dir, previous)
		return Machine{}, err
	}
	values, err := c.applyStack(ctx, dir, env)
	if err != nil {
		_ = writeStack(dir, previous)
		return Machine{}, err
	}
	if values["id"] == "" || values["ip"] == "" {
		_ = writeStack(dir, previous)
		_, _ = c.applyStack(ctx, dir, env)
		return Machine{}, fmt.Errorf("Le fichier doit produire id et ip.")
	}
	return Machine{ID: values["id"], IPv4: values["ip"]}, nil
}

func (c Client) applyStack(ctx context.Context, dir string, env []string) (map[string]string, error) {
	if _, err := c.tofu(ctx, dir, env, "init", "-no-color"); err != nil {
		return nil, err
	}
	if _, err := c.tofu(ctx, dir, env, "apply", "-auto-approve", "-no-color"); err != nil {
		_, _ = c.tofu(context.Background(), dir, env, "destroy", "-auto-approve", "-no-color")
		return nil, err
	}
	out, err := c.tofu(ctx, dir, env, "output", "-json")
	if err != nil {
		return nil, err
	}
	var decoded map[string]struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		return nil, err
	}
	values := map[string]string{}
	for name, item := range decoded {
		values[name] = fmt.Sprint(item.Value)
	}
	return values, nil
}

func (c Client) tofu(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	root, err := c.tofuRoot()
	if err != nil {
		return nil, err
	}
	bin := c.TofuBin
	if bin == "" {
		bin = tools.Tofu()
	}
	if bin == "" {
		bin = "tofu"
	}
	cmd := proc.Hide(exec.CommandContext(ctx, bin, append([]string{"-chdir=" + dir}, args...)...))
	cmd.Env = append(os.Environ(), append(env, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "TF_PLUGIN_CACHE_DIR="+filepath.Join(root, ".plugins"))...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		text := out.String()
		if len(text) > 600 {
			text = text[len(text)-600:]
		}
		slog.Error("tofu", "args", strings.Join(args, " "), "err", err, "out", text)
		if ip := unauthorizedIP(out.String()); ip != "" {
			return out.Bytes(), fmt.Errorf("Le provider refuse l'adresse IP de MyInfra (%s) : autorisez-la dans les accès de la clé API.", ip)
		}
		if strings.Contains(text, "TRIAL_FIREWALL") {
			// The trial firewall only opens web ports: WireGuard cannot pass.
			return out.Bytes(), fmt.Errorf("Le provider a refusé la machine : ce compte UpCloud est en essai, son pare-feu est verrouillé. Passez le compte en accès complet.")
		}
		return out.Bytes(), fmt.Errorf("Le provider a refusé la machine.")
	}
	return out.Bytes(), nil
}

func writeStack(dir string, files map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for name, body := range files {
		if !configFile(name) {
			return fmt.Errorf("Seul un fichier .tf ou .tf.json peut être enregistré.")
		}
		if strings.Contains(name, "..") || strings.Contains(name, "/") {
			return fmt.Errorf("Nom de fichier refusé.")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func readStack(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !configFile(entry.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		files[entry.Name()] = string(raw)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("Aucun fichier OpenTofu.")
	}
	return files, nil
}
