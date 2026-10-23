//go:build linux && !desktop

package main

// The executable alone has no window: it stays static, for any Linux, and
// --app opens the browser. The packages are built with -tags desktop.
func showWindow(string) bool { return false }
