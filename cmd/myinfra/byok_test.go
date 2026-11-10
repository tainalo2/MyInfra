package main

import (
	"bytes"
	"encoding/base64"
	"testing"

	"myinfra/internal/boot"
)

// A server copy without MYINFRA_BYOK_MASTER_KEY gets no key, and no error:
// the assistant is off, the server starts.
func TestBYOKKeysServerCopy(t *testing.T) {
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", "")
	t.Setenv("MYINFRA_BYOK_MASTER_KEY_PREVIOUS", "")
	master, previous, sealed := byokKeys(&boot.Runtime{})
	if master != nil || previous != nil || sealed != nil {
		t.Fatal("sans variable, pas de clé")
	}
	// An unreadable key is reported, not fatal.
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", "invalide")
	if master, _, _ := byokKeys(&boot.Runtime{}); master != nil {
		t.Fatal("une clé invalide doit désactiver l'assistant")
	}
	key := bytes.Repeat([]byte{4}, 32)
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", base64.StdEncoding.EncodeToString(key))
	if master, _, _ := byokKeys(&boot.Runtime{}); !bytes.Equal(master, key) {
		t.Fatal("la variable doit être lue")
	}
}
