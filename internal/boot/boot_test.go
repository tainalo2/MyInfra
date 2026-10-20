package boot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
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

// archiveOf packs entries into a tar.gz the way the build packs PostgreSQL.
func archiveOf(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	for _, header := range entries {
		body := []byte(header.Name)
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(body))
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

func TestExtractKeepsLibraryLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("les archives Windows n'ont pas de liens")
	}
	archive := archiveOf(t,
		&tar.Header{Name: "lib/libicuuc.so.60.2", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "lib/libicuuc.so.60", Typeflag: tar.TypeSymlink, Linkname: "libicuuc.so.60.2"},
	)
	root := filepath.Join(t.TempDir(), "pg")
	if err := extractPostgres(archive, root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "lib", "libicuuc.so.60"))
	if err != nil || string(got) != "lib/libicuuc.so.60.2" {
		t.Fatalf("le lien ne mène pas à la bibliothèque : %v %q", err, got)
	}
}

func TestExtractRefusesLinkOutside(t *testing.T) {
	archive := archiveOf(t, &tar.Header{Name: "lib/evil", Typeflag: tar.TypeSymlink, Linkname: "../../outside"})
	if err := extractPostgres(archive, filepath.Join(t.TempDir(), "pg")); err == nil {
		t.Fatal("un lien sortant du dossier a été accepté")
	}
}

func TestExtractRedoesIncompleteInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pg")
	// What an older binary left: bin/postgres there, its library links missing.
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "postgres"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := archiveOf(t,
		&tar.Header{Name: "bin/postgres", Typeflag: tar.TypeReg, Mode: 0o755},
		&tar.Header{Name: "lib/libz.so.1", Typeflag: tar.TypeReg, Mode: 0o644},
	)
	if err := extractPostgres(archive, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "libz.so.1")); err != nil {
		t.Fatalf("installation incomplète gardée : %v", err)
	}
	// Unpacked in full once, the same archive is not unpacked again.
	if err := os.WriteFile(filepath.Join(root, "bin", "kept"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractPostgres(archive, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "kept")); err != nil {
		t.Fatal("la même archive a été extraite une seconde fois")
	}
}
