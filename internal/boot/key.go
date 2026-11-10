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

// BYOKKeys is the master key that seals the users' own provider keys (BYOK),
// with the key it replaces during a rotation. It is a key of its own,
// independent of the master key of the box: rotating one never touches the
// other.
type BYOKKeys struct {
	// Current seals and opens the provider keys. Nil: the assistant stays
	// off, MyInfra runs without it.
	Current []byte
	// Previous, when a rotation is under way, still opens the keys sealed
	// before it; they are sealed again with Current.
	Previous []byte
}

// BYOKFile is the file of the BYOK master key, next to master.key, written
// at the first start of a local copy.
const BYOKFile = "byok.key"

// BYOKMaster resolves the master key of the BYOK provider keys.
//
// MYINFRA_BYOK_MASTER_KEY wins when it is set: 32 bytes in base64, as the
// other master key. Otherwise a local copy reads <home>/byok.key, written at
// the first start. A copy on a server, with no home, gets no key: the
// assistant stays off until the variable is set. A key that cannot be read
// is an error; the caller turns the assistant off and keeps serving.
//
// MYINFRA_BYOK_MASTER_KEY_PREVIOUS carries the key being replaced during a
// rotation, in the same form.
func BYOKMaster(home string) (BYOKKeys, error) {
	var keys BYOKKeys
	if raw := strings.TrimSpace(os.Getenv("MYINFRA_BYOK_MASTER_KEY_PREVIOUS")); raw != "" {
		previous, err := decodeBYOK(raw, "MYINFRA_BYOK_MASTER_KEY_PREVIOUS")
		if err != nil {
			return BYOKKeys{}, err
		}
		keys.Previous = previous
	}
	if raw := strings.TrimSpace(os.Getenv("MYINFRA_BYOK_MASTER_KEY")); raw != "" {
		current, err := decodeBYOK(raw, "MYINFRA_BYOK_MASTER_KEY")
		if err != nil {
			return BYOKKeys{}, err
		}
		keys.Current = current
		return keys, nil
	}
	if home == "" {
		return keys, nil
	}
	path := filepath.Join(home, BYOKFile)
	raw, err := os.ReadFile(path)
	if err == nil {
		current, err := decodeBYOK(string(raw), BYOKFile)
		if err != nil {
			return BYOKKeys{}, err
		}
		keys.Current = current
		return keys, nil
	}
	if !os.IsNotExist(err) {
		return BYOKKeys{}, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return BYOKKeys{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return BYOKKeys{}, err
	}
	keys.Current = key
	return keys, nil
}

func decodeBYOK(raw, source string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(key) != 32 {
		return nil, errString(source + " doit être 32 octets en base64")
	}
	return key, nil
}

type errString string

func (e errString) Error() string { return string(e) }
