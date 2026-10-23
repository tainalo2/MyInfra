//go:build windows

package main

import (
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sys/windows"

	"myinfra/internal/selfupdate"
)

// Installed by its setup, MyInfra.exe has no console: it shows the interface
// in its own window and stops with it, its PostgreSQL included. The
// executable is built with -H windowsgui, and gui says so.

var gui = ""

func init() {
	if inApp() {
		// The window answers on the thread main starts on.
		runtime.LockOSThread()
		selfupdate.Variant = "app"
	}
}

func inApp() bool { return gui == "1" }

// restartApp starts the updated executable, which opens its own window.
func restartApp() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return show && strings.HasPrefix(kv, "MYINFRA_NO_BROWSER=")
	})
	return cmd.Start()
}

// appLog keeps the output of the app, which has no console, in myinfra.log.
func appLog(home string) {
	path := filepath.Join(home, "myinfra.log")
	_ = os.Rename(path, path+".1")
	logs, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(logs.Fd()))
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(logs.Fd()))
	os.Stdout, os.Stderr = logs, logs
	log.SetOutput(logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
}

func alert(title, message string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(message)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
