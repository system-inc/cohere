// Package standard_library_table is where the binary carries the Go standard library's analysis entries: a table a
// release writes into table.bin before it compiles, and the release stamp it was written under. Both are empty in
// this repository, and a build with an empty stamp reads no table at all, so a local build, cohere-dev's among them,
// computes the standard library as it always has.
package standard_library_table

import (
	_ "embed"
	"fmt"
	"strings"
)

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

// VersionPrefix begins the `--version` line that says whether a binary carries a table it reads (VersionLine).
const VersionPrefix = "go standard library table: "

// VersionLine is the `--version` line for a binary stamped with stamp and carrying a table of size bytes: whether its
// cold runs read the standard library's entries or compute them, which the benchmark's record keeps, since a
// release-shaped binary and a local build measure different work and are never compared (#ccqee0e). The engine reads
// a table only when both are present (go_engine's lintCache), so a binary with one and not the other carries none.
func VersionLine(stamp string, size int) string {
	switch {
	case stamp != "" && size > 0:
		return fmt.Sprintf("%srelease %s, %d bytes", VersionPrefix, stamp, size)
	case stamp != "":
		return VersionPrefix + "none, though stamped release " + stamp + ", so a run computes it"
	case size > 0:
		return fmt.Sprintf("%snone read: %d bytes carried with no release stamp, so a run computes it",
			VersionPrefix, size)
	}
	return VersionPrefix + "none, a local build, so a run computes it"
}

// ReadVersionLine is the release stamp and table size a `--version` output's line says the binary carries, whether it
// carries one it reads, and whether the output has the line at all. A binary older than the line says nothing either
// way, though a release candidate among them carries a table, so its caller refuses it rather than reading it as a
// local build.
func ReadVersionLine(output string) (string, int, bool, bool) {
	for line := range strings.SplitSeq(output, "\n") {
		described, found := strings.CutPrefix(strings.TrimSpace(line), VersionPrefix)
		if !found {
			continue
		}
		var stamp string
		var size int
		if _, scanError := fmt.Sscanf(described, "release %s %d bytes", &stamp, &size); scanError != nil {
			return "", 0, false, true
		}
		stamp = strings.TrimSuffix(stamp, ",")
		return stamp, size, stamp != "" && size > 0, true
	}
	return "", 0, false, false
}
