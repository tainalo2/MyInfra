package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

func fakePython(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho Python 3.12.15\n")
	for _, h := range []*tar.Header{
		{Name: "python/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "python/bin/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "python/bin/python3.12", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))},
		{Name: "python/bin/python3", Typeflag: tar.TypeSymlink, Linkname: "python3.12"},
		{Name: "python/python.exe", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write(body)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestInstallPythonChecksTheSum(t *testing.T) {
	asset := pythonAsset(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		t.Skip("pas de Python pour ce système")
	}
	archive := fakePython(t)
	sum := sha256.Sum256(archive)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		case asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	before := pythonBase
	pythonBase = srv.URL
	defer func() { pythonBase = before }()

	home := t.TempDir()
	sums = strings.Repeat("0", 64) + "  " + asset + "\n"
	if _, err := installPython(context.Background(), home); err == nil || !strings.Contains(err.Error(), "somme") {
		t.Fatalf("somme fausse acceptée : %v", err)
	}
	sums = hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	python, err := installPython(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(python); err != nil {
		t.Fatalf("%s : %v", python, err)
	}
}

func TestPythonAssetNames(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "cpython-" + pythonVersion + "+" + pythonRelease + "-aarch64-apple-darwin-install_only.tar.gz"},
		{"linux", "amd64", "cpython-" + pythonVersion + "+" + pythonRelease + "-x86_64-unknown-linux-gnu-install_only.tar.gz"},
		{"windows", "amd64", "cpython-" + pythonVersion + "+" + pythonRelease + "-x86_64-pc-windows-msvc-install_only.tar.gz"},
		{"linux", "386", ""},
	} {
		if got := pythonAsset(c.goos, c.goarch); got != c.want {
			t.Errorf("%s/%s : %q", c.goos, c.goarch, got)
		}
	}
}
