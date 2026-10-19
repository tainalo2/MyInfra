package sshk

import (
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
