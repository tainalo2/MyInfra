package library

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing/fstest"
)

// trusted are the public keys that sign the releases of the library. A new
// key is added here a version of MyInfra before releases are signed with it.
var trusted = []string{
	"Uxo30T5FH8Y+dMcgfRAdH9x3BlwQFK2TPrlaeU0mF9E=",
}

// snapshot is the release built into this MyInfra: what it proposes before
// it has fetched one, or without network.
//
//go:embed snapshot
var snapshot embed.FS

var (
	// Engine is the version of this MyInfra, set before the library is read.
	Engine = "dev"

	current atomic.Pointer[Library]
	first   sync.Once
)

// Keys are the keys a release must be signed with.
func Keys() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, raw := range trusted {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err == nil && len(key) == ed25519.PublicKeySize {
			out = append(out, ed25519.PublicKey(key))
		}
	}
	return out
}

// Current is the library MyInfra proposes now: the last release it adopted,
// else the one built in. MYINFRA_LIBRARY_DIR, in development, reads a
// working copy of the library repository instead.
func Current() *Library {
	if lib := current.Load(); lib != nil {
		return lib
	}
	first.Do(func() {
		lib, err := initial()
		if err != nil {
			slog.Error("bibliothèque", "err", err)
			lib = &Library{logos: map[string]string{}, files: fstest.MapFS{}}
		}
		current.CompareAndSwap(nil, lib)
	})
	return current.Load()
}

func initial() (*Library, error) {
	if dir := FromDir(); dir != "" {
		return Read(os.DirFS(dir), Dev, Engine)
	}
	return Open(Snapshot(), Keys(), Engine)
}

// FromDir is the working copy MyInfra reads in development, "" otherwise.
func FromDir() string {
	if os.Getenv("MYINFRA_DEV") != "1" {
		return ""
	}
	return os.Getenv("MYINFRA_LIBRARY_DIR")
}

// Set makes lib the library MyInfra proposes.
func Set(lib *Library) {
	Current()
	current.Store(lib)
}

// Snapshot is the release built into this MyInfra.
func Snapshot() Bundle {
	read := func(name string) []byte {
		raw, _ := snapshot.ReadFile("snapshot/" + name)
		return raw
	}
	return Bundle{Archive: read(ArchiveName), Sums: read(SumsName), Signature: read(SignatureName)}
}
