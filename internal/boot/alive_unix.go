//go:build !windows

package boot

import "syscall"

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
