//go:build windows || linux

package main

import (
	"os"
	"time"

	"myinfra/internal/boot"
)

// On Windows and Linux, the app is its window: closing it stops MyInfra.
// There is no Dock to open it again; the Start menu or the applications
// menu does, as for any program.

// show is whether the window opens, as MYINFRA_NO_BROWSER said at launch.
var show = true

// appRun runs MyInfra as an app when it is one, and returns false otherwise.
// run serves in the background while the main thread runs the window; the
// process ends with it.
func appRun(run func()) bool {
	if !inApp() {
		return false
	}
	// The window takes the place of the browser.
	show = os.Getenv("MYINFRA_NO_BROWSER") != "1"
	os.Setenv("MYINFRA_NO_BROWSER", "1")
	home, err := boot.Home()
	if err != nil {
		alert("MyInfra ne démarre pas", err.Error())
		return true
	}
	appLog(home)
	// Another MyInfra, from a terminal or an earlier launch, already
	// answers: two would share the same PostgreSQL folder. The window shows
	// that one, and closing it leaves it running.
	if answers() {
		if !showWindow(home) {
			openMyInfra()
		}
		return true
	}
	go func() {
		run()
		os.Exit(0)
	}()
	if !show {
		select {}
	}
	if !showWindow(home) {
		// No web engine: the browser, as from a terminal, and MyInfra runs
		// until the session ends.
		go func() {
			for !answers() {
				sleep()
			}
			openMyInfra()
		}()
		select {}
	}
	// The window is closed: MyInfra stops, and the process with run.
	requestQuit()
	select {}
}

func sleep() { time.Sleep(300 * time.Millisecond) }
