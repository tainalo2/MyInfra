//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"

	"myinfra/internal/boot"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// reportFatal shows the error of the app, which has no console. Otherwise
// it waits for Enter when this process is alone in its console: Explorer
// opened the window for it and closes it with the process.
func reportFatal(text string) {
	if inApp() {
		if home, err := boot.Home(); err == nil {
			text += "\n\nJournal : " + filepath.Join(home, "myinfra.log")
		}
		alert("MyInfra s'est arrêté", text)
		return
	}
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return
	}
	fmt.Fprint(os.Stderr, "\nAppuyez sur Entrée pour fermer.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
