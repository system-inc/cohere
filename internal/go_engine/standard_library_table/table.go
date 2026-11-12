// Package standard_library_table is where the binary carries the Go standard library's analysis entries: a table a
// release writes into table.bin before it compiles, and the release stamp it was written under. Both are empty in
// this repository, and a build with an empty stamp reads no table at all, so a local build, cohere-dev's among them,
// computes the standard library as it always has.
package standard_library_table

import (
	_ "embed"
	"fmt"
	"strconv"
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
// carries one it reads, and whether the line says either way. A binary older than the line says nothing, though a
// release candidate among them carries a table, so its caller refuses it rather than reading it as a local build, and
// so does a line it cannot read: only a `none` line, as VersionLine writes each of its three, reads as no table, and a
// line in any other shape, a `release` line without its size among them, says nothing, so a change to the line's
// shape fails closed rather than reading every release as a local build (#ccqee0e).
func ReadVersionLine(output string) (string, int, bool, bool) {
	for line := range strings.SplitSeq(output, "\n") {
		described, found := strings.CutPrefix(strings.TrimSpace(line), VersionPrefix)
		if !found {
			continue
		}
		// A release's stamp holds spaces, `0.2.0 <commit> go1.27.0`, so the line is cut at its last comma, after which
		// only the size is written (VersionLine).
		if strings.HasPrefix(described, "none") {
			return "", 0, false, true
		}
		released, isRelease := strings.CutPrefix(described, "release ")
		cut := strings.LastIndex(released, ", ")
		if !isRelease || cut < 0 {
			return "", 0, false, false
		}
		stamp := released[:cut]
		sized, isSized := strings.CutSuffix(released[cut+len(", "):], " bytes")
		size, sizeError := strconv.Atoi(sized)
		if !isSized || sizeError != nil || stamp == "" || size <= 0 {
			return "", 0, false, false
		}
		return stamp, size, true, true
	}
	return "", 0, false, false
}
