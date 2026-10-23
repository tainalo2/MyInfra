//go:build !darwin && !windows && !linux

package main

// Only macOS, Windows and Linux open MyInfra as an app.

func inApp() bool { return false }

func appRun(func()) bool { return false }

func onQuit(func()) {}

func restartApp() error { return nil }
