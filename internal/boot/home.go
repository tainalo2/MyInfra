package boot

import (
	"os"
	"path/filepath"
)

// Home est le dossier de la copie locale. MYINFRA_HOME l'emporte sur ~/.myinfra.
func Home() (string, error) {
	dir := os.Getenv("MYINFRA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".myinfra")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, os.Chmod(dir, 0o700)
}
