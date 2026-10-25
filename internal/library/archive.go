package library

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing/fstest"
	"time"
)

const (
	// The files of a release.
	ArchiveName   = "library.tar.gz"
	SumsName      = "SHA256SUMS"
	SignatureName = "SHA256SUMS.sig"
	// MaxArchive bounds what MyInfra downloads and opens.
	MaxArchive = 32 << 20
	maxFile    = 4 << 20
)

// Index is index.json, the first file of an archive.
type Index struct {
	Format  int    `json:"format"`
	Release string `json:"release"`
	// Files gives the sha256 of every other file of the archive.
	Files map[string]string `json:"files"`
	Items []IndexItem       `json:"items"`
}

type IndexItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Item
}

// Bundle is a release as it is downloaded and kept: the archive, its sums
// and their signature.
type Bundle struct {
	Release   string
	Archive   []byte
	Sums      []byte
	Signature []byte
}

func Sum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// published lists the files of a library tree that a release carries.
func published(fsys fs.FS) ([]string, error) {
	out := []string{"categories.yaml", "order.txt"}
	for _, dir := range []string{"packages", "templates", "logos"} {
		err := fs.WalkDir(fsys, dir, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && name == dir {
					return fs.SkipDir
				}
				return err
			}
			if !entry.IsDir() && !strings.HasPrefix(path.Base(name), ".") {
				out = append(out, name)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// Pack checks a library tree and writes its release: the archive and the
// sums to sign. The same tree gives the same archive.
func Pack(fsys fs.FS, release string) (Bundle, error) {
	lib, err := Read(fsys, release, "")
	if err != nil {
		return Bundle{}, err
	}
	names, err := published(fsys)
	if err != nil {
		return Bundle{}, err
	}
	index := Index{Format: Format, Release: release, Files: map[string]string{}}
	contents := map[string][]byte{}
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return Bundle{}, err
		}
		if len(raw) > maxFile {
			return Bundle{}, fmt.Errorf("%s dépasse %d Mo", name, maxFile>>20)
		}
		contents[name] = raw
		index.Files[name] = Sum(raw)
	}
	for _, p := range lib.Packages {
		index.Items = append(index.Items, IndexItem{Kind: "package", ID: p.ID, Item: p.Item})
	}
	for _, t := range lib.Templates {
		index.Items = append(index.Items, IndexItem{Kind: "template", ID: t.ID, Item: t.Item})
	}
	head, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return Bundle{}, err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.ModTime = time.Unix(0, 0)
	tw := tar.NewWriter(zw)
	write := func(name string, raw []byte) error {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(raw)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := tw.Write(raw)
		return err
	}
	if err := write("index.json", head); err != nil {
		return Bundle{}, err
	}
	for _, name := range names {
		if err := write(name, contents[name]); err != nil {
			return Bundle{}, err
		}
	}
	if err := tw.Close(); err != nil {
		return Bundle{}, err
	}
	if err := zw.Close(); err != nil {
		return Bundle{}, err
	}
	archive := buf.Bytes()
	sums := fmt.Sprintf("%s  %s\n", Sum(archive), ArchiveName)
	return Bundle{Release: release, Archive: archive, Sums: []byte(sums)}, nil
}

// Sign signs the sums of a release with the library's private key.
func Sign(sums []byte, key ed25519.PrivateKey) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, sums)) + "\n")
}

// Open checks a release and reads its library: the sums signed by one of
// the keys, the archive against the sums, each file against the index.
func Open(b Bundle, keys []ed25519.PublicKey, engine string) (*Library, error) {
	if err := verify(b, keys); err != nil {
		return nil, err
	}
	fsys, index, err := unpack(b.Archive)
	if err != nil {
		return nil, err
	}
	if index.Format != Format {
		return nil, fmt.Errorf("bibliothèque au format %d, cette version de MyInfra lit le format %d", index.Format, Format)
	}
	if b.Release != "" && index.Release != b.Release {
		return nil, fmt.Errorf("l'archive est la version %s, pas %s", index.Release, b.Release)
	}
	return Read(fsys, index.Release, engine)
}

func verify(b Bundle, keys []ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b.Signature)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature illisible")
	}
	signed := false
	for _, key := range keys {
		signed = signed || ed25519.Verify(key, b.Sums, sig)
	}
	if !signed {
		return fmt.Errorf("signature inconnue : la bibliothèque n'est pas celle publiée")
	}
	for _, line := range strings.Split(string(b.Sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == ArchiveName {
			if !strings.EqualFold(fields[0], Sum(b.Archive)) {
				return fmt.Errorf("%s ne correspond pas à %s", ArchiveName, SumsName)
			}
			return nil
		}
	}
	return fmt.Errorf("%s ne cite pas %s", SumsName, ArchiveName)
}

// unpack reads an archive in memory, each file checked against index.json.
func unpack(archive []byte) (fs.FS, Index, error) {
	var index Index
	if len(archive) > MaxArchive {
		return nil, index, fmt.Errorf("archive trop grande")
	}
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, index, fmt.Errorf("archive illisible")
	}
	tr := tar.NewReader(zr)
	files := fstest.MapFS{}
	total := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, index, fmt.Errorf("archive illisible")
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		if !fs.ValidPath(name) || hdr.Size > maxFile {
			return nil, index, fmt.Errorf("archive : fichier refusé : %s", hdr.Name)
		}
		raw, err := io.ReadAll(io.LimitReader(tr, maxFile+1))
		if err != nil {
			return nil, index, fmt.Errorf("archive illisible")
		}
		total += len(raw)
		if total > 4*MaxArchive {
			return nil, index, fmt.Errorf("archive trop grande")
		}
		if name == "index.json" {
			if err := json.Unmarshal(raw, &index); err != nil {
				return nil, index, fmt.Errorf("index.json illisible")
			}
			continue
		}
		files[name] = &fstest.MapFile{Data: raw, Mode: 0o644}
	}
	if index.Files == nil {
		return nil, index, fmt.Errorf("index.json est absent")
	}
	for name, file := range files {
		want, ok := index.Files[name]
		if !ok || !strings.EqualFold(want, Sum(file.Data)) {
			return nil, index, fmt.Errorf("archive : %s ne correspond pas à l'index", name)
		}
	}
	for name := range index.Files {
		if _, ok := files[name]; !ok {
			return nil, index, fmt.Errorf("archive : %s manque", name)
		}
	}
	return files, index, nil
}
