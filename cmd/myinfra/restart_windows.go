//go:build windows

package main

import (
	"os"
	"os/exec"
)

// restart starts the executable now at exe, then this process ends:
// Windows cannot replace a running process in place.
func restart(exe string) error {
	if inApp() {
		return restartApp()
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "MYINFRA_NO_BROWSER=1")
	return cmd.Start()
}
