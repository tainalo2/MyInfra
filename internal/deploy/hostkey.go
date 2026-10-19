package deploy

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

var (
	hostKeyMu sync.Mutex
	knownKeys = map[string]string{}
	loadKey   func(address string) (string, error)
	saveKey   func(address, key string) error
)

func UseHostKeys(load func(string) (string, error), save func(string, string) error) {
	hostKeyMu.Lock()
	loadKey, saveKey = load, save
	hostKeyMu.Unlock()
}

// ForgetHostKey drops the key remembered for an address. A machine just
// bought is a new host, even when the provider gives back the address of a
// machine destroyed before.
func ForgetHostKey(address string) {
	hostKeyMu.Lock()
	delete(knownKeys, address)
	hostKeyMu.Unlock()
}

func checkHostKey(address string, key ssh.PublicKey) error {
	presented := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if presented == "" {
		return fmt.Errorf("La clé d'hôte SSH est illisible.")
	}
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	if have, ok := knownKeys[address]; ok {
		if have != presented {
			return fmt.Errorf("La clé d'hôte SSH a changé.")
		}
		if saveKey != nil {
			if err := saveKey(address, presented); err != nil {
				return fmt.Errorf("La clé d'hôte SSH n'a pas pu être retenue.")
			}
		}
		return nil
	}
	if loadKey != nil {
		stored, err := loadKey(address)
		if err != nil {
			return fmt.Errorf("La clé d'hôte SSH n'a pas pu être lue.")
		}
		stored = strings.TrimSpace(stored)
		if stored != "" {
			knownKeys[address] = stored
			if stored != presented {
				return fmt.Errorf("La clé d'hôte SSH a changé.")
			}
			return nil
		}
	}
	knownKeys[address] = presented
	if saveKey != nil {
		if err := saveKey(address, presented); err != nil {
			delete(knownKeys, address)
			return fmt.Errorf("La clé d'hôte SSH n'a pas pu être retenue.")
		}
	}
	return nil
}
