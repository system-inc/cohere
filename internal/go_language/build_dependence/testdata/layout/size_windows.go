//go:build windows

package layout

// Size is 4 on darwin, 2 on linux and 8 on windows.
const Size = 8

// platformName is declared in each platform's file.
func platformName() string { return "windows" }
