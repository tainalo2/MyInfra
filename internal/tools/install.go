package tools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"myinfra/internal/boot"
)

const (
	tofuVersion = "1.13.1"
	helmVersion = "3.19.0"
)

func Install(ctx context.Context, name string) error {
	switch name {
	case "ansible":
		return installAnsible(ctx)
	case "tofu":
		return installTofu(ctx)
	case "helm":
		return installHelm(ctx)
	default:
		return errString("Outil inconnu.")
	}
}

// installAnsible puts Ansible in a virtual environment of MyInfra's folder.
// Without a Python able to make one — none at all, or a Debian one without
// its venv module — MyInfra installs its own Python first.
func installAnsible(ctx context.Context) error {
	home, err := boot.Home()
	if err != nil {
		return err
	}
	venv := filepath.Join(home, "ansible")
	made := false
	for _, name := range []string{"python3", "python"} {
		python, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := run(ctx, python, "-m", "venv", "--clear", venv); err == nil {
			made = true
			break
		}
	}
	if !made {
		python, err := installPython(ctx, home)
		if err != nil {
			return err
		}
		if err := run(ctx, python, "-m", "venv", "--clear", venv); err != nil {
			return errString("Ansible n'a pas pu être installé.")
		}
	}
	pip := filepath.Join(venv, "bin", exe("pip"))
	if runtime.GOOS == "windows" {
		pip = filepath.Join(venv, "Scripts", "pip.exe")
	}
	if err := run(ctx, pip, "install", "ansible-core"); err != nil {
		return errString("Ansible n'a pas pu être installé.")
	}
	if Ansible() == "" {
		return errString("Ansible n'a pas pu être installé.")
	}
	PreparePath()
	return nil
}

func installTofu(ctx context.Context) error {
	home, err := boot.Home()
	if err != nil {
		return err
	}
	arch := runtime.GOARCH
	name := fmt.Sprintf("tofu_%s_%s_%s.zip", tofuVersion, runtime.GOOS, arch)
	url := fmt.Sprintf("https://github.com/opentofu/opentofu/releases/download/v%s/%s", tofuVersion, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "myinfra")
	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		return errString("OpenTofu n'a pas pu être installé.")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 80<<20))
	if err != nil {
		return errString("OpenTofu n'a pas pu être installé.")
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return errString("OpenTofu n'a pas pu être installé.")
	}
	var file *zip.File
	want := exe("tofu")
	for _, item := range reader.File {
		base := filepath.Base(item.Name)
		if base == want {
			file = item
			break
		}
	}
	if file == nil {
		return errString("OpenTofu n'a pas pu être installé.")
	}
	src, err := file.Open()
	if err != nil {
		return errString("OpenTofu n'a pas pu être installé.")
	}
	defer src.Close()
	dir := filepath.Join(home, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(dir, want)
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return errString("OpenTofu n'a pas pu être installé.")
	}
	if closeErr != nil {
		return closeErr
	}
	PreparePath()
	return nil
}

// installHelm fetches Helm, checks it against the digest its site
// publishes, and keeps its one binary. MyInfra only renders charts with it.
func installHelm(ctx context.Context) error {
	home, err := boot.Home()
	if err != nil {
		return err
	}
	failed := errString("Helm n'a pas pu être installé.")
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	name := fmt.Sprintf("helm-v%s-%s-%s.%s", helmVersion, runtime.GOOS, runtime.GOARCH, ext)
	archive, err := fetch(ctx, "https://get.helm.sh/"+name, 80<<20)
	if err != nil {
		return failed
	}
	sum, err := fetch(ctx, "https://get.helm.sh/"+name+".sha256sum", 4<<10)
	if err != nil {
		return failed
	}
	digest := sha256.Sum256(archive)
	if fields := strings.Fields(string(sum)); len(fields) == 0 || fields[0] != hex.EncodeToString(digest[:]) {
		return errString("Helm téléchargé ne correspond pas à son empreinte : il n'est pas installé.")
	}
	want := exe("helm")
	var binary []byte
	if ext == "zip" {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return failed
		}
		for _, item := range reader.File {
			if filepath.Base(item.Name) == want {
				src, err := item.Open()
				if err != nil {
					return failed
				}
				binary, err = io.ReadAll(io.LimitReader(src, 200<<20))
				src.Close()
				if err != nil {
					return failed
				}
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return failed
		}
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return failed
			}
			if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == want {
				if binary, err = io.ReadAll(io.LimitReader(tr, 200<<20)); err != nil {
					return failed
				}
			}
		}
	}
	if len(binary) == 0 {
		return failed
	}
	dir := filepath.Join(home, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, want), binary, 0o755); err != nil {
		return err
	}
	PreparePath()
	return nil
}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "myinfra")
	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		if text == "" {
			return err
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }
