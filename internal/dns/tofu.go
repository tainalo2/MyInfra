package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"myinfra/internal/proc"
	"myinfra/internal/tools"
)

func (c Client) recordDir(provider, host string) (string, error) {
	if c.TofuDir == "" {
		return "", fmt.Errorf("OpenTofu n'est pas configuré.")
	}
	name := strings.NewReplacer("/", "_", ".", "_").Replace(provider + "-" + host)
	dir := filepath.Join(c.TofuDir, name)
	return dir, nil
}

func (c Client) ensureStack(ctx context.Context, provider string, creds Creds, zone, host string, ips []string, kind string) error {
	dir, err := c.recordDir(provider, host)
	if err != nil {
		return err
	}
	hcl, env, err := recordHCL(provider, creds, zone, host, ips, kind)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(c.TofuDir, ".plugins"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf.json"), []byte(hcl), 0o600); err != nil {
		return err
	}
	values, err := c.apply(ctx, dir, env)
	if err != nil {
		return err
	}
	if values["hostname"] == "" || values["address"] == "" {
		return fmt.Errorf("Le fichier doit produire hostname et address.")
	}
	return nil
}

func (c Client) dropStack(ctx context.Context, provider string, creds Creds, zone, host string) error {
	dir, err := c.recordDir(provider, host)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(filepath.Join(dir, "main.tf.json")); statErr != nil {
		return statErr
	}
	_, env, err := recordHCL(provider, creds, zone, host, []string{"0.0.0.0"}, "A")
	if err != nil {
		return err
	}
	if _, err := c.tofu(ctx, dir, env, "destroy", "-auto-approve", "-no-color"); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (c Client) RecordFiles(provider, host string) (map[string]string, error) {
	dir, err := c.recordDir(provider, host)
	if err != nil {
		return nil, err
	}
	return readTF(dir)
}

func (c Client) ApplyRecord(ctx context.Context, provider string, creds Creds, host string, files map[string]string) error {
	dir, err := c.recordDir(provider, host)
	if err != nil {
		return err
	}
	previous, _ := readTF(dir)
	if err := sameDNS(provider, files); err != nil {
		return err
	}
	if err := writeTF(dir, files); err != nil {
		return err
	}
	_, env, err := recordHCL(provider, creds, "", host, []string{"0.0.0.0"}, "A")
	if err != nil {
		return err
	}
	values, err := c.apply(ctx, dir, env)
	if err != nil || values["hostname"] == "" || values["address"] == "" {
		if previous != nil {
			_ = writeTF(dir, previous)
			_, _ = c.apply(ctx, dir, env)
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("Le fichier doit produire hostname et address.")
	}
	return nil
}

// recordHCL describes one record per address. for_each keys each record by
// its address, so adding a node adds a record without touching the others.
func recordHCL(provider string, creds Creds, zone, host string, ips []string, kind string) (string, []string, error) {
	each := map[string]string{}
	for _, ip := range ips {
		each[ip] = ip
	}
	env := []string{"TF_VAR_token=" + creds.Token}
	var doc map[string]any
	switch provider {
	case "cloudflare":
		doc = map[string]any{
			"terraform": map[string]any{"required_providers": map[string]any{"cloudflare": map[string]any{"source": "cloudflare/cloudflare", "version": "~> 5.0"}}},
			"variable":  tofuVariables("token"),
			"provider":  map[string]any{"cloudflare": map[string]any{"api_token": "${var.token}"}},
			"data": map[string]any{"cloudflare_zone": map[string]any{"zone": map[string]any{
				"filter": map[string]any{"name": zone},
			}}},
			"resource": map[string]any{"cloudflare_dns_record": map[string]any{"name": map[string]any{
				"for_each": each,
				"zone_id":  "${data.cloudflare_zone.zone.zone_id}", "name": host, "type": kind, "content": "${each.value}", "ttl": 300,
			}}},
			"output": map[string]any{
				"hostname": map[string]any{"value": "${values(cloudflare_dns_record.name)[0].name}"},
				"address":  map[string]any{"value": "${join(\",\", sort([for r in cloudflare_dns_record.name : r.content]))}"},
			},
		}
	case "ovh":
		env = append(env,
			"TF_VAR_application_key="+creds.ApplicationKey,
			"TF_VAR_application_secret="+creds.ApplicationSecret,
			"TF_VAR_consumer_key="+creds.ConsumerKey,
		)
		doc = map[string]any{
			"terraform": map[string]any{"required_providers": map[string]any{"ovh": map[string]any{"source": "ovh/ovh", "version": "~> 2.0"}}},
			"variable":  tofuVariables("application_key", "application_secret", "consumer_key"),
			"provider": map[string]any{"ovh": map[string]any{
				"endpoint": "ovh-eu", "application_key": "${var.application_key}", "application_secret": "${var.application_secret}", "consumer_key": "${var.consumer_key}",
			}},
			"resource": map[string]any{"ovh_domain_zone_record": map[string]any{"name": map[string]any{
				"for_each": each,
				"zone":     zone, "subdomain": strings.TrimSuffix(host, "."+zone), "fieldtype": kind, "target": "${each.value}", "ttl": 300,
			}}},
			"output": map[string]any{
				"hostname": map[string]any{"value": "${values(ovh_domain_zone_record.name)[0].subdomain}"},
				"address":  map[string]any{"value": "${join(\",\", sort([for r in ovh_domain_zone_record.name : r.target]))}"},
			},
		}
	default:
		return "", nil, fmt.Errorf("Ce compte DNS est inconnu.")
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return string(raw) + "\n", env, nil
}

func tofuVariables(names ...string) map[string]any {
	vars := map[string]any{}
	for _, name := range names {
		vars[name] = map[string]any{"type": "string", "sensitive": true, "default": ""}
	}
	return vars
}

func (c Client) apply(ctx context.Context, dir string, env []string) (map[string]string, error) {
	if _, err := c.tofu(ctx, dir, env, "init", "-no-color"); err != nil {
		return nil, err
	}
	if _, err := c.tofu(ctx, dir, env, "apply", "-auto-approve", "-no-color"); err != nil {
		return nil, err
	}
	out, err := c.tofu(ctx, dir, env, "output", "-json")
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	values := map[string]string{}
	for key, item := range raw {
		values[key] = fmt.Sprint(item.Value)
	}
	return values, nil
}

func (c Client) tofu(ctx context.Context, dir string, extra []string, args ...string) ([]byte, error) {
	bin := c.TofuBin
	if bin == "" {
		bin = tools.Tofu()
	}
	if bin == "" {
		bin = "tofu"
	}
	cmd := proc.Hide(exec.CommandContext(ctx, bin, args...))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extra...)
	cmd.Env = append(cmd.Env, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "TF_PLUGIN_CACHE_DIR="+filepath.Join(c.TofuDir, ".plugins"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Le compte DNS a refusé l'enregistrement.")
	}
	return stdout.Bytes(), nil
}

func sameDNS(provider string, files map[string]string) error {
	want := map[string]string{"cloudflare": "cloudflare/cloudflare", "ovh": "ovh/ovh"}[provider]
	if want == "" {
		return fmt.Errorf("Ce compte DNS est inconnu.")
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

func readTF(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("Aucun fichier OpenTofu.")
	}
	files := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") && !strings.HasSuffix(entry.Name(), ".tf.json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		files[entry.Name()] = string(body)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("Aucun fichier OpenTofu.")
	}
	return files, nil
}

func writeTF(dir string, files map[string]string) error {
	if len(files) == 0 {
		return fmt.Errorf("Aucun fichier OpenTofu.")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for name, body := range files {
		if strings.Contains(name, "/") || strings.Contains(name, "..") || (!strings.HasSuffix(name, ".tf") && !strings.HasSuffix(name, ".tf.json")) {
			return fmt.Errorf("Nom de fichier refusé.")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}
