// Command library checks, packs and signs the library of MyInfra: the
// packages and templates published apart from the engine. The library
// repository runs it in its CI, from a checkout of MyInfra, so that the
// rules a release must follow are those MyInfra reads with.
//
//	library check <dir> [-against <archive|latest>]
//	library pack <dir> <version> <out>
//	library sign <SHA256SUMS>             key in LIBRARY_SIGNING_KEY
//	library verify <dir with the three files of a release>
//	library keygen <private key file>
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"myinfra/internal/library"
	"myinfra/internal/selfupdate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "check":
		err = check(args)
	case "pack":
		err = packCmd(args)
	case "sign":
		err = sign(args)
	case "verify":
		err = verifyCmd(args)
	case "keygen":
		err = keygen(args)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "library :", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage : library check <dir> [-against <archive|latest>] | pack <dir> <version> <out> | sign <SHA256SUMS> | verify <dir> | keygen <fichier>")
	os.Exit(2)
}

// check reads the library as MyInfra does. Against a previous release, it
// also asks that a changed item has a newer version.
func check(args []string) error {
	flags := flag.NewFlagSet("check", flag.ExitOnError)
	against := flags.String("against", "", "archive de la version précédente, ou latest")
	if len(args) < 1 {
		usage()
	}
	dir := args[0]
	_ = flags.Parse(args[1:])
	lib, err := library.Read(os.DirFS(dir), library.Dev, "")
	if err != nil {
		return err
	}
	fmt.Printf("%d paquets, %d templates, %d catégories\n", len(lib.Packages), len(lib.Templates), len(lib.Categories))
	if *against == "" {
		return nil
	}
	previous, err := previousRelease(*against)
	if err != nil {
		return err
	}
	if previous == nil {
		fmt.Println("aucune version précédente")
		return nil
	}
	problems := library.Bumps(previous, lib)
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d éléments changés sans nouvelle version", len(problems))
	}
	fmt.Printf("versions comparées à %s\n", previous.Release)
	return nil
}

func previousRelease(against string) (*library.Library, error) {
	if against != "latest" {
		archive, err := os.ReadFile(against)
		if err != nil {
			return nil, err
		}
		return library.ReadArchive(archive)
	}
	ctx := context.Background()
	release, assets, err := library.Latest(ctx, library.Repo())
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	b, err := library.Download(ctx, release, assets)
	if err != nil {
		return nil, err
	}
	return library.Open(b, library.Keys(), "")
}

func packCmd(args []string) error {
	if len(args) != 3 {
		usage()
	}
	dir, version, out := args[0], strings.TrimPrefix(args[1], "v"), args[2]
	if !selfupdate.Valid(version) {
		return fmt.Errorf("version illisible : %s", args[1])
	}
	b, err := library.Pack(os.DirFS(dir), version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, library.ArchiveName), b.Archive, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, library.SumsName), b.Sums, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s %s (%d octets)\n", library.ArchiveName, version, len(b.Archive))
	return nil
}

func sign(args []string) error {
	if len(args) != 1 {
		usage()
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("LIBRARY_SIGNING_KEY")))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return fmt.Errorf("LIBRARY_SIGNING_KEY doit être la clé privée ed25519 en base64")
	}
	sums, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	out := filepath.Join(filepath.Dir(args[0]), library.SignatureName)
	return os.WriteFile(out, library.Sign(sums, ed25519.PrivateKey(raw)), 0o644)
}

func verifyCmd(args []string) error {
	if len(args) != 1 {
		usage()
	}
	read := func(name string) []byte {
		raw, _ := os.ReadFile(filepath.Join(args[0], name))
		return raw
	}
	b := library.Bundle{Archive: read(library.ArchiveName), Sums: read(library.SumsName), Signature: read(library.SignatureName)}
	lib, err := library.Open(b, library.Keys(), "")
	if err != nil {
		return err
	}
	fmt.Printf("version %s : %d paquets, %d templates, signature reconnue\n", lib.Release, len(lib.Packages), len(lib.Templates))
	return nil
}

// keygen writes a new private key in a file only its owner reads, and
// prints the public key: the private one never goes through a terminal.
func keygen(args []string) error {
	if len(args) != 1 {
		usage()
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(base64.StdEncoding.EncodeToString(private) + "\n"); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Println("clé publique, à mettre dans internal/library/current.go :", base64.StdEncoding.EncodeToString(public))
	fmt.Println("clé privée écrite dans", args[0], ": à mettre dans le secret LIBRARY_SIGNING_KEY du dépôt de la bibliothèque")
	return nil
}
