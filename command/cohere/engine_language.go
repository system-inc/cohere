package main

import (
	"fmt"

	"github.com/system-inc/cohere/internal/release/packaging"
)

// engineLanguage is the words a contract-3 run's output uses for the engine that wrote it.
//
// The records are the same whichever engine writes them, so one renderer serves every engine that
// speaks the contract. What differs is only how a reader names the engine's world: Swift checks a
// package of Swift files with swiftc, Go a module or workspace of Go files with go. Held in one value
// handed to the run, so no sentence the renderer prints names a language the run is not checking.
type engineLanguage struct {
	// Name is the language as a reader says it, "Swift" in "the Swift engine".
	Name string

	// Container is what the engine describes before any phase runs, "package" for Swift and "module" or
	// "workspace" for Go. It names the accounting line's graph step and the project line.
	Container string

	// FileNoun is what the project line counts, "Swift files".
	FileNoun string

	// CompilerLabel stands where a rule goes on a compiler finding with no group of its own, in the
	// human and JSON views, as tsc's code does on a TypeScript type error.
	CompilerLabel string

	// VersionLines are what `--version` prints about the engine under cohere's own report, from its
	// provenance record and the commit whose sources the front door vouches for, or empty when it vouches
	// for none.
	VersionLines func(provenance *engineProvenanceRecord, engineSourceCommit string) []string

	// ExpectedExclusionReasons are the reasons the engine gives, in its own words, for files nobody expects a
	// run to check. Every other reason a project record gives is the run's gap (engineRun.writeExcluded), so an
	// engine that rewords one makes the gap louder and never hides it. Nil keeps no gap: only Swift's are
	// known, and its gap is said as Swift files, in `gaps.uncheckedSwift` and on the footer.
	ExpectedExclusionReasons map[string]bool

	// CacheOffDetail is what `--no-cache` kept the run from, said after "cache: off, by --no-cache:" in the
	// account. An engine keeps no cache table of the TypeScript pipeline's, so each says what is true of its own.
	CacheOffDetail string

	// NamedScratch is the directory in a project's cache directory where the engine keeps a scratch of its own whose
	// files its inputs record may name, empty when it names none there. Swift's names the package pins an Xcode
	// root's build starts from, which only that scratch holds (RunInputs.pinsRead). See engineInputsUnreplayable.
	NamedScratch string
}

// theEngine is how a sentence names the engine, "the Swift engine", so every error and unfinished
// reason names it the same way.
func (language engineLanguage) theEngine() string {
	return "the " + language.Name + " engine"
}

// swiftLanguage is the words a Swift run has always printed. A change here changes every Swift run's
// output, which the release and the renderer's tests read.
var swiftLanguage = engineLanguage{
	Name:          "Swift",
	Container:     "package",
	FileNoun:      "Swift files",
	CompilerLabel: "swiftc",
	VersionLines:  swiftVersionLines,
	// In swift/Sources/CohereSwift/Project/FileSet.swift's words: what git ignores, what is vendored, and the
	// manifests. A standalone script is checked, alone (#13nbb1s), so it is never excluded. Every other file no
	// target compiles is a gap, a file of declarations only and a script whose imports name two platforms, and so
	// is whatever sentence comes next.
	ExpectedExclusionReasons: map[string]bool{
		"ignored by git":         true,
		"vendored under Vendor/": true,
		"a package manifest":     true,
	},
	// A Swift run's cache is the run cache's table, which replays a verdict with nothing changed (beginSwiftRunCache),
	// beside the engine's own scratch in the package's .cache, so the sentence says the flag reaches both.
	CacheOffDetail: "nothing was read from or written to this package's cache: no recorded run was replayed and " +
		"this one was not recorded, and the engine described and built the package in a scratch of its own and " +
		"removed it",
	// Pipeline.scratchPath in swift/Sources/CohereSwift/Pipeline/Pipeline.swift: `.cache/cohere/swift`.
	NamedScratch: "swift",
}

// swiftVersionLines is the Swift engine's `--version` lines. Only Swift has the source-commit line,
// because only the Swift engine is built by the launcher from a commit's sources and cached by them.
func swiftVersionLines(provenance *engineProvenanceRecord, engineSourceCommit string) []string {
	lines := []string{fmt.Sprintf("swift engine: %s %s (commit %s, %s, swift-syntax %s, swift-format %s)",
		provenance.Engine, provenance.Version, provenance.Commit,
		provenance.Toolchain, provenance.SwiftSyntax, provenance.SwiftFormat)}
	if provenance.SourceTreeModified {
		lines = append(lines, "  the Swift engine was built from a modified tree, so no commit reproduces it")
	}
	if engineSourceCommit != "" {
		lines = append(lines, fmt.Sprintf("  the Swift engine was built from Swift sources identical to commit %s, "+
			"as the launcher's cache key requires", release.ShortCommit(engineSourceCommit)))
	}
	return lines
}

// goLanguage is the words a Go run prints. The container is the caller's, since a Go root is a module
// or a workspace and the project line should say which one it described.
func goLanguage(container string) engineLanguage {
	return engineLanguage{
		Name:          "Go",
		Container:     container,
		FileNoun:      "Go files",
		CompilerLabel: "go",
		VersionLines:  goVersionLines,
		// A Go run's cache is the run cache's table, which replays a verdict with nothing changed (beginGoRunCache).
		// The engine loads the packages through the go command, whose build cache is go's
		// (internal/go_engine/load), so the sentence says which of the two the flag reaches.
		CacheOffDetail: "no recorded run was replayed and this one was not recorded, and the go command's " +
			"build cache, which loading the packages reads and may fill, is go's to keep",
	}
}

// goVersionLines is the Go engine's `--version` lines. The Go engine has no source-commit line: nothing
// builds it from a commit's sources on the front door's behalf, so the front door has no commit to vouch
// for, and its own provenance record names what it was built from.
//
// The line names the contract the engine's record says it speaks. The `swift contract` line above it is
// cohere's own and stays in every mode, Go's included, because the release reads it from a `--version` run
// wherever it stands, and cohere's own repository is a Go root.
func goVersionLines(provenance *engineProvenanceRecord, _ string) []string {
	contract := "none"
	if provenance.Contract != nil {
		contract = fmt.Sprint(*provenance.Contract)
	}
	lines := []string{fmt.Sprintf("go engine: %s %s (commit %s, %s, contract %s)",
		provenance.Engine, provenance.Version, provenance.Commit, provenance.Toolchain, contract)}
	if provenance.SourceTreeModified {
		lines = append(lines, "  the Go engine was built from a modified tree, so no commit reproduces it")
	}
	return lines
}
