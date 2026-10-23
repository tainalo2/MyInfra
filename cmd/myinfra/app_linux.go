//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"myinfra/internal/boot"
	"myinfra/internal/selfupdate"
)

// The applications menu starts MyInfra with --app, the AppImage too: it
// shows the interface in its own window and stops with it, its PostgreSQL
// included. Started from a terminal without it, nothing changes.

func init() {
	if inApp() {
		// GTK answers on the thread main starts on.
		runtime.LockOSThread()
	}
	if os.Getenv("APPIMAGE") != "" {
		selfupdate.Variant = "AppImage"
	}
}

func inApp() bool {
	return slices.Contains(os.Args[1:], "--app")
}

// restartApp starts the updated app in place of this process, with its
// window.
func restartApp() error {
	exe, err := selfupdate.Executable()
	if err != nil {
		return err
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return show && strings.HasPrefix(kv, "MYINFRA_NO_BROWSER=")
	})
	return syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), env)
}

// appLog keeps the output of the app, which has no terminal, in myinfra.log.
func appLog(home string) {
	path := filepath.Join(home, "myinfra.log")
	_ = os.Rename(path, path+".1")
	logs, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_ = unix.Dup2(int(logs.Fd()), 1)
	_ = unix.Dup2(int(logs.Fd()), 2)
}

// reportFatal shows the error of the app: it has no terminal.
func reportFatal(text string) {
	if !inApp() {
		return
	}
	if home, err := boot.Home(); err == nil {
		text += "\n\nJournal : " + filepath.Join(home, "myinfra.log")
	}
	alert("MyInfra s'est arrêté", text)
}

// alert shows a message with the dialog tool of the desktop, when it has one.
func alert(title, message string) {
	if path, err := exec.LookPath("zenity"); err == nil {
		_ = exec.Command(path, "--error", "--title", title, "--text", message).Run()
	} else if path, err := exec.LookPath("kdialog"); err == nil {
		_ = exec.Command(path, "--error", message, "--title", title).Run()
	}
}
