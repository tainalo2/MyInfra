package sshk

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateAndParse(t *testing.T) {
	pair, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pair.Public, "ssh-ed25519 ") {
		t.Fatalf("public %q", pair.Public)
	}
	if !strings.Contains(pair.Private, "OPENSSH PRIVATE KEY") {
		t.Fatal(pair.Private)
	}
	block, err := ssh.ParseRawPrivateKey([]byte(pair.Private))
	if err != nil || block == nil {
		t.Fatal(err)
	}
	normalized, fp, err := ParsePublic(pair.Public + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != pair.Public || fp != pair.Fingerprint {
		t.Fatalf("normalized %q fp %q", normalized, fp)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, _, err := ParsePublic("bonjour"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHint(t *testing.T) {
	if Hint("abcdefgh") != "efgh" {
		t.Fatal(Hint("abcdefgh"))
	}
	if Hint("ab") != "ab" {
		t.Fatal(Hint("ab"))
	}
}

func TestParsePrivate(t *testing.T) {
	pair, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePrivate("  " + pair.Private + "\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Public != pair.Public || got.Fingerprint != pair.Fingerprint || got.Private != pair.Private {
		t.Fatalf("got %+v", got)
	}
	if _, err := ParsePrivate(pair.Public); err == nil || err == ErrPassphrase {
		t.Fatalf("public key read as private: %v", err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePrivate(string(pem.EncodeToMemory(block))); err != ErrPassphrase {
		t.Fatalf("locked key: %v", err)
	}
}
