package imagefile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

const config = `{"architecture":"amd64","os":"linux","config":{"ExposedPorts":{"8080/tcp":{}},"Volumes":{"/data":{}}}}`

func digestOf(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func tarOf(t *testing.T, files map[string]string, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range order {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// dockerArchive is what docker save wrote before Docker 25: manifest.json last.
func dockerArchive(t *testing.T) []byte {
	cfg := digestOf(config) + ".json"
	files := map[string]string{
		cfg:             config,
		"abc/layer.tar": "layer-bytes",
		"manifest.json": `[{"Config":"` + cfg + `","RepoTags":["demo:1.0"],"Layers":["abc/layer.tar"]}]`,
	}
	return tarOf(t, files, []string{cfg, "abc/layer.tar", "manifest.json"})
}

// ociArchive is an OCI layout: index.json, then a manifest and a config blob.
func ociArchive(t *testing.T) []byte {
	cfgDigest := digestOf(config)
	manifest := `{"schemaVersion":2,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + cfgDigest + `"},"layers":[]}`
	manDigest := digestOf(manifest)
	files := map[string]string{
		"oci-layout":                `{"imageLayoutVersion":"1.0.0"}`,
		"index.json":                `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + manDigest + `","size":1}]}`,
		"blobs/sha256/" + manDigest: manifest,
		"blobs/sha256/" + cfgDigest: config,
	}
	return tarOf(t, files, []string{"oci-layout", "index.json", "blobs/sha256/" + manDigest, "blobs/sha256/" + cfgDigest})
}

func gzipped(t *testing.T, raw []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func entries(t *testing.T, file string) map[string]string {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	tr := tar.NewReader(f)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(tr)
		out[header.Name] = string(raw)
	}
}

func TestDockerArchive(t *testing.T) {
	for name, raw := range map[string][]byte{"tar": dockerArchive(t), "tar.gz": gzipped(t, dockerArchive(t))} {
		t.Run(name, func(t *testing.T) {
			archive, err := Read(bytes.NewReader(raw), t.TempDir(), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			if archive.Platform != "linux/amd64" || archive.Digest != "sha256:"+digestOf(config) || len(archive.Tags) != 1 || archive.Tags[0] != "demo:1.0" {
				t.Fatalf("lu : %+v", archive)
			}
			out, size, err := archive.WriteAs("myinfra.local/demo:1.0", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(out)
			if size == 0 {
				t.Fatal("taille nulle")
			}
			got := entries(t, out)
			var manifest []dockerEntry
			if err := json.Unmarshal([]byte(got["manifest.json"]), &manifest); err != nil || manifest[0].RepoTags[0] != "myinfra.local/demo:1.0" {
				t.Fatalf("manifest.json : %v %s", err, got["manifest.json"])
			}
			if got["abc/layer.tar"] != "layer-bytes" {
				t.Fatal("couche altérée")
			}
		})
	}
}

func TestOCIArchive(t *testing.T) {
	archive, err := Read(bytes.NewReader(ociArchive(t)), t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if archive.Platform != "linux/amd64" || archive.Digest != "sha256:"+digestOf(config) {
		t.Fatalf("lu : %+v", archive)
	}
	out, _, err := archive.WriteAs("myinfra.local/demo:sha-123", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(out)
	index := entries(t, out)["index.json"]
	if !strings.Contains(index, `"io.containerd.image.name":"myinfra.local/demo:sha-123"`) || !strings.Contains(index, `"org.opencontainers.image.ref.name":"sha-123"`) {
		t.Fatalf("index.json : %s", index)
	}
}

func TestNotAnImage(t *testing.T) {
	raw := tarOf(t, map[string]string{"hello.txt": "hi"}, []string{"hello.txt"})
	if _, err := Read(bytes.NewReader(raw), t.TempDir(), 1<<20); err == nil || !strings.Contains(err.Error(), "docker save") {
		t.Fatalf("erreur attendue, reçu %v", err)
	}
	if _, err := Read(strings.NewReader("pas une archive"), t.TempDir(), 1<<20); err == nil {
		t.Fatal("un texte a été accepté")
	}
}

func TestTooLarge(t *testing.T) {
	if _, err := Read(bytes.NewReader(dockerArchive(t)), t.TempDir(), 100); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limite ignorée : %v", err)
	}
}
