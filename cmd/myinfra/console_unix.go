//go:build !windows && !darwin && !linux

package main

// reportFatal does nothing: a terminal outlives the process it started.
func reportFatal(string) {}
