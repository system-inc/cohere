package parity

import "runtime"

// The file's name constrains it to amd64.
func architectures() {
	use(runtime.GOARCH == "arm64")   // Found.
	use(runtime.GOARCH == "amd64")   // Not found.
	use(runtime.GOARCH != "386")     // Found.
	use(runtime.GOARCH == "sparc")   // Not found: no architecture the knowledge holds.
	use(runtime.GOOS == "windows")   // Not found: amd64 leaves the operating system open.
	use(runtime.GOARCH == "wasm")    // Found.
	use(runtime.GOARCH == "loong64") // Found.
}
