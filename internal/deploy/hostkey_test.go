package deploy

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestHostKeyIsPinnedAfterTheFirstConnection(t *testing.T) {
	hostKeyMu.Lock()
	knownKeys = map[string]string{}
	loadKey = nil
	saveKey = nil
	hostKeyMu.Unlock()

	first := testPublicKey(t, 1)
	second := testPublicKey(t, 2)
	if err := checkHostKey("203.0.113.10", first); err != nil {
		t.Fatal(err)
	}
	if err := checkHostKey("203.0.113.10", first); err != nil {
		t.Fatal(err)
	}
	if err := checkHostKey("203.0.113.10", second); err == nil || !strings.Contains(err.Error(), "changé") {
		t.Fatal(err)
	}
}

func testPublicKey(t *testing.T, seed byte) ssh.PublicKey {
	t.Helper()
	secret := make([]byte, ed25519.SeedSize)
	secret[0] = seed
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = secret
	public, err := ssh.NewPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	if seed == 2 {
		_, private, err = ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		public, err = ssh.NewPublicKey(private.Public())
		if err != nil {
			t.Fatal(err)
		}
	}
	return public
}
