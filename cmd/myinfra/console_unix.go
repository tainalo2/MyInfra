//go:build !windows

package main

// holdConsole does nothing: a terminal outlives the process it started.
func holdConsole() {}
