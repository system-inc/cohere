package program_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/rule"
)

// writeProject lays a tiny TypeScript project on disk and returns its directory.
//
// Fixtures live on a real filesystem rather than in an in-memory one because that is the only way
// this test exercises the path production uses: bundled libs overlaid on osvfs, config resolution
// through `extends`, and module resolution across files. A memory FS would prove a different code
// path works.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()

	directory := t.TempDir()
	for name, contents := range files {
		full := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("writing %s: %v", full, err)
		}
	}
	return directory
}

const minimalConfig = `{
    "compilerOptions": {
        "target": "ES2022",
        "module": "esnext",
        "moduleResolution": "bundler",
        "strict": true,
        "noEmit": true
    },
    "include": ["./*.ts"]
}`

// TestBuildProducesAProgramAndAChecker is the claim the whole tool rests on: a tsconfig on disk
// becomes a program in this process, and the checker is a function call away.
func TestBuildProducesAProgramAndAChecker(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"main.ts":       "export const greeting: string = 'hello';\n",
	})

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	// The program carries far more than our one file: every lib.*.d.ts the target pulls in. That is
	// the population the checker needs, and its size is why building it once and reusing it is the
	// point of this package.
	if len(graph.SourceFiles()) < 2 {
		t.Fatalf("expected the program to hold lib declarations plus our file, got %d", len(graph.SourceFiles()))
	}

	projectFiles := graph.ProjectFiles()
	if len(projectFiles) != 1 {
		t.Fatalf("expected exactly the one file the config named, got %d", len(projectFiles))
	}

	fileChecker, release := graph.CheckerForFile(context.Background(), projectFiles[0])
	defer release()
	if fileChecker == nil {
		t.Fatal("no checker for a file the program contains")
	}
}

// TestProjectFilesExcludesDeclarations proves the linted set is the config's own root set rather
// than the program's whole population.
//
// This matters more than it looks. The program's file paths are canonicalized — lowercased on a
// case-insensitive filesystem — while the config's names keep their original casing. Comparing the
// two without canonicalizing matches nothing, and "nothing" is indistinguishable from a clean tree.
// That is the exact shape of failure this project exists to refuse, so it is asserted rather than
// assumed.
func TestProjectFilesExcludesDeclarations(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"Alpha.ts":      "export const alpha = 1;\n",
		"Beta.ts":       "export const beta = 2;\n",
	})

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	projectFiles := graph.ProjectFiles()
	if len(projectFiles) != 2 {
		t.Fatalf("expected the 2 files the config named, got %d", len(projectFiles))
	}
	if len(projectFiles) >= len(graph.SourceFiles()) {
		t.Fatalf("project files (%d) should be a strict subset of the program (%d)",
			len(projectFiles), len(graph.SourceFiles()))
	}
}

// TestWalkDispatchesRulesWithALiveChecker is the load-bearing assertion of this package.
//
// The rule here does not inspect syntax. It asks the checker for the type of an initializer and
// reports what it got back, which no amount of parsing can answer. A walk that dispatched rules but
// handed them a nil or foreign checker would pass a syntax-only test and fail this one.
func TestWalkDispatchesRulesWithALiveChecker(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"main.ts":       "const counted: number = 41 + 1;\nconst named = 'ahra';\n",
	})

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	typeNames := []string{}
	typeReporter := rule.Rule{
		Name: "test-report-initializer-type",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			if ctx.TypeChecker == nil {
				t.Error("a rule was dispatched without a checker")
				return nil
			}
			return rule.Listeners{
				ast.KindVariableDeclaration: func(node *ast.Node) {
					declaration := node.AsVariableDeclaration()
					if declaration == nil || declaration.Initializer == nil {
						return
					}
					initializerType := ctx.TypeChecker.GetTypeAtLocation(declaration.Initializer)
					if initializerType == nil {
						return
					}
					typeNames = append(typeNames, ctx.TypeChecker.TypeToString(initializerType))
					ctx.ReportNode(node, rule.Message{Id: "sawType", Description: "saw a type"})
				},
			}
		},
	}

	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), []rule.Rule{typeReporter})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}

	if len(result.Diagnostics) != 2 {
		t.Fatalf("expected one finding per declaration, got %d", len(result.Diagnostics))
	}

	// The type names are the proof, and the asymmetry between them is the interesting part. A parser
	// sees two initializers that look alike; only a checker knows that `41 + 1` widens to `number`
	// while an untyped `const` string stays at its literal type. Neither answer is derivable from
	// syntax, so a nil or foreign checker cannot produce this pair.
	saw := map[string]bool{}
	for _, name := range typeNames {
		saw[name] = true
	}
	if !saw["number"] {
		t.Errorf("checker did not type the arithmetic initializer as number, saw %v", typeNames)
	}
	if !saw["\"ahra\""] {
		t.Errorf("checker did not keep the string initializer at its literal type, saw %v", typeNames)
	}
}

// TestWalkReportsCoverage asserts that a run says what it looked at, not only what it found.
func TestWalkReportsCoverage(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"main.ts":       "export const value = 1;\n",
	})

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	listening := rule.Rule{
		Name: "test-listening",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {}}
		},
	}
	// A rule that declines every file must be visibly distinct from one that ran and found nothing.
	// Four rules in the gate this tool replaces were dead for months inside that ambiguity.
	declining := rule.Rule{
		Name: "test-declining",
		Run:  func(ctx rule.Context, options any) rule.Listeners { return nil },
	}

	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), []rule.Rule{listening, declining})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}

	if result.Coverage.FilesWalked != 1 {
		t.Errorf("expected 1 file walked, got %d", result.Coverage.FilesWalked)
	}
	if result.Coverage.RulesRun != 2 {
		t.Errorf("expected 2 rules run, got %d", result.Coverage.RulesRun)
	}
	if result.Coverage.NodesVisited == 0 {
		t.Error("a walk that visited no nodes reported success")
	}
	if result.Coverage.FilesInProgram <= result.Coverage.FilesWalked {
		t.Errorf("program (%d) should exceed the walked set (%d)",
			result.Coverage.FilesInProgram, result.Coverage.FilesWalked)
	}
	if result.Coverage.RulesListening["test-listening"] != 1 {
		t.Errorf("the listening rule should have listened to 1 file, got %d",
			result.Coverage.RulesListening["test-listening"])
	}
	if result.Coverage.RulesListening["test-declining"] != 0 {
		t.Errorf("the declining rule should read as listening to 0 files, got %d",
			result.Coverage.RulesListening["test-declining"])
	}
}

// TestBuildFailsLoudly is the guard the whole package exists for: every way of ending up with no
// files is an error, never an empty success.
func TestBuildFailsLoudly(t *testing.T) {
	t.Run("no config named", func(t *testing.T) {
		if _, err := program.Build(program.Options{CurrentDirectory: t.TempDir()}); err == nil {
			t.Fatal("building with no tsconfig returned no error")
		}
	})

	t.Run("config does not exist", func(t *testing.T) {
		_, err := program.Build(program.Options{
			ConfigFileName:   "absent.json",
			CurrentDirectory: t.TempDir(),
		})
		if err == nil {
			t.Fatal("a missing tsconfig returned no error")
		}
	})

	t.Run("config matches no files", func(t *testing.T) {
		directory := writeProject(t, map[string]string{
			"tsconfig.json": `{"include":["./nothing-here/**/*.ts"]}`,
		})
		_, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
		if err == nil {
			t.Fatal("a config matching zero files built a program instead of failing")
		}
	})

	t.Run("config does not parse", func(t *testing.T) {
		directory := writeProject(t, map[string]string{
			"tsconfig.json": `{"compilerOptions": {"target": "NotARealTarget"}}`,
			"main.ts":       "export const value = 1;\n",
		})
		if _, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory}); err == nil {
			t.Fatal("an invalid compiler option built a program instead of failing")
		}
	})
}

// TestWalkRefusesAnEmptyRun asserts that walking nothing is an error rather than a clean result.
//
// A caller that filtered its file set down to nothing, or that forgot to register any rules, must
// hear about it. Returning an empty Result with a nil error would render both as a passing run.
func TestWalkRefusesAnEmptyRun(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"main.ts":       "export const value = 1;\n",
	})

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	if _, err := graph.Walk(context.Background(), nil, []rule.Rule{{Name: "any"}}); err == nil {
		t.Error("walking an empty file set returned a clean result")
	}
	if _, err := graph.Walk(context.Background(), graph.ProjectFiles(), nil); err == nil {
		t.Error("walking with no rules returned a clean result")
	}
}

// TestDiagnosticsReportsRealTypeErrors proves the graph reports TypeScript's own findings, in both
// directions: a broken file produces the error, and a correct one produces silence.
func TestDiagnosticsReportsRealTypeErrors(t *testing.T) {
	t.Run("reports a real error", func(t *testing.T) {
		directory := writeProject(t, map[string]string{
			"tsconfig.json": minimalConfig,
			"main.ts":       "const value: number = 'not a number';\n",
		})

		graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
		if err != nil {
			t.Fatalf("building: %v", err)
		}

		diagnostics := graph.Diagnostics(context.Background(), graph.ProjectFiles()[0])
		if len(diagnostics) == 0 {
			t.Fatal("assigning a string to a number produced no diagnostic")
		}

		foundAssignabilityError := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Code() == 2322 {
				foundAssignabilityError = true
			}
		}
		if !foundAssignabilityError {
			t.Errorf("expected TS2322, got %v", codesOf(diagnostics))
		}
	})

	// The half a violations-only corpus never has. A checker that reported errors everywhere would
	// pass the case above and fail this one.
	t.Run("stays silent on correct code", func(t *testing.T) {
		directory := writeProject(t, map[string]string{
			"tsconfig.json": minimalConfig,
			"main.ts":       "const value: number = 42;\nexport default value;\n",
		})

		graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
		if err != nil {
			t.Fatalf("building: %v", err)
		}

		diagnostics := graph.Diagnostics(context.Background(), graph.ProjectFiles()[0])
		if len(diagnostics) != 0 {
			t.Errorf("correct code reported %v", codesOf(diagnostics))
		}
	})
}

// TestCheckerResolvesAcrossFiles proves the graph is one program rather than a set of isolated
// parses: a type defined in one file is known when another file imports it.
func TestCheckerResolvesAcrossFiles(t *testing.T) {
	directory := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"Shape.ts":      "export interface Shape { sides: number; }\n",
		"main.ts":       "import type { Shape } from './Shape.ts';\nconst square: Shape = { sides: 'four' };\nexport default square;\n",
	})

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	found := false
	for _, sourceFile := range graph.ProjectFiles() {
		for _, diagnostic := range graph.Diagnostics(context.Background(), sourceFile) {
			if diagnostic.Code() == 2322 || diagnostic.Code() == 2418 {
				found = true
			}
		}
	}
	if !found {
		t.Error("a cross-file type violation went unreported, so the files are not one program")
	}
}

func codesOf(diagnostics []*ast.Diagnostic) []int32 {
	codes := make([]int32, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code())
	}
	return codes
}
