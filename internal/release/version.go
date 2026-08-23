// Package release holds what a shipped verify knows about itself: which build it is, and where its
// binary comes from on a machine that has no source and no Go toolchain.
//
// The two halves answer the two questions a bug report has to answer. `Provenance` says what was
// running, so a report names a version, a toolchain, and a pinned compiler commit rather than
// "latest". `Resolve` says which file ran, and refuses to guess — a resolver that falls through to
// something plausible is how the gate this tool replaces printed green over zero files for days.
//
// `Build` here stages the published packages, which is the other end of the same path: it is what
// puts a binary where `Resolve` will find it, stamped with what `Provenance` will report.
package release

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// These are stamped at link time by the release pipeline, with `-ldflags=-X`. Their defaults are
// what a local `go build` produces, and they say so plainly rather than reading like a real
// release: a binary that cannot name its own provenance must not be able to look like one that can.
var (
	// version is the published npm version, like "0.3.1".
	version = "dev"

	// typeScriptGoCommit is the commit of the vendored compiler this binary statically links. It is
	// most of the code in the binary and it moves independently of this repository, so a bug that
	// reproduces on one pin and not another is only diagnosable if the binary carries it.
	typeScriptGoCommit = "unknown"

	// goToolchain is the toolchain that compiled this binary, like "go1.27.0". The compiler is an
	// input to the behavior, not just to the build: it is recorded for the same reason the rebuild
	// cache hashes it.
	goToolchain = "unknown"
)

// Provenance is everything a shipped binary knows about where it came from.
type Provenance struct {
	// Version is the published version, or "dev" for a local build.
	Version string

	// TypeScriptGoCommit is the pinned commit of the vendored compiler.
	TypeScriptGoCommit string

	// GoToolchain is the Go version that compiled this binary.
	GoToolchain string

	// Platform is the operating system and architecture this binary was built for, as "os/arch".
	//
	// It is read from the runtime rather than stamped, because the runtime cannot be wrong about it
	// and a stamp can. A binary that reports the platform it was supposed to be built for, rather
	// than the one it was, would turn the most common packaging mistake into an invisible one.
	Platform string
}

// Current returns this binary's provenance.
func Current() Provenance {
	return Provenance{
		Version:            version,
		TypeScriptGoCommit: resolveTypeScriptGoCommit(),
		GoToolchain:        resolveGoToolchain(),
		Platform:           runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// IsDevelopment reports whether this binary came from a local build rather than a release.
//
// The distinction matters for a bug report: a development binary's rules are whatever was on disk
// at the time, so its version number identifies nothing and should not be trusted as if it did.
func (provenance Provenance) IsDevelopment() bool {
	return provenance.Version == "dev"
}

// String renders the provenance as one line per fact.
//
// Every fact prints on its own line, including the ones that are unknown, because a missing line
// and an unknown value read identically in a pasted bug report while meaning very different things:
// one is an old binary that predates the stamping, the other is a build that lost its stamp.
func (provenance Provenance) String() string {
	lines := []string{
		"verify " + provenance.Version,
		"  platform:       " + provenance.Platform,
		"  go:             " + provenance.GoToolchain,
		"  typescript-go:  " + provenance.TypeScriptGoCommit,
	}
	if provenance.IsDevelopment() {
		lines = append(lines, "  note:           a local build, so the rules are whatever was on disk when it was compiled")
	}
	return strings.Join(lines, "\n")
}

// resolveGoToolchain prefers the toolchain recorded in the binary over the stamped value.
//
// `runtime.Version()` is written by the linker that actually built this file, so it cannot disagree
// with reality. The stamp is a fallback for the same reason the platform is not stamped at all: a
// value a pipeline computes can be wrong, and a value the toolchain writes about itself cannot.
func resolveGoToolchain() string {
	if toolchain := runtime.Version(); toolchain != "" {
		return toolchain
	}
	return goToolchain
}

// resolveTypeScriptGoCommit prefers the stamped commit, falling back to the module's own VCS data.
//
// The stamp is authoritative because the vendored compiler is a git submodule rather than a Go
// module dependency, so the build info records this repository's revision and never the submodule's.
// The fallback exists so that a `go build` with no stamping still says something true about which
// checkout it came from, instead of the flat "unknown" that reads like a packaging failure.
func resolveTypeScriptGoCommit() string {
	if typeScriptGoCommit != "unknown" && typeScriptGoCommit != "" {
		return typeScriptGoCommit
	}

	information, available := debug.ReadBuildInfo()
	if !available {
		return "unknown"
	}
	for _, setting := range information.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			// Named for what it actually is. Reporting this repository's commit under the
			// typescript-go label would be a true fact wearing a wrong name, which is worse than
			// the honest "unknown" because a reader would act on it.
			return fmt.Sprintf("unknown (built from verify %s)", shortCommit(setting.Value))
		}
	}
	return "unknown"
}

// shortCommit trims a full hash to the length people actually read.
func shortCommit(commit string) string {
	const shortLength = 12
	if len(commit) <= shortLength {
		return commit
	}
	return commit[:shortLength]
}
