//go:build amd64 || arm64

package constraints

// A line after the package clause is never read, though it would parse to nothing and wipe the header's.
//
//go:buildx windows
func taggedAfter() {}
