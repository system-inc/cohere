// Package standard_library_table is where the binary carries the Go standard library's analysis entries: a table a
// release writes into table.bin before it compiles, and the release stamp it was written under. Both are empty in
// this repository, and a build with an empty stamp reads no table at all, so a local build, cohere-dev's among them,
// computes the standard library as it always has.
package standard_library_table

import _ "embed"

// Contents is the table as cache.WriteStandardLibraryTable wrote it, empty unless a release wrote one before
// compiling.
//
//go:embed table.bin
var Contents []byte

// ReleaseStamp names the release whose table Contents is, stamped at link time with `-ldflags=-X`, as the release's
// version is (see internal/release/packaging's version.go). Every standard-library key holds it, so a table written
// for one release is never read by another. Its default, empty, is what a local `go build` produces, and reads
// nothing.
var ReleaseStamp = ""
