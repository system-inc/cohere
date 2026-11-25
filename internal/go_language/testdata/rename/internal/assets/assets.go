// Package assets holds names whose directives a rename leaves as they are written: a tool reads a directive's text
// exactly, and no load resolves it.
package assets

import (
	"embed"
	_ "unsafe"
)

// cfg holds the settings files its directive embeds, from a directory of its own name.
//
//go:embed cfg/*
var cfg embed.FS

// modeID is a mode, whose generator directive names the type as it is written.
//
//go:generate stringer -type=modeID
type modeID int

// clockID is the runtime's clock, which its linkname names, so it keeps its name.
//
// Unsafe: runtime.nanotime takes no arguments and returns an int64, the signature declared here.
//
//go:linkname clockID runtime.nanotime
func clockID() int64

// Settings reads the settings file, with the mode and the clock.
func Settings() ([]byte, modeID, int64, error) {
	contents, readError := cfg.ReadFile("cfg/settings.txt")
	return contents, modeID(0), clockID(), readError
}
