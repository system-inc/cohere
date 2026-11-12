package release

import "strings"

// Provenance is everything a shipped binary knows about where it came from.
type Provenance struct {
	// Version is the published version, or "dev" for a local build.
	Version string

	// SelfCommit is the commit of this repository the binary was built from, or "" when the build
	// carried no version-control stamp.
	//
	// Separate from CompilerCommit, which names the vendored compiler and moves independently. A
	// local build reports its version as "dev", which says nothing about which rules it holds, and
	// every measurement taken with it is only reproducible if the binary can name its own vintage.
	// Two numbers taken from binaries at different commits look exactly like a contradiction, and
	// the cheapest way to tell those apart is for each number to arrive with its commit attached.
	SelfCommit string

	// CompilerCommit is the pinned commit of the vendored compiler.
	CompilerCommit string

	// CompilerUpstream is the repository CompilerCommit belongs to.
	//
	// A commit hash means nothing without the repository it lives in, and this one has moved: a
	// pin recorded before the `microsoft/TypeScript` migration and one recorded after are both
	// forty hex characters and resolve in different places.
	CompilerUpstream string

	// GoToolchain is the Go version that compiled this binary.
	GoToolchain string

	// SourceTreeModified reports whether the tree this binary was built from had uncommitted
	// changes.
	//
	// Read from Go's own `vcs.modified` stamp rather than computed, because the linker observes the
	// tree at build time and nothing later can. A `--dev` build is the exception: the launcher reads the
	// tree once and stamps that answer, because the same answer names the cached binary (#50wtb2s).
	//
	// It answers the question a version number cannot: a version identifies what was released, and
	// this says whether that release is reproducible from a fresh clone at all.
	//
	// It matters here more than in most projects because the source lives in a worktree several
	// members edit at once. A release staged while someone is mid-flight embeds their uncommitted
	// work, ships, installs, and reports a version that no commit produces. Measured: eight modified
	// files in the tree at the time this was written.
	SourceTreeModified bool

	// FormatterIdentity is a hash of the formatter's inputs: its printers and everything they import,
	// the pass loop that decides what formatted means, and the toolchain. The format record is keyed
	// by it rather than by the binary, so a cohere commit that leaves the formatter alone leaves the
	// record valid. Empty when the build was not stamped, and then the record keys by the binary.
	FormatterIdentity string

	// Platform is the operating system and architecture this binary was built for, as "os/arch".
	//
	// It is read from the runtime rather than stamped, because the runtime cannot be wrong about it
	// and a stamp can. A binary that reports the platform it was supposed to be built for, rather
	// than the one it was, would turn the most common packaging mistake into an invisible one.
	Platform string
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
		"cohere " + provenance.Version,
		"  platform:       " + provenance.Platform,
		"  go:             " + provenance.GoToolchain,
		"  compiler:       " + provenance.describeCompiler(),
	}
	// Printed whenever the build carried a stamp, including a release, because a published version
	// still does not say which commit produced it and a bug report quoting both is easier to chase.
	if provenance.SelfCommit != "" {
		lines = append(lines, "  commit:         "+ShortCommit(provenance.SelfCommit))
	}
	// Printed only when true, because "built from a clean tree" is the ordinary case and a line
	// asserting it on every release would be noise that hides the one time it matters.
	if provenance.SourceTreeModified {
		lines = append(
			lines,
			"  source:         built from a tree with uncommitted changes, so no commit reproduces this binary",
		)
	}
	if provenance.IsDevelopment() {
		// A local build of a named commit with nothing uncommitted in it is reproducible from that
		// commit, which is what the launcher builds every gate from. Saying "whatever was on disk"
		// there would be a disclaimer that is no longer true, and a reader who learns the disclaimer
		// is decorative stops reading the one that is not: a `--dev` build, or any build from a
		// dirty tree, still carries it.
		if provenance.SelfCommit != "" && !provenance.SourceTreeModified {
			lines = append(
				lines,
				"  note:           a local build of the commit above, with nothing uncommitted in it, so that commit reproduces it",
			)
		} else {
			lines = append(
				lines,
				"  note:           a local build, so the rules are whatever was on disk when it was compiled",
			)
		}
	}
	return strings.Join(lines, "\n")
}

// describeCompiler renders the vendored compiler as a repository and a commit in it.
//
// The repository is printed alongside the hash rather than assumed, because the vendored compiler
// has already moved once: `microsoft/typescript-go` was archived and the pin is now against
// `microsoft/TypeScript`. Two pins from either side of that migration are both forty hex characters
// and resolve in different places, so a hash alone is not enough to look one up.
func (provenance Provenance) describeCompiler() string {
	if provenance.CompilerUpstream == "" || provenance.CompilerUpstream == "unknown" {
		return provenance.CompilerCommit
	}
	return provenance.CompilerUpstream + "@" + provenance.CompilerCommit
}
