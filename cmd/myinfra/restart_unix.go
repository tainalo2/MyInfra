//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restart replaces this process by the executable now at exe, with the
// same arguments and environment.
func restart(exe string) error {
	return syscall.Exec(exe, os.Args, append(os.Environ(), "MYINFRA_NO_BROWSER=1"))
}
