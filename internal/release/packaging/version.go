// Package release holds what a shipped cohere knows about itself: which build it is, and where its
// binary comes from on a machine that has no source and no Go toolchain.
//
// The two halves answer the two questions a bug report has to answer. `Provenance` says what was
// running, so a report names a version, a toolchain, and a pinned compiler commit rather than
// "latest". `Resolve` says which file ran, and refuses to guess: a resolver that falls through to
// something plausible is how the gate this tool replaces printed green over zero files for days.
//
// `Build` here stages the published packages, which is the other end of the same path: it is what
// puts a binary where `Resolve` will find it, stamped with what `Provenance` will report.
package release

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// These are stamped at link time by the release pipeline, with `-ldflags=-X`. Their defaults are
// what a local `go build` produces, and they say so plainly rather than reading like a real
// release: a binary that cannot name its own provenance must not be able to look like one that can.
var (
	// version is the published npm version, like "0.3.1".
	version = "dev"

	// compilerCommit is the commit of the vendored compiler this binary statically links. It is
	// most of the code in the binary and it moves independently of this repository, so a bug that
	// reproduces on one pin and not another is only diagnosable if the binary carries it.
	compilerCommit = "unknown"

	// compilerUpstream is the repository that commit belongs to.
	//
	// It is stamped rather than written down because the upstream has already moved once: the
	// vendored compiler was `microsoft/typescript-go` until that repository was archived, and is
	// now `microsoft/TypeScript`. A hardcoded label survives a migration like that while silently
	// becoming false, which is the worst outcome available here: a bug report would name a commit
	// against a repository it does not exist in, and the reader would have no way to tell.
	compilerUpstream = "unknown"

	// goToolchain is the toolchain that compiled this binary, like "go1.27.0". The compiler is an
	// input to the behavior, not just to the build: it is recorded for the same reason the rebuild
	// cache hashes it.
	goToolchain = "unknown"

	// selfCommit is the commit of this repository the binary was built from, stamped by the launcher
	// when it builds from a snapshot of that commit.
	//
	// A snapshot has no `.git`, so Go writes no `vcs.revision` for it, and building it with VCS
	// stamping on would make Go walk up to the enclosing checkout and stamp that tree's state instead,
	// dirty files and all. So the launcher turns stamping off and states the commit itself. It can,
	// because it extracted the snapshot from that commit and nothing else.
	selfCommit = ""

	// formatterIdentity names everything that can change what the formatter prints, stamped by the
	// launcher when it builds (see dispatch.FormatterIdentity). Empty for a build nobody stamped.
	formatterIdentity = ""

	// sourceTreeModified is "true" or "false" when the launcher stamps the state of the tree it built
	// from, as a `--dev` build does. That build turns VCS stamping off and reads the state itself, once,
	// for the binary's cached name and for this stamp, so the binary reports the state its name holds
	// (dispatch.ResolveWorkingTree, #50wtb2s). Empty otherwise, and Go's own `vcs.modified` is read.
	sourceTreeModified = ""
)

// Current returns this binary's provenance.
func Current() Provenance {
	return Provenance{
		Version:            version,
		SelfCommit:         resolveSelfCommit(),
		CompilerCommit:     resolveCompilerCommit(),
		CompilerUpstream:   compilerUpstream,
		GoToolchain:        resolveGoToolchain(),
		SourceTreeModified: resolveSourceTreeModified(),
		FormatterIdentity:  formatterIdentity,
		Platform:           runtime.GOOS + "/" + runtime.GOARCH,
	}
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

// resolveCompilerCommit prefers the stamped commit, falling back to the module's own VCS data.
//
// The stamp is authoritative because the vendored compiler is a git submodule rather than a Go
// module dependency, so the build info records this repository's revision and never the submodule's.
// The fallback exists so that a `go build` with no stamping still says something true about which
// checkout it came from, instead of the flat "unknown" that reads like a packaging failure.
func resolveCompilerCommit() string {
	if compilerCommit != "unknown" && compilerCommit != "" {
		return compilerCommit
	}

	information, available := debug.ReadBuildInfo()
	if !available {
		return "unknown"
	}
	for _, setting := range information.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			// Named for what it actually is. Reporting this repository's commit under the
			// compiler's label would be a true fact wearing a wrong name, which is worse than
			// the honest "unknown" because a reader would act on it.
			return fmt.Sprintf("unknown (built from cohere %s)", ShortCommit(setting.Value))
		}
	}
	return "unknown"
}

// resolveSelfCommit reads this repository's commit from the stamp Go's linker writes.
//
// Go records `vcs.revision` on any build inside a version-controlled tree, so this needs no
// `-ldflags` and works for a local `go build` as well as a release. A launcher build from a committed
// snapshot stamps selfCommit instead, and that wins, because it was stated deliberately by the only
// process that knows which commit the snapshot holds. Empty when neither is present, which is the
// honest answer for a build made outside a repository or with `-buildvcs=false` and no stamp.
func resolveSelfCommit() string {
	if selfCommit != "" {
		return selfCommit
	}
	information, available := debug.ReadBuildInfo()
	if !available {
		return ""
	}
	for _, setting := range information.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

// resolveSourceTreeModified reports whether the build tree carried uncommitted changes.
//
// A stamp from the launcher wins, as resolveSelfCommit's does, for the reason sourceTreeModified gives.
// Otherwise Go's linker writes this stamp itself, so it observes the tree at the moment of the build and
// cannot be wrong about it the way a value computed afterwards could. A binary with no build info
// at all reports false rather than true: absent evidence is not evidence of a dirty tree, and
// claiming otherwise would put a warning on every binary that predates this field.
func resolveSourceTreeModified() bool {
	switch sourceTreeModified {
	case "true":
		return true
	case "false":
		return false
	}
	information, available := debug.ReadBuildInfo()
	if !available {
		return false
	}
	for _, setting := range information.Settings {
		if setting.Key == "vcs.modified" {
			return setting.Value == "true"
		}
	}
	return false
}

// ShortCommit trims a full hash to the length people actually read, the one home for that length.
//
// Exported because the launcher checks a freshly built binary by finding its own commit, shortened
// this way, on the `commit:` line this package prints. Two copies of the length would have to agree
// by coincidence, and the day they did not, every committed build would be refused.
func ShortCommit(commit string) string {
	const shortLength = 12
	if len(commit) <= shortLength {
		return commit
	}
	return commit[:shortLength]
}
