//go:build darwin

package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"myinfra/internal/boot"
)

// Opened from the Finder, MyInfra.app has no window: the process the double
// click starts hands over to a detached copy and ends at once. A later
// double click starts it again, finds the copy running and opens the browser.

const backgroundEnv = "MYINFRA_BACKGROUND"

// appLock stays open while the background copy runs: its lock tells a new
// double click that MyInfra is already there.
var appLock *os.File

func inApp() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

func backgroundApp() bool {
	return inApp() && os.Getenv(backgroundEnv) == "1"
}

// appStart returns true when this process has nothing more to do: it handed
// over to the background copy, or one already runs.
func appStart() bool {
	if !inApp() {
		return false
	}
	home, err := boot.Home()
	if err != nil {
		alert("MyInfra ne démarre pas", err.Error())
		return true
	}
	lockPath := filepath.Join(home, "myinfra.lock")
	if os.Getenv(backgroundEnv) == "1" {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			fatal("verrou", err)
		}
		if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			// Two double clicks in a row: the first copy carries on.
			return true
		}
		appLock = file
		appPath()
		return false
	}
	if running(lockPath) {
		// Still starting, the copy opens the browser itself once it answers.
		url := localURL(listenAddr())
		client := http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Get(url + "/api/health"); err == nil {
			resp.Body.Close()
			boot.OpenBrowser(url)
		}
		return true
	}
	logPath := filepath.Join(home, "myinfra.log")
	_ = os.Rename(logPath, logPath+".1")
	logs, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		alert("MyInfra ne démarre pas", err.Error())
		return true
	}
	defer logs.Close()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), backgroundEnv+"=1")
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		alert("MyInfra ne démarre pas", err.Error())
	}
	return true
}

// running says whether a background copy holds the lock.
func running(lockPath string) bool {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer file.Close()
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

// appPath adds the folders of Homebrew: an app opened from the Finder only
// gets the system ones.
func appPath() {
	path := os.Getenv("PATH")
	for _, dir := range []string{"/usr/local/bin", "/opt/homebrew/bin"} {
		if !strings.Contains(path, dir) {
			path += string(os.PathListSeparator) + dir
		}
	}
	os.Setenv("PATH", path)
}

// reportFatal shows the error of the background copy: it has no terminal.
func reportFatal(text string) {
	if !backgroundApp() {
		return
	}
	if home, err := boot.Home(); err == nil {
		text += "\n\nJournal : " + filepath.Join(home, "myinfra.log")
	}
	alert("MyInfra s'est arrêté", text)
}

func alert(title, message string) {
	script := "display alert " + appleString(title) + " message " + appleString(message) + " as critical"
	_ = exec.Command("osascript", "-e", script).Run()
}

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
