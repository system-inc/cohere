//go:build linux

package lowering

// Slots is 4 on darwin and 2 on linux.
const Slots = 2

// stop returns on linux.
func stop() {}

// Loud is declared on linux only.
func (Speaker) Loud() string { return "loud" }

// platformLabel is initialized in the package initializer on every platform, from a different file in each.
var platformLabel = []string{"linux"}

// The blank identifier's initializer on linux only, which moves the blank ones after it.
var _ any = 1
