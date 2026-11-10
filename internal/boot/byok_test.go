package boot

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestBYOKMasterAbsentIsNotAnError(t *testing.T) {
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", "")
	t.Setenv("MYINFRA_BYOK_MASTER_KEY_PREVIOUS", "")
	keys, err := BYOKMaster("")
	if err != nil {
		t.Fatal(err)
	}
	if keys.Current != nil || keys.Previous != nil {
		t.Fatal("sans dossier ni variable, pas de clé")
	}
}

func TestBYOKMasterFileIsItsOwn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MYINFRA_MASTER_KEY", "")
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", "")
	t.Setenv("MYINFRA_BYOK_MASTER_KEY_PREVIOUS", "")
	master, err := Master(home)
	if err != nil {
		t.Fatal(err)
	}
	first, err := BYOKMaster(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Current) != 32 {
		t.Fatalf("clé %d", len(first.Current))
	}
	if bytes.Equal(first.Current, master) {
		t.Fatal("la clé BYOK doit être distincte de la clé maîtresse du coffre")
	}
	second, err := BYOKMaster(home)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Current, second.Current) {
		t.Fatal("la clé a changé")
	}
	info, err := os.Stat(filepath.Join(home, BYOKFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %o", info.Mode().Perm())
	}
	// Rotating the box key leaves the BYOK key alone.
	t.Setenv("MYINFRA_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	third, err := BYOKMaster(home)
	if err != nil || !bytes.Equal(first.Current, third.Current) {
		t.Fatal("la rotation de MYINFRA_MASTER_KEY ne doit pas toucher la clé BYOK")
	}
}

func TestBYOKMasterEnvWinsAndCarriesPrevious(t *testing.T) {
	home := t.TempDir()
	current := bytes.Repeat([]byte{1}, 32)
	previous := bytes.Repeat([]byte{2}, 32)
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", base64.StdEncoding.EncodeToString(current))
	t.Setenv("MYINFRA_BYOK_MASTER_KEY_PREVIOUS", base64.StdEncoding.EncodeToString(previous))
	keys, err := BYOKMaster(home)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(keys.Current, current) || !bytes.Equal(keys.Previous, previous) {
		t.Fatal("variables mal lues")
	}
	if _, err := os.Stat(filepath.Join(home, BYOKFile)); !os.IsNotExist(err) {
		t.Fatal("la variable ne doit pas écrire de fichier")
	}
}

func TestBYOKMasterInvalidIsAnError(t *testing.T) {
	t.Setenv("MYINFRA_BYOK_MASTER_KEY_PREVIOUS", "")
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", "court")
	if _, err := BYOKMaster(""); err == nil {
		t.Fatal("une clé trop courte doit être refusée")
	}
	t.Setenv("MYINFRA_BYOK_MASTER_KEY", "")
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, BYOKFile), []byte("pas du base64\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BYOKMaster(home); err == nil {
		t.Fatal("un fichier illisible doit être signalé, pas remplacé")
	}
}
