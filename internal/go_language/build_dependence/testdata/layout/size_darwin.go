//go:build darwin

package layout

// Size is 4 on darwin and 2 on linux.
const Size = 4

// platformName is declared in each platform's file.
func platformName() string { return "darwin" }
