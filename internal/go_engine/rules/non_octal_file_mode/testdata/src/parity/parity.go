// Package parity holds the file modes SA9002 judges, flagged and left alone, for the port and the original to judge
// side by side: literals of three octal digits passed as os.FileMode and io/fs.FileMode, to the standard library, to
// a function of the package's own and through a conversion, and the literals and arguments it leaves alone.
package parity

import (
	"io/fs"
	"os"
)

type mode uint32

func takesFileSystemMode(name string, permissions fs.FileMode) {}

func takesOwnMode(permissions mode) {}

func takesInteger(permissions int) {}

func flagged() {
	os.OpenFile("", 0, 644)
	os.Chmod("", 755)
	os.MkdirAll("", 700)
	os.WriteFile("", nil, 600)
	takesFileSystemMode("", 100)
	takesFileSystemMode("", 777)
	_ = os.FileMode(640)
	_ = fs.FileMode(444)
}

func spared(permissions os.FileMode) {
	os.OpenFile("", 0, 0644)
	os.OpenFile("", 0, 0o644)
	os.OpenFile("", 0, 800)
	os.OpenFile("", 0, 678)
	os.OpenFile("", 0, 1644)
	os.OpenFile("", 0, 64)
	os.OpenFile("", 0, 0x1a4)
	os.OpenFile("", 0, 6_4)
	os.OpenFile("", 0, (644))
	os.OpenFile("", 0, 644|permissions)
	os.OpenFile("", 0, permissions)
	takesOwnMode(644)
	takesInteger(644)
}
