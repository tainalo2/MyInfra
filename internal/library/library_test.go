package library

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// fixture is testdata/lib in memory, for each test to change.
func fixture(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	err := fs.WalkDir(os.DirFS("testdata/lib"), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile("testdata/lib/" + name)
		out[name] = &fstest.MapFile{Data: raw}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReadsALibrary(t *testing.T) {
	lib, err := Read(fixture(t), Dev, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Packages) != 2 || len(lib.Templates) != 1 || len(lib.Categories) != 2 {
		t.Fatalf("%d %d %d", len(lib.Packages), len(lib.Templates), len(lib.Categories))
	}
	p, ok := lib.Package("umami")
	if !ok || p.Version != "1.0.0" || p.Sum == "" || p.Pkg.Name != "Umami" {
		t.Fatalf("%+v", p)
	}
	if _, kind, ok := lib.Logo("memos"); !ok || kind != "image/svg+xml" {
		t.Fatal("logo")
	}
	if lib.Category("Sites") == nil || lib.Category("Sites").Translations["de"] == "" {
		t.Fatal("category")
	}
}

// A library that does not hold together is refused with what to fix.
func TestRefusesABrokenLibrary(t *testing.T) {
	cases := []struct {
		name   string
		change func(fstest.MapFS)
		where  string
	}{
		{"unlisted", func(f fstest.MapFS) { f["order.txt"].Data = []byte("umami\n") }, "manque dans order.txt"},
		{"missing", func(f fstest.MapFS) { delete(f, "packages/memos/package.yaml") }, "memos/package.yaml est absent"},
		{"invalid", func(f fstest.MapFS) { edit(f, "packages/umami/package.yaml", "port: 3000", "port: 99999") }, "ligne"},
		{"untranslated", func(f fstest.MapFS) { edit(f, "packages/umami/package.yaml", "    de:", "    xx:") }, "translations"},
		{"unread", func(f fstest.MapFS) {
			edit(f, "packages/umami/package.yaml", "services:", "inputs:\n  - key: unused\n    label: Inutile\nservices:")
		}, "unused"},
		{"category", func(f fstest.MapFS) { edit(f, "packages/umami/package.yaml", "category: Sites", "category: Ailleurs") }, "categories.yaml"},
		{"version", func(f fstest.MapFS) { f["packages/umami/item.yaml"].Data = []byte("version: 1.0\n") }, "version"},
		{"unknown field", func(f fstest.MapFS) { f["packages/umami/item.yaml"].Data = []byte("version: 1.0.0\nauthor: moi\n") }, "item.yaml"},
		{"planes", func(f fstest.MapFS) {
			edit(f, "templates/site/template.yaml", "controlPlane: true", "controlPlane: false")
		}, "impair"},
		{"package", func(f fstest.MapFS) { edit(f, "templates/site/template.yaml", "package: umami", "package: absent") }, "absent"},
		{"logo", func(f fstest.MapFS) { f["logos/memos.gif"] = &fstest.MapFile{Data: []byte("x")} }, "logos/memos.gif"},
		{"template order", func(f fstest.MapFS) { f["templates/order.txt"] = &fstest.MapFile{Data: []byte("site\nabsent\n")} }, "absent"},
		{"template unlisted", func(f fstest.MapFS) { f["templates/order.txt"] = &fstest.MapFile{Data: []byte("\n")} }, "manque dans templates/order.txt"},
	}
	for _, c := range cases {
		files := fixture(t)
		c.change(files)
		_, err := Read(files, Dev, "")
		if err == nil || !strings.Contains(err.Error(), c.where) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

func edit(f fstest.MapFS, name, old, next string) {
	f[name].Data = []byte(strings.Replace(string(f[name].Data), old, next, 1))
}

// An older MyInfra leaves out what asks for a newer one, and the templates
// that need it; a copy built from source reads everything.
func TestLeavesOutWhatIsTooNew(t *testing.T) {
	files := fixture(t)
	files["packages/umami/item.yaml"].Data = []byte("version: 1.1.0\nminMyInfra: 9.0.0\n")
	lib, err := Read(files, Dev, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lib.Package("umami"); ok || len(lib.Templates) != 0 || len(lib.Left) != 2 {
		t.Fatalf("%v %d", lib.Left, len(lib.Templates))
	}
	lib, err = Read(files, Dev, "dev")
	if err != nil || len(lib.Packages) != 2 || len(lib.Templates) != 1 {
		t.Fatal(err)
	}
}

// A release opens only with its signature, its sums and its index.
func TestOpensASignedRelease(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	b, err := Pack(fixture(t), "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Pack(fixture(t), "1.2.0")
	if !bytes.Equal(b.Archive, again.Archive) {
		t.Fatal("the same tree gives another archive")
	}
	b.Signature = Sign(b.Sums, private)
	lib, err := Open(b, []ed25519.PublicKey{other, public}, "")
	if err != nil || lib.Release != "1.2.0" || len(lib.Packages) != 2 {
		t.Fatal(err)
	}
	if _, err := Open(b, []ed25519.PublicKey{other}, ""); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatal(err)
	}
	tampered := b
	tampered.Archive = append(append([]byte(nil), b.Archive...), 0)
	if _, err := Open(tampered, []ed25519.PublicKey{public}, ""); err == nil {
		t.Fatal("a changed archive opened")
	}
	tampered = b
	tampered.Sums = []byte(strings.Repeat("0", 64) + "  " + ArchiveName + "\n")
	if _, err := Open(tampered, []ed25519.PublicKey{public}, ""); err == nil {
		t.Fatal("changed sums opened")
	}
	tampered = b
	tampered.Release = "1.3.0"
	if _, err := Open(tampered, []ed25519.PublicKey{public}, ""); err == nil {
		t.Fatal("another release opened")
	}
}

// A changed item needs a newer version; an id never goes away.
func TestBumps(t *testing.T) {
	before, _ := Read(fixture(t), "1.0.0", "")
	files := fixture(t)
	edit(files, "packages/umami/package.yaml", "Umami", "Umami Analytics")
	after, _ := Read(files, "1.1.0", "")
	if problems := Bumps(before, after); len(problems) != 1 || !strings.Contains(problems[0], "umami") {
		t.Fatal(problems)
	}
	files["packages/umami/item.yaml"].Data = []byte("version: 1.0.1\n")
	after, _ = Read(files, "1.1.0", "")
	if problems := Bumps(before, after); len(problems) != 0 {
		t.Fatal(problems)
	}
	if Level("1.0.0", "1.0.1") != "patch" || Level("1.0.0", "1.2.0") != "minor" || Level("1.4.0", "2.0.0") != "major" {
		t.Fatal("level")
	}
}

func TestNewest(t *testing.T) {
	lib, _ := Read(fixture(t), "1.0.0", "")
	origin := Origin{Repo: Repo(), Kind: "package", ID: "umami", Version: "0.9.0"}
	if version, _ := lib.Newest(origin); version != "1.0.0" {
		t.Fatal(version)
	}
	origin.Version = "1.0.0"
	if version, _ := lib.Newest(origin); version != "" {
		t.Fatal(version)
	}
	origin.Repo = "someone/else"
	origin.Version = "0.1.0"
	if version, _ := lib.Newest(origin); version != "" {
		t.Fatal(version)
	}
}

// The release built into MyInfra opens with the keys MyInfra trusts.
func TestSnapshotOpens(t *testing.T) {
	lib, err := Open(Snapshot(), Keys(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Packages) < 80 || len(lib.Templates) == 0 || lib.Release == "" {
		t.Fatalf("%d %d %s", len(lib.Packages), len(lib.Templates), lib.Release)
	}
}
