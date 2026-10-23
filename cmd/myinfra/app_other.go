//go:build !darwin

package main

// appStart has nothing to hand over: only macOS opens MyInfra as an app.
func appStart() bool { return false }

func backgroundApp() bool { return false }
