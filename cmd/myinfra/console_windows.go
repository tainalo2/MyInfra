//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// reportFatal waits for Enter when this process is alone in its console:
// Explorer opened the window for it and closes it with the process.
func reportFatal(string) {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return
	}
	fmt.Fprint(os.Stderr, "\nAppuyez sur Entrée pour fermer.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
