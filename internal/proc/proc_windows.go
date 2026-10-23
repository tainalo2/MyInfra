//go:build windows

package proc

import (
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

var procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// noConsole is true for the app: started from a terminal, commands share its
// console and Ctrl+C still reaches them.
var noConsole = sync.OnceValue(func() bool {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return hwnd == 0
})

func hide(cmd *exec.Cmd) {
	if !noConsole() || os.Getenv("MYINFRA_SHOW_CONSOLES") == "1" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
