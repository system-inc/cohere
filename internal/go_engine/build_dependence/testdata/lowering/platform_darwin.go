//go:build darwin

package lowering

// Slots is 4 on darwin and 2 on linux.
const Slots = 4

// stop never returns on darwin.
func stop() { panic("stop") }

// platformLabel is initialized in the package initializer on every platform, from a different file in each.
var platformLabel = []string{"darwin"}
