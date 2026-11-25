//go:build linux

package definedfrom

// platformExtra holds a Name, a string, on linux.
type platformExtra struct {
	Name string
}

// platformPart embeds Gadget on linux, so it promotes Spin.
type platformPart struct {
	Gadget
}
