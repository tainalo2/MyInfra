package boot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestMasterStable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MYINFRA_MASTER_KEY", "")
	first, err := Master(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 {
		t.Fatalf("clé %d", len(first))
	}
	second, err := Master(home)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("la clé a changé")
	}
	info, err := os.Stat(filepath.Join(home, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %o", info.Mode().Perm())
	}
}

func TestMasterEnvWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MYINFRA_MASTER_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	key, err := Master(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "master.key")); !os.IsNotExist(err) {
		t.Fatal("la variable ne doit pas écrire de fichier")
	}
	again, err := DecodeMaster("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil || !bytes.Equal(key, again) {
		t.Fatal(err)
	}
}

func TestExtractSkipsArchiveRoot(t *testing.T) {
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	body := []byte("ok")
	if err := tw.WriteHeader(&tar.Header{Name: "./bin/marker", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := extractPostgres(raw.Bytes(), root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "bin", "marker"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("extraction %v %q", err, got)
	}
}

func TestLoopback(t *testing.T) {
	if err := requireLoopback("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopback("::1"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopback("8.8.8.8"); err == nil {
		t.Fatal("adresse publique acceptée")
	}
	if err := requireLoopback("0.0.0.0"); err == nil {
		t.Fatal("écoute ouverte acceptée")
	}
}
