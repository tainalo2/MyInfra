package tools

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// A Python that runs without being installed: python-build-standalone, the
// builds uv and others use. MyInfra unpacks it in its own folder when the
// machine has no Python able to make a virtual environment for Ansible.
const (
	pythonRelease = "20261003"
	pythonVersion = "3.12.15"
)

// pythonBase is where the builds are published; tests point it elsewhere.
var pythonBase = "https://github.com/astral-sh/python-build-standalone/releases/download/" + pythonRelease

// pythonTriple names the build of a system, empty when there is none.
func pythonTriple(goos, goarch string) string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if arch == "" {
		return ""
	}
	switch goos {
	case "darwin":
		return arch + "-apple-darwin"
	case "linux":
		return arch + "-unknown-linux-gnu"
	case "windows":
		return arch + "-pc-windows-msvc"
	}
	return ""
}

func pythonAsset(goos, goarch string) string {
	triple := pythonTriple(goos, goarch)
	if triple == "" {
		return ""
	}
	return "cpython-" + pythonVersion + "+" + pythonRelease + "-" + triple + "-install_only.tar.gz"
}

// pythonIn is the interpreter of a build unpacked in dir.
func pythonIn(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "python.exe")
	}
	return filepath.Join(dir, "bin", "python3")
}

// installPython unpacks the Python of this system into home/python, checked
// against the sums of its release, and returns its interpreter.
func installPython(ctx context.Context, home string) (string, error) {
	dir := filepath.Join(home, "python")
	if _, err := os.Stat(pythonIn(dir)); err == nil {
		return pythonIn(dir), nil
	}
	asset := pythonAsset(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		return "", errString("Aucun Python n'est disponible pour ce système : installez Python 3, puis réessayez.")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	sums, err := download(ctx, pythonBase+"/SHA256SUMS", 4<<20)
	if err != nil {
		return "", errString("Python n'a pas pu être téléchargé.")
	}
	want := sumOf(string(sums), asset)
	if want == "" {
		return "", errString("Python n'a pas pu être téléchargé.")
	}
	archive, err := download(ctx, pythonBase+"/"+asset, 200<<20)
	if err != nil {
		return "", errString("Python n'a pas pu être téléchargé.")
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return "", errString("Le Python téléchargé ne correspond pas à sa somme de contrôle : il a été écarté.")
	}
	// Unpacked beside, then moved in place: a cut download leaves nothing half done.
	tmp := dir + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := unpackPython(archive, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", errString("Python n'a pas pu être installé.")
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return "", errString("Python n'a pas pu être installé.")
	}
	if _, err := os.Stat(pythonIn(dir)); err != nil {
		return "", errString("Python n'a pas pu être installé.")
	}
	return pythonIn(dir), nil
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "myinfra")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

func sumOf(sums, name string) string {
	scanner := bufio.NewScanner(strings.NewReader(sums))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

// unpackPython extracts the archive, whose files sit under python/, into dir.
func unpackPython(archive []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(filepath.ToSlash(filepath.Clean(header.Name)), "python/")
		if !ok || rel == "" {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if inside, err := filepath.Rel(dir, dest); err != nil || strings.HasPrefix(inside, "..") {
			return fmt.Errorf("chemin refusé")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o755|0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			target := filepath.Join(filepath.Dir(dest), filepath.FromSlash(header.Linkname))
			if inside, err := filepath.Rel(dir, target); err != nil || strings.HasPrefix(inside, "..") {
				return fmt.Errorf("lien refusé")
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, dest); err != nil {
				return err
			}
		}
	}
}
