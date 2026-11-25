// Package dependency stands for a dependency of the module, a package outside the workspace.
package dependency

// Cell's underlying hangs on Width.
type Cell struct {
	cells [Width]byte
}

// Label is the same in every build.
const Label = "cell"

// Wide reads Width: dependent.
func Wide() bool { return Width > 8 }
