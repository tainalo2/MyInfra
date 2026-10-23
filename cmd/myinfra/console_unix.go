//go:build !windows && !darwin

package main

// reportFatal does nothing: a terminal outlives the process it started.
func reportFatal(string) {}
