//go:build (linux && !feature) || (custom && !linux)

package differences

import "runtime"

// Found by neither: with custom set, windows builds this file. Both other tags are numbered and tried.
func partlyRead() {
	use(runtime.GOOS == "windows")
}
