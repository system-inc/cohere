package program

import (
	"context"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/incremental"
)

// NewIncrementalSession prepares a warm-capable pass. Returns nil when the graph cannot
// support one, which is a reason to run the full pass rather than an error: no compiler host,
// no config, or a tsconfig without `incremental` all mean the caller should use
// AllDiagnostics.
func (graph *Graph) NewIncrementalSession() *IncrementalSession {
	if graph.CompilerHost == nil || graph.Config == nil {
		return nil
	}
	if !graph.Config.CompilerOptions().IsIncremental() {
		return nil
	}

	// The host must be the one this graph's program was built with. Build info paths resolve
	// relative to it, so a fresh host with a different current directory resolves them
	// elsewhere and reports every file as changed. Silently, and in the direction that looks
	// like a cold cache rather than like a bug.
	host := incremental.CreateHost(graph.CompilerHost)

	// A nil previous program is a cold start, not a failure: no build info on disk, a version
	// mismatch, or a non-incremental one all return nil and all mean check everything.
	reader := incremental.NewBuildInfoReader(graph.CompilerHost)
	previous := incremental.ReadBuildInfoProgram(graph.Config, reader, graph.CompilerHost)

	incrementalProgram := incremental.NewProgram(graph.Program, previous, host)
	if incrementalProgram == nil {
		return nil
	}
	return &IncrementalSession{graph: graph, program: incrementalProgram}
}

// IncrementalDiagnostics is the one-shot form: check warm and persist for next time, falling
// back to the full pass when the graph cannot support a session.
//
// Prefer this at a call site that just wants the findings. Use NewIncrementalSession directly
// when the caller needs to decide whether to write, for example when it is about to report a
// failure and would rather not persist state from a run it is discarding.
func (graph *Graph) IncrementalDiagnostics(callerContext context.Context) []*ast.Diagnostic {
	session := graph.NewIncrementalSession()
	if session == nil {
		return graph.AllDiagnostics(callerContext)
	}
	diagnostics := session.Diagnostics(callerContext)
	session.Write(callerContext)
	return diagnostics
}
