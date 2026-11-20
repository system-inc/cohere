// Package dependency stands for a dependency of the module, a package outside the workspace, whose Pad the module's
// own variables hold.
package dependency

// Pad is padSize bytes, as the module's own Pad is: its underlying moves with the architecture and its name does not.
type Pad struct {
	_ [padSize]byte
}
