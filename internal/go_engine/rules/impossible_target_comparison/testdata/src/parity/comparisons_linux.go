package parity

import (
	"runtime"
	rt "runtime"
)

const windowsName = "windows"

// The file's name constrains it to linux.
func comparisons() {
	use(runtime.GOOS == "windows")   // Found.
	use(runtime.GOOS != "windows")   // Found: not equal is judged as equal is.
	use(runtime.GOOS == "android")   // Not found: android is linux.
	use(runtime.GOOS != "linux")     // Not found.
	use((runtime.GOOS == "windows")) // Found twice: the parentheses and the comparison inside them.
	use(runtime.GOOS == ("windows")) // Found: the literal in parentheses.
	use((runtime.GOOS) == "windows") // Found: the target in parentheses.
	use(runtime.GOOS == `windows`)   // Found: a raw string.
	use(rt.GOOS == "plan9")          // Found: the package by another name.
	use("windows" == runtime.GOOS)   // Not found: the literal on the left.
	use(runtime.GOOS == windowsName) // Not found: a constant, no literal.
	use(runtime.GOOS == "DomOS")     // Not found: no operating system the knowledge holds.
	use(runtime.GOOS < "windows")    // Not found: no equality.
	use(runtime.GOARCH == "arm64")   // Not found: linux leaves the architecture open.
	GOOS := "linux"
	use(GOOS == "windows") // Not found: a local of the same name.
	switch runtime.GOOS {
	case "windows": // Not found: no comparison.
	}
}
