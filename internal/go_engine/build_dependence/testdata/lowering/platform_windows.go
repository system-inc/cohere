//go:build windows

package lowering

// Slots is 4 on darwin, 2 on linux and 8 on windows.
const Slots = 8

// stop returns on windows, as on linux.
func stop() {}

// platformLabel is initialized in the package initializer on every platform, from a different file in each.
var platformLabel = []string{"windows"}
