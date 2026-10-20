// Package imagefile reads the archive docker save writes, in its Docker or its
// OCI layout, compressed or not, and writes it again under the name MyInfra
// gives the image: docker load and ctr images import then both know it by it.
package imagefile

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// ErrTooLarge stops an archive past the limit while it is read.
var ErrTooLarge = errors.New("archive trop grande")

// Archive is an image archive spooled to a temporary file.
type Archive struct {
	file string
	// Config is the image configuration, as the image carries it.
	Config []byte
	// Digest is the sha256 of the configuration: the image's own identity.
	Digest string
	// Platform is os/architecture, linux/amd64 for instance.
	Platform string
	// Tags are the names docker save recorded, such as nginx:1.27.
	Tags []string
}

// smallFile bounds what is kept in memory while looking for the manifests.
const smallFile = 4 << 20

// Read spools src to dir, uncompressing it if it is gzip, and reads what the
// archive says about the image.
func Read(src io.Reader, dir string, limit int64) (*Archive, error) {
	buffered := bufio.NewReader(src)
	var in io.Reader = buffered
	if head, err := buffered.Peek(2); err == nil && head[0] == 0x1f && head[1] == 0x8b {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, fmt.Errorf("Archive illisible.")
		}
		defer gz.Close()
		in = gz
	}
	tmp, err := os.CreateTemp(dir, "myinfra-image-*.tar")
	if err != nil {
		return nil, err
	}
	archive := &Archive{file: tmp.Name()}
	written, err := io.Copy(tmp, io.LimitReader(in, limit+1))
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && written > limit {
		err = ErrTooLarge
	}
	if err != nil {
		archive.Close()
		return nil, err
	}
	if err := archive.inspect(); err != nil {
		archive.Close()
		return nil, err
	}
	return archive, nil
}

// Close removes the temporary file.
func (a *Archive) Close() {
	_ = os.Remove(a.file)
}

type dockerEntry struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

type ociDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Platform    *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"platform,omitempty"`
}

type ociIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType,omitempty"`
	Manifests     []ociDescriptor `json:"manifests"`
	Config        *ociDescriptor  `json:"config,omitempty"`
}

// inspect finds the configuration through manifest.json (Docker) or
// index.json (OCI), whichever the archive has.
func (a *Archive) inspect() error {
	files, err := a.smallFiles()
	if err != nil {
		return err
	}
	var config []byte
	if raw, ok := files["manifest.json"]; ok {
		var entries []dockerEntry
		if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
			return fmt.Errorf("Archive illisible : manifest.json n'est pas lisible.")
		}
		a.Tags = entries[0].RepoTags
		config = files[clean(entries[0].Config)]
	}
	if config == nil {
		raw, ok := files["index.json"]
		if !ok {
			return fmt.Errorf("Ce fichier n'est pas une image : ni manifest.json ni index.json. Produisez-le avec docker save.")
		}
		config, err = ociConfig(raw, files, 0)
		if err != nil {
			return err
		}
	}
	if config == nil {
		return fmt.Errorf("Archive incomplète : la configuration de l'image manque.")
	}
	var meta struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	}
	if err := json.Unmarshal(config, &meta); err != nil {
		return fmt.Errorf("Archive illisible : la configuration de l'image n'est pas lisible.")
	}
	sum := sha256.Sum256(config)
	a.Config = config
	a.Digest = "sha256:" + hex.EncodeToString(sum[:])
	if meta.OS != "" && meta.Architecture != "" {
		a.Platform = meta.OS + "/" + meta.Architecture
	}
	return nil
}

// ociConfig follows an index to a manifest, linux/amd64 first, then to its config.
func ociConfig(raw []byte, files map[string][]byte, depth int) ([]byte, error) {
	if depth > 3 {
		return nil, fmt.Errorf("Archive illisible : index trop profond.")
	}
	var doc ociIndex
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("Archive illisible : index.json n'est pas lisible.")
	}
	if doc.Config != nil && doc.Config.Digest != "" {
		return files[blobPath(doc.Config.Digest)], nil
	}
	if len(doc.Manifests) == 0 {
		return nil, fmt.Errorf("Archive incomplète : aucun manifeste.")
	}
	pick := doc.Manifests[0]
	for _, item := range doc.Manifests {
		if item.Platform != nil && item.Platform.OS == "linux" && item.Platform.Architecture == "amd64" {
			pick = item
			break
		}
	}
	next, ok := files[blobPath(pick.Digest)]
	if !ok {
		return nil, fmt.Errorf("Archive incomplète : le manifeste %s manque.", pick.Digest)
	}
	return ociConfig(next, files, depth+1)
}

func blobPath(digest string) string {
	algo, hexa, _ := strings.Cut(digest, ":")
	return "blobs/" + algo + "/" + hexa
}

func clean(name string) string {
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}

// smallFiles reads the files small enough to be manifests or configurations.
func (a *Archive) smallFiles() (map[string][]byte, error) {
	f, err := os.Open(a.file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	files := map[string][]byte{}
	tr := tar.NewReader(f)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("Ce fichier n'est pas une archive tar. Produisez-le avec docker save.")
		}
		if header.Typeflag != tar.TypeReg || header.Size > smallFile {
			continue
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("Archive illisible.")
		}
		files[clean(header.Name)] = raw
	}
}

// WriteAs writes the archive again under ref, to a new file in dir, and
// returns its path and size. Every entry is copied; manifest.json and
// index.json name the image ref and nothing else.
func (a *Archive) WriteAs(ref, dir string) (string, int64, error) {
	src, err := os.Open(a.file)
	if err != nil {
		return "", 0, err
	}
	defer src.Close()
	out, err := os.CreateTemp(dir, "myinfra-image-*.tar")
	if err != nil {
		return "", 0, err
	}
	fail := func(err error) (string, int64, error) {
		out.Close()
		os.Remove(out.Name())
		return "", 0, err
	}
	tw := tar.NewWriter(out)
	tr := tar.NewReader(src)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(fmt.Errorf("Archive illisible."))
		}
		name := clean(header.Name)
		if header.Typeflag == tar.TypeReg && (name == "manifest.json" || name == "index.json") {
			raw, err := io.ReadAll(tr)
			if err != nil {
				return fail(err)
			}
			if name == "manifest.json" {
				raw, err = renameDocker(raw, ref)
			} else {
				raw, err = renameOCI(raw, ref)
			}
			if err != nil {
				return fail(err)
			}
			header.Size = int64(len(raw))
			if err := tw.WriteHeader(header); err != nil {
				return fail(err)
			}
			if _, err := tw.Write(raw); err != nil {
				return fail(err)
			}
			continue
		}
		if err := tw.WriteHeader(header); err != nil {
			return fail(err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return fail(err)
		}
	}
	if err := tw.Close(); err != nil {
		return fail(err)
	}
	info, err := out.Stat()
	if err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", 0, err
	}
	return out.Name(), info.Size(), nil
}

// renameDocker keeps the first image of manifest.json, named ref.
func renameDocker(raw []byte, ref string) ([]byte, error) {
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("Archive illisible : manifest.json n'est pas lisible.")
	}
	first := entries[0]
	first["RepoTags"] = []string{ref}
	return json.Marshal([]map[string]any{first})
}

// renameOCI names the first manifest of index.json ref, the way containerd and
// Docker read the name of an OCI archive.
func renameOCI(raw []byte, ref string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("Archive illisible : index.json n'est pas lisible.")
	}
	manifests, _ := doc["manifests"].([]any)
	if len(manifests) == 0 {
		return nil, fmt.Errorf("Archive incomplète : aucun manifeste.")
	}
	first, _ := manifests[0].(map[string]any)
	if first == nil {
		return nil, fmt.Errorf("Archive illisible : index.json n'est pas lisible.")
	}
	annotations, _ := first["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
	}
	tag := ref[strings.LastIndex(ref, ":")+1:]
	annotations["io.containerd.image.name"] = ref
	annotations["org.opencontainers.image.ref.name"] = tag
	first["annotations"] = annotations
	doc["manifests"] = []any{first}
	return json.Marshal(doc)
}

// LocalPrefix starts the name of every image MyInfra keeps itself: the servers
// load it from MyInfra and never pull it.
const LocalPrefix = "myinfra.local/"

// IsLocal tells an image MyInfra keeps from one a registry serves.
func IsLocal(image string) bool {
	return strings.HasPrefix(image, LocalPrefix)
}
