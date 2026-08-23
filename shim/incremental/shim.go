// Package incremental re-exports typescript-go's incremental build machinery.
//
// Hand-written rather than generated, for the same reason the parser and format shims are:
// tools/gen_shims covers the packages a headless linter needed, and incremental was not among
// them because a linter that always checks everything has no use for change detection.
//
// Note this is internal/execute/incremental, which lives inside the CLI driver rather than
// beside the compiler. That placement is the reason it was easy to miss: a search for
// internal/incremental finds nothing.
//
// What this buys, measured on the ahra tree rather than assumed: tsgo --noEmit --incremental
// costs 2.11s cold and 0.85s warm, so the mechanism is worth ~1.26s to the compiler that
// ships it. What it does NOT buy is a warm program. ReadBuildInfoProgram returns a *Program
// whose `program` field is nil (incremental.go:44-62), GetProgram panics on that path
// (program.go:110), and upstream calls compiler.NewProgram unconditionally right after
// reading the build info (tsc.go:304-310). The build info is a change-detection input, not a
// graph you load instead of building.
//
// One thing that will mislead whoever wires this up. The build info our tree produces carries
// NO semanticDiagnosticsPerFile, and neither does one written by a fresh --noEmit
// --incremental run: verified on two artifacts, both with fileNames 9,982 matching program
// size. The field exists in the format (buildInfo.go:483) and setSemanticDiagnostics runs
// unconditionally beside setReferencedMap, which does populate. So the 1.26s above is earned
// by fileInfos signatures plus referencedMap, and code that expects to replay cached
// diagnostics out of the build info will read an empty list and decide it is cold on every
// single run. That failure is silent and it looks exactly like a working cache.
package incremental

import (
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/execute/incremental"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
)

// Program is the incremental wrapper around a compiler program. Its `program` field is nil
// when it came from ReadBuildInfoProgram, so treat it as change detection rather than a graph.
type Program = incremental.Program

// BuildInfo is the parsed .tsbuildinfo. Read IsValidVersion and IsIncremental before trusting
// it: ReadBuildInfoProgram returns nil for either failure, and a nil here means "rebuild
// everything" rather than "something went wrong".
type BuildInfo = incremental.BuildInfo

// BuildInfoReader reads a .tsbuildinfo from disk through a compiler host.
type BuildInfoReader = incremental.BuildInfoReader

// Host is the incremental machinery's view of the filesystem.
type Host = incremental.Host

// NewBuildInfoReader builds a reader over a compiler host.
func NewBuildInfoReader(host compiler.CompilerHost) BuildInfoReader {
	return incremental.NewBuildInfoReader(host)
}

// ReadBuildInfoProgram loads the previous run's change-detection state. Returns nil when no
// build info exists, its version does not match, or it was not written incrementally.
func ReadBuildInfoProgram(
	config *tsoptions.ParsedCommandLine,
	reader BuildInfoReader,
	host compiler.CompilerHost,
) *Program {
	return incremental.ReadBuildInfoProgram(config, reader, host)
}

// CreateHost adapts a compiler host for the incremental machinery.
func CreateHost(compilerHost compiler.CompilerHost) Host {
	return incremental.CreateHost(compilerHost)
}
