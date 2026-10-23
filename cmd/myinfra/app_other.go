//go:build !darwin

package main

// Only macOS opens MyInfra as an app.

func inApp() bool { return false }

func appRun(func()) bool { return false }

func onQuit(func()) {}

func restartApp() error { return nil }
