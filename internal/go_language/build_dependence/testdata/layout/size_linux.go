//go:build linux

package layout

// Size is 4 on darwin and 2 on linux.
const Size = 2

// platformName is declared in each platform's file.
func platformName() string { return "linux" }
