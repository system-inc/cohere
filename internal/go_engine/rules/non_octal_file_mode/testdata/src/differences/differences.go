// Package differences holds the file modes where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a mode in a call's arguments spread from another call, a literal inside a
// nested call, a method's argument, a named type of the package's own whose underlying type is os.FileMode, and a
// generic function's argument constrained to file modes.
package differences

import "os"

type permissions os.FileMode

type opener struct{}

func (opener) open(name string, mode os.FileMode) {}

func modes() (string, int, os.FileMode) { return "", 0, 0 }

func generic[Mode ~uint32](mode Mode) {}

func plausible(handle opener) {
	os.OpenFile(modes())
	os.Chmod("", os.FileMode(uint32(644)))
	handle.open("", 644)
	_ = permissions(644)
	generic[os.FileMode](644)
	generic(os.FileMode(755))
}
