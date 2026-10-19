package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"myinfra/internal/boot"
)

// PreparePath ajoute les binaires installés par MyInfra devant le PATH.
func PreparePath() {
	home, err := boot.Home()
	if err != nil {
		return
	}
	for _, dir := range binDirs(home) {
		current := os.Getenv("PATH")
		prefix := dir + string(os.PathListSeparator)
		if !strings.Contains(current, dir) {
			os.Setenv("PATH", prefix+current)
		}
	}
}

func Ansible() string { return look("ansible-playbook") }

func Tofu() string { return look("tofu") }

func look(name string) string {
	home, err := boot.Home()
	if err == nil {
		for _, dir := range binDirs(home) {
			candidate := filepath.Join(dir, exe(name))
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	found, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return found
}

func binDirs(home string) []string {
	dirs := []string{
		filepath.Join(home, "bin"),
		filepath.Join(home, "ansible", "bin"),
	}
	if runtime.GOOS == "windows" {
		dirs = append(dirs, filepath.Join(home, "ansible", "Scripts"))
	}
	return dirs
}

func exe(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		return name + ".exe"
	}
	return name
}
