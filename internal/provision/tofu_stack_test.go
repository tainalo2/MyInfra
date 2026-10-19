package provision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"myinfra/internal/catalog"
)

func TestCreateStackWritesHCL(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tofu")
	script := []byte("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"-json\" ]; then\n    printf '%s\\n' '{\"id\":{\"value\":\"srv-1\"},\"ip\":{\"value\":\"203.0.113.10\"}}'\n    exit 0\n  fi\ndone\nexit 0\n")
	if err := os.WriteFile(bin, script, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "state")
	machine, err := (Client{TofuDir: root, TofuBin: bin}).Create(context.Background(), "hetzner", catalog.Creds{Token: "secret-token"}, Spec{
		Name: "web", OfferID: "cx22", Location: "fsn1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if machine.ID != "srv-1" || machine.IPv4 != "203.0.113.10" {
		t.Fatalf("%+v", machine)
	}
	body, err := os.ReadFile(filepath.Join(root, "hetzner-srv-1", "main.tf.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "secret-token") || !strings.Contains(text, "hcloud_server") {
		t.Fatalf("%s", text)
	}
}

func TestApplyStackRejectsMissingOutputs(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tofu")
	ok := []byte("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"-json\" ]; then\n    printf '%s\\n' '{\"id\":{\"value\":\"srv-1\"},\"ip\":{\"value\":\"203.0.113.10\"}}'\n    exit 0\n  fi\ndone\nexit 0\n")
	if err := os.WriteFile(bin, ok, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "state")
	client := Client{TofuDir: root, TofuBin: bin}
	if _, err := client.Create(context.Background(), "hetzner", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "cx22", Location: "fsn1"}); err != nil {
		t.Fatal(err)
	}
	bad := []byte("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"-json\" ]; then\n    printf '%s\\n' '{\"name\":{\"value\":\"web\"}}'\n    exit 0\n  fi\ndone\nexit 0\n")
	if err := os.WriteFile(bin, bad, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := client.ApplyStack(context.Background(), "hetzner", catalog.Creds{Token: "secret-token"}, "srv-1", map[string]string{
		"main.tf.json": "{\n  \"terraform\": {\"required_providers\": {\"hcloud\": {\"source\": \"hetznercloud/hcloud\"}}}\n}\n",
	})
	if err == nil || !strings.Contains(err.Error(), "id et ip") {
		t.Fatal(err)
	}
}
