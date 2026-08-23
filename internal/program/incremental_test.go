package program_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/incremental"
	"github.com/system-inc/verify/internal/program"
)

// incrementalConfig turns on the two options the incremental machinery needs. Everything else
// matches minimalConfig, so a difference in findings is a difference the machinery caused
// rather than a difference in how the program was configured.
const incrementalConfig = `{
    "compilerOptions": {
        "target": "ES2022",
        "module": "esnext",
        "moduleResolution": "bundler",
        "strict": true,
        "noEmit": true,
        "incremental": true,
        "tsBuildInfoFile": "./tsconfig.tsbuildinfo"
    },
    "include": ["./*.ts"]
}`

// messagesOf renders diagnostics into a comparable form. Comparing pointers would compare
// identity rather than content, and two runs never share pointers.
//
// The identity is file, code, and position rather than message text: MessageText is empty for
// a compiler diagnostic (the text is assembled at render time from a key and args), so keying
// on it would make every diagnostic compare equal and every comparison here vacuous.
func messagesOf(diagnostics []*ast.Diagnostic) []string {
	rendered := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		name := ""
		if file := diagnostic.File(); file != nil {
			name = filepath.Base(file.FileName())
		}
		rendered = append(rendered, fmt.Sprintf("%s:%d:TS%d", name, diagnostic.Pos(), diagnostic.Code()))
	}
	return rendered
}

// TestIncrementalProgramSkipsUnchangedFiles is the baseline the incremental path must match,
// and it is deliberately NOT yet a test of that path.
//
// Read this before trusting it: verify does not currently write a build info. Measured, not
// assumed — after a full program.Build the project directory holds only the source and the
// tsconfig. compiler.NewProgram does not emit one; producing a build info is incremental.
// NewProgram plus an emit step, which is why tsgo writes one and verify does not. So leg one's
// assertion that ReadBuildInfoProgram returns nil is true on EVERY run here, not only the
// first, and no leg below is served from a cache.
//
// What this test therefore proves today: the plant-and-heal harness works, and these are the
// findings the incremental path has to reproduce exactly once it is wired. That makes it the
// control for the real test rather than the real test, and calling it the real test would be
// the precise shape of failure this domain exists to prevent.
//
// The dangerous case, once the wiring lands, is warm-after-a-change: a stale snapshot that
// silently reports yesterday's findings would be the fastest run we ever measured and would
// look clean. The three legs below are shaped for that: cold is clean, a planted error is
// found, removing it clears. A cache that never invalidates fails leg three; a cache that
// reports nothing regardless fails leg two.
func TestIncrementalProgramSkipsUnchangedFiles(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": incrementalConfig,
		"clean.ts":      "export const value: number = 1;\n",
	})

	build := func() *program.Graph {
		t.Helper()
		graph, err := program.Build(program.Options{
			ConfigFileName:   "tsconfig.json",
			CurrentDirectory: directory,
		})
		if err != nil {
			t.Fatalf("building: %v", err)
		}
		return graph
	}

	// The host has to be the one the program was built with. The build info's paths resolve
	// relative to it, so a fresh host would resolve them elsewhere and report everything as
	// changed, which looks like a cold cache rather than like a bug.
	readBuildInfo := func(graph *program.Graph) *incremental.Program {
		if graph.CompilerHost == nil {
			t.Fatal("Graph.CompilerHost is nil; the incremental path cannot be reached without it")
		}
		reader := incremental.NewBuildInfoReader(graph.CompilerHost)
		return incremental.ReadBuildInfoProgram(graph.Config, reader, graph.CompilerHost)
	}

	// Leg one: cold. No build info exists, so ReadBuildInfoProgram must return nil, and that
	// nil means rebuild-everything rather than an error.
	//
	// This currently passes for a second reason worth stating: verify never writes a build
	// info, so nil is what this returns on every run. When the wiring lands and verify starts
	// writing one, this assertion becomes a real cold-start check and the legs below start
	// exercising the cache. Until then it is documenting the gap.
	cold := build()
	if previous := readBuildInfo(cold); previous != nil {
		t.Fatal("a build info was found before any run wrote one; the temporary project is dirty " +
			"and every later leg would be measured against the wrong state")
	}
	coldFindings := messagesOf(cold.AllDiagnostics(context.Background()))

	// The clean fixture must produce nothing, or the later legs cannot tell a real finding from
	// pre-existing noise.
	if len(coldFindings) != 0 {
		t.Fatalf("the clean fixture produced %d findings, so this test cannot distinguish a "+
			"planted error from existing ones: %v", len(coldFindings), coldFindings)
	}

	// Leg two: plant an error and confirm a fresh build finds it. This proves the fixture and
	// the harness can produce a finding at all, which is what makes leg three's result mean
	// something. Without it, a silent leg three is indistinguishable from a broken detector.
	plantedPath := filepath.Join(directory, "planted.ts")
	if err := os.WriteFile(plantedPath, []byte("export const broken: number = \"a string\";\n"), 0o644); err != nil {
		t.Fatalf("planting: %v", err)
	}

	planted := build()
	plantedFindings := messagesOf(planted.AllDiagnostics(context.Background()))
	if len(plantedFindings) == 0 {
		t.Fatal("a deliberate type error produced no findings; this harness cannot detect a " +
			"regression and a clean result from it would be vacuous")
	}
	t.Logf("control: planted error found, %d finding(s): %v", len(plantedFindings), plantedFindings)

	// Leg three: remove the error and confirm the findings clear. A cache that never
	// invalidates would keep reporting the planted error here, and a cache that reports
	// nothing regardless would have failed leg two.
	if err := os.Remove(plantedPath); err != nil {
		t.Fatalf("removing the planted file: %v", err)
	}

	healed := build()
	healedFindings := messagesOf(healed.AllDiagnostics(context.Background()))
	if len(healedFindings) != 0 {
		t.Fatalf("after removing the planted error the findings did not clear, which is a stale "+
			"result rather than a clean tree: %v", healedFindings)
	}
}

// TestBuildInfoReaderReturnsNilBeforeAnyBuild pins the contract a caller is most likely to get
// wrong: nil is "no previous state", not "an error occurred". A caller that treats it as a
// failure reports an error on every first run.
func TestBuildInfoReaderReturnsNilBeforeAnyBuild(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": incrementalConfig,
		"main.ts":       "export const value: number = 1;\n",
	})

	graph, err := program.Build(program.Options{
		ConfigFileName:   "tsconfig.json",
		CurrentDirectory: directory,
	})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if graph.CompilerHost == nil {
		t.Fatal("Graph.CompilerHost is nil; the incremental path cannot be reached without it")
	}

	reader := incremental.NewBuildInfoReader(graph.CompilerHost)
	if previous := incremental.ReadBuildInfoProgram(graph.Config, reader, graph.CompilerHost); previous != nil {
		t.Fatal("ReadBuildInfoProgram returned a program with no build info on disk")
	}
}
