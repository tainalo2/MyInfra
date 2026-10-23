package ui

import "embed"

// Dist is the interface ./build copies from web/dist. Git only keeps
// dist/.keep: built without it, MyInfra says so instead of showing it.
//
//go:embed all:dist
var Dist embed.FS
