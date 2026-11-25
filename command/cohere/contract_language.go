package main

import (
	"fmt"
	"strings"

	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// contractLanguage is the words a contract-3 run's output uses for the child that wrote it.
//
// The records are the same whichever child writes them, so one renderer serves every language that
// speaks the contract. What differs is only how a reader names the child's world: Swift checks a
// package of Swift files with swiftc, Go a module or workspace of Go files with go. Held in one value
// handed to the run, so no sentence the renderer prints names a language the run is not checking.
type contractLanguage struct {
	// Name is the language as a reader says it, "Swift" in "cohere-swift".
	Name string

	// Container is what the child describes before any phase runs, "package" for Swift and "module" or
	// "workspace" for Go. It names the accounting line's graph step and the project line.
	Container string

	// FileNoun is what the project line counts, "Swift files".
	FileNoun string

	// CompilerLabel stands where a rule goes on a compiler finding with no group of its own, in the
	// human and JSON views, as tsc's code does on a TypeScript type error.
	CompilerLabel string

	// VersionLines are what `--version` prints about the child under cohere's own report, from its
	// provenance record and the commit whose sources the front door vouches for, or empty when it vouches
	// for none.
	VersionLines func(provenance *contractProvenanceRecord, swiftSourceCommit string) []string

	// ExpectedExclusionReasons are the reasons the child gives, in its own words, for files nobody expects a
	// run to check. Every other reason a project record gives is the run's gap (contractRun.writeExcluded), so an
	// child that rewords one makes the gap louder and never hides it. The gap is said in the language's name, in
	// `gaps.unchecked` and on the footer, "⚠ 3 Swift files not checked". Nil keeps no gap.
	ExpectedExclusionReasons map[string]bool

	// ExpectedExclusionReasonPrefixes begin the reasons nobody expects a run to check that name something of their
	// own, as Go's "checked by the windows/arm64 build" names the build that checked the file. A reason is matched
	// whole or by one of these, and every other is the run's gap.
	ExpectedExclusionReasonPrefixes []string

	// NotSourceReasons are the reasons the child gives for a file that was never one of the project's sources to
	// check, as Swift's package manifest. A run that checked nothing counts every other excluded file in its reasons
	// (contractRun.countExcludedWhenNothingChecked), so a package whose only file is its manifest is a project with
	// none of its language's files, which passes with the warning, and an all-ignored package's sentence does not count
	// the manifest beside its sources.
	NotSourceReasons map[string]bool

	// CacheOffDetail is what `--no-cache` kept the run from, said after "cache: off, by --no-cache:" in the
	// account. A child keeps no cache table of the TypeScript pipeline's, so each says what is true of its own.
	CacheOffDetail string

	// NamedScratch is the directory in a project's cache directory where the child keeps a scratch of its own whose
	// files its inputs record may name, empty when it names none there. Swift's names the package pins an Xcode
	// root's build starts from, which only that scratch holds (RunInputs.pinsRead). See contractInputsUnreplayable.
	NamedScratch string
}

// programName is how a sentence names the child, "cohere-swift", so every error and unfinished
// reason names it the same way.
func (language contractLanguage) programName() string {
	return "cohere-" + strings.ToLower(language.Name)
}

// expectsExcluded reports whether a reason the project record gives is one nobody expects the run to check: one of
// ExpectedExclusionReasons, or one beginning with one of ExpectedExclusionReasonPrefixes.
func (language contractLanguage) expectsExcluded(reason string) bool {
	return language.ExpectedExclusionReasons[reason] || language.checkedElsewhere(reason)
}

// checkedElsewhere reports whether a reason begins with one of ExpectedExclusionReasonPrefixes, which name what
// checked the file, as "checked by the windows/arm64 build" does: the file was left out of this build's check and
// checked by another, so the account says so rather than calling it not checked.
func (language contractLanguage) checkedElsewhere(reason string) bool {
	for _, prefix := range language.ExpectedExclusionReasonPrefixes {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}

// swiftLanguage is the words a Swift run has always printed. A change here changes every Swift run's
// output, which the release and the renderer's tests read.
var swiftLanguage = contractLanguage{
	Name:          "Swift",
	Container:     "package",
	FileNoun:      "Swift files",
	CompilerLabel: "swiftc",
	VersionLines:  swiftVersionLines,
	// In swift_language/Sources/CohereSwift/Project/FileSet.swift's words: what git ignores, what is vendored, and the
	// manifests. A standalone script is checked, alone (#13nbb1s), so it is never excluded. Every other file no
	// target compiles is a gap, a file of declarations only and a script whose imports name two platforms, and so
	// is whatever sentence comes next.
	ExpectedExclusionReasons: map[string]bool{
		"ignored by git":         true,
		"vendored under Vendor/": true,
		"a package manifest":     true,
	},
	NotSourceReasons: map[string]bool{"a package manifest": true},
	// A Swift run's cache is the run cache's table, which replays a verdict with nothing changed (beginSwiftRunCache),
	// beside the child's own scratch in the package's .cache, so the sentence says the flag reaches both.
	CacheOffDetail: "nothing was read from or written to this package's cache: no recorded run was replayed and " +
		"this one was not recorded, and the child described and built the package in a scratch of its own and " +
		"removed it",
	// Pipeline.scratchPath in swift_language/Sources/CohereSwift/Pipeline/Pipeline.swift: `.cache/cohere/swift`.
	NamedScratch: "swift",
}

// swiftVersionLines is cohere-swift's `--version` lines. Only Swift has the source-commit line,
// because only cohere-swift is built by the launcher from a commit's sources and cached by them.
func swiftVersionLines(provenance *contractProvenanceRecord, swiftSourceCommit string) []string {
	lines := []string{fmt.Sprintf("swift: %s %s (commit %s, %s, swift-syntax %s, swift-format %s)",
		provenance.Program, provenance.Version, provenance.Commit,
		provenance.Toolchain, provenance.SwiftSyntax, provenance.SwiftFormat)}
	if provenance.SourceTreeModified {
		lines = append(
			lines,
			"  cohere-swift was built from uncommitted changes to its own sources under swift_language/, "+
				"so no commit reproduces it",
		)
	}
	if swiftSourceCommit != "" {
		lines = append(lines, fmt.Sprintf("  cohere-swift was built from Swift sources identical to commit %s, "+
			"as the launcher's cache key requires", release.ShortCommit(swiftSourceCommit)))
	}
	return lines
}

// goLanguage is the words a Go run prints. The container is the caller's, since a Go root is a module
// or a workspace and the project line should say which one it described.
func goLanguage(container string) contractLanguage {
	return contractLanguage{
		Name:          "Go",
		Container:     container,
		FileNoun:      "Go files",
		CompilerLabel: "go",
		VersionLines:  goVersionLines,
		// The project record's reasons, from the census and the build constraints (internal/go_language's
		// describeProject): what git ignores, and what lies in a repository of its own below a module, a submodule's
		// checkout or a vendored clone that no ignore file of ours governs (#hd5bsqh), are files nobody expects
		// checked. What go never reads (testdata, a directory starting with `.` or `_`, a nested module, below
		// vendor/) the census never lists, so it has no reason here. Every other reason is a gap, and so is whatever
		// sentence comes next: a file only another platform builds, `excluded by build constraints on darwin/arm64`,
		// and one that imports "C" while cgo is off.
		// Those are files of the project that someone builds, and a run under `--host-build-only` checks the host's
		// build alone, so a run on a Mac never loaded the `_windows.go` file it would otherwise read green over
		// (#72n9mq0).
		ExpectedExclusionReasons: map[string]bool{
			project.IgnoredByGit:         true,
			project.InRepositoryOfItsOwn: true,
		},
		// A run checks every build by default (#qqyq61s): a file only another build compiles is checked by that build,
		// and the record says which, "checked by the windows/arm64 build" (project.CheckedByBuild), so the reason is
		// matched by how it begins. One no build compiles keeps its constraint's reason and stays a gap.
		ExpectedExclusionReasonPrefixes: []string{project.CheckedByBuildPrefix},
		// A Go run's cache is the run cache's table, which replays a verdict with nothing changed (beginGoRunCache).
		// The child loads the packages through the go command, whose build cache is go's
		// (internal/go_language/load), so the sentence says which of the two the flag reaches.
		CacheOffDetail: "no recorded run was replayed and this one was not recorded, and the go command's " +
			"build cache, which loading the packages reads and may fill, is go's to keep",
	}
}

// goVersionLines is cohere-go's `--version` lines. cohere-go has no source-commit line: nothing
// builds it from a commit's sources on the front door's behalf, so the front door has no commit to vouch
// for, and its own provenance record names what it was built from.
//
// The line names the contract the child's record says it speaks. The `swift contract` line above it is
// cohere's own and stays in every mode, Go's included, because the release reads it from a `--version` run
// wherever it stands, and cohere's own repository is a Go root.
func goVersionLines(provenance *contractProvenanceRecord, _ string) []string {
	contract := "none"
	if provenance.Contract != nil {
		contract = fmt.Sprint(*provenance.Contract)
	}
	lines := []string{fmt.Sprintf("go: %s %s (commit %s, %s, contract %s)",
		provenance.Program, provenance.Version, provenance.Commit, provenance.Toolchain, contract)}
	if provenance.SourceTreeModified {
		lines = append(lines, "  cohere-go was built from a modified tree, so no commit reproduces it")
	}
	return lines
}
