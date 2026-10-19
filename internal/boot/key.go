package boot

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

// Master lit la clé du dossier, ou l'écrit au premier passage.
// MYINFRA_MASTER_KEY, quand elle est posée, l'emporte et n'est pas recopiée.
func Master(home string) ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv("MYINFRA_MASTER_KEY")); raw != "" {
		return DecodeMaster(raw)
	}
	path := filepath.Join(home, "master.key")
	raw, err := os.ReadFile(path)
	if err == nil {
		return DecodeMaster(string(raw))
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func DecodeMaster(raw string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(key) != 32 {
		return nil, errString("MYINFRA_MASTER_KEY doit être 32 octets en base64")
	}
	return key, nil
}

type errString string

func (e errString) Error() string { return string(e) }
