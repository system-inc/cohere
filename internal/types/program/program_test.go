package program_test

import (
	"context"
	"fmt"
	"github.com/microsoft/TypeScript/tsc/shim/locale"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/internal/types/program"
)

// writeProject lays a tiny TypeScript project on disk and returns its directory.
//
// Fixtures live on a real filesystem rather than in an in-memory one because that is the only way
// this test exercises the path production uses: bundled libs overlaid on osvfs, config resolution
// through `extends`, and module resolution across files. A memory FS would prove a different code
// path works.
func writeProject(test testing.TB, files map[string]string) string {
	test.Helper()

	directory := test.TempDir()
	for name, contents := range files {
		full := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(full), 0o755); createDirectoryError != nil {
			test.Fatalf("creating %s: %v", filepath.Dir(full), createDirectoryError)
		}
		if writeError := os.WriteFile(full, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", full, writeError)
		}
	}
	return directory
}

const minimalConfiguration = `{
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
func TestBuildProducesAProgramAndAChecker(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "export const greeting: string = 'hello';\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	// The program carries far more than our one file: every lib.*.d.ts the target pulls in. That is
	// the population the checker needs, and its size is why building it once and reusing it is the
	// point of this package.
	if len(graph.SourceFiles()) < 2 {
		test.Fatalf("expected the program to hold lib declarations plus our file, got %d", len(graph.SourceFiles()))
	}

	projectFiles := graph.ProjectFiles()
	if len(projectFiles) != 1 {
		test.Fatalf("expected exactly the one file the config named, got %d", len(projectFiles))
	}

	fileChecker, release := graph.CheckerForFile(context.Background(), projectFiles[0])
	defer release()
	if fileChecker == nil {
		test.Fatal("no checker for a file the program contains")
	}
}

// TestProjectFilesExcludesDeclarations proves the linted set is the config's own root set rather
// than the program's whole population.
//
// This matters more than it looks. The program's file paths are canonicalized (lowercased on a
// case-insensitive filesystem) while the config's names keep their original casing. Comparing the
// two without canonicalizing matches nothing, and "nothing" is indistinguishable from a clean tree.
// That is the exact shape of failure this project exists to refuse, so it is asserted rather than
// assumed.
func TestProjectFilesExcludesDeclarations(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"Alpha.ts":      "export const alpha = 1;\n",
		"Beta.ts":       "export const beta = 2;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	projectFiles := graph.ProjectFiles()
	if len(projectFiles) != 2 {
		test.Fatalf("expected the 2 files the config named, got %d", len(projectFiles))
	}
	if len(projectFiles) >= len(graph.SourceFiles()) {
		test.Fatalf("project files (%d) should be a strict subset of the program (%d)",
			len(projectFiles), len(graph.SourceFiles()))
	}
}

// TestWalkDispatchesRulesWithALiveChecker is the load-bearing assertion of this package.
//
// The rule here does not inspect syntax. It asks the checker for the type of an initializer and
// reports what it got back, which no amount of parsing can answer. A walk that dispatched rules but
// handed them a nil or foreign checker would pass a syntax-only test and fail this one.
func TestWalkDispatchesRulesWithALiveChecker(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "const counted: number = 41 + 1;\nconst named = 'ahra';\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	typeNames := []string{}
	typeReporter := rule.Rule[rule.NoOptions]{
		Name: "test-report-initializer-type",

		// Declared, because the walk acquires a checker only for rules that ask. The nil assertion
		// below is what catches an undeclared rule, and it caught this one.
		NeedsTypeChecker: true,

		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			if ruleContext.TypeChecker == nil {
				test.Error("a rule was dispatched without a checker")
				return nil
			}
			return rule.Listeners{
				ast.KindVariableDeclaration: func(node *ast.Node) {
					declaration := node.AsVariableDeclaration()
					if declaration == nil || declaration.Initializer == nil {
						return
					}
					initializerType := ruleContext.TypeChecker.GetTypeAtLocation(declaration.Initializer)
					if initializerType == nil {
						return
					}
					typeNames = append(typeNames, ruleContext.TypeChecker.TypeToString(initializerType))
					ruleContext.ReportNode(node, rule.Message{Id: "sawType", Description: "saw a type"})
				},
			}
		},
	}

	result, walkError := graph.Walk(
		context.Background(),
		graph.ProjectFiles(),
		rule_testing.EntriesOf(test, typeReporter),
	)
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}

	if len(result.Diagnostics) != 2 {
		test.Fatalf("expected one finding per declaration, got %d", len(result.Diagnostics))
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
		test.Errorf("checker did not type the arithmetic initializer as number, saw %v", typeNames)
	}
	if !saw["\"ahra\""] {
		test.Errorf("checker did not keep the string initializer at its literal type, saw %v", typeNames)
	}
}

// TestWalkReportsCoverage asserts that a run says what it looked at, not only what it found.
func TestWalkReportsCoverage(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "export const value = 1;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	listening := rule.Rule[rule.NoOptions]{
		Name: "test-listening",
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
				// Listens and reports nothing: the test tells a rule that ran quiet from one that declined.
			}}
		},
	}
	// A rule that declines every file must be visibly distinct from one that ran and found nothing.
	// Four rules in the gate this tool replaces were dead for months inside that ambiguity.
	declining := rule.Rule[rule.NoOptions]{
		Name: "test-declining",
		Run:  func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { return nil },
	}

	result, walkError := graph.Walk(
		context.Background(),
		graph.ProjectFiles(),
		rule_testing.EntriesOf(test, listening, declining),
	)
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}

	if result.Coverage.FilesWalked != 1 {
		test.Errorf("expected 1 file walked, got %d", result.Coverage.FilesWalked)
	}
	if result.Coverage.RulesRun != 2 {
		test.Errorf("expected 2 rules run, got %d", result.Coverage.RulesRun)
	}
	if result.Coverage.NodesVisited == 0 {
		test.Error("a walk that visited no nodes reported success")
	}
	if result.Coverage.FilesInProgram <= result.Coverage.FilesWalked {
		test.Errorf("program (%d) should exceed the walked set (%d)",
			result.Coverage.FilesInProgram, result.Coverage.FilesWalked)
	}
	if result.Coverage.RulesListening["test-listening"] != 1 {
		test.Errorf("the listening rule should have listened to 1 file, got %d",
			result.Coverage.RulesListening["test-listening"])
	}
	if result.Coverage.RulesListening["test-declining"] != 0 {
		test.Errorf("the declining rule should read as listening to 0 files, got %d",
			result.Coverage.RulesListening["test-declining"])
	}
}

// TestBuildFailsLoudly is the guard the whole package exists for: every way of ending up with no
// files is an error, never an empty success.
func TestBuildFailsLoudly(test *testing.T) {
	test.Parallel()
	test.Run("no config named", func(test *testing.T) {
		test.Parallel()
		if _, buildError := program.Build(program.Options{CurrentDirectory: test.TempDir()}); buildError == nil {
			test.Fatal("building with no tsconfig returned no error")
		}
	})

	test.Run("config does not exist", func(test *testing.T) {
		test.Parallel()
		_, buildError := program.Build(program.Options{
			ConfigFileName:   "absent.json",
			CurrentDirectory: test.TempDir(),
		})
		if buildError == nil {
			test.Fatal("a missing tsconfig returned no error")
		}
	})

	test.Run("config matches no files", func(test *testing.T) {
		test.Parallel()
		directory := writeProject(test, map[string]string{
			"tsconfig.json": `{"include":["./nothing-here/**/*.ts"]}`,
		})
		_, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
		if buildError == nil {
			test.Fatal("a config matching zero files built a program instead of failing")
		}
	})

	test.Run("config does not parse", func(test *testing.T) {
		test.Parallel()
		directory := writeProject(test, map[string]string{
			"tsconfig.json": `{"compilerOptions": {"target": "ES2022",, "strict": true}}`,
			"main.ts":       "export const value = 1;\n",
		})
		if _, buildError := program.Build(
			program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory},
		); buildError == nil {
			test.Fatal("a tsconfig with a JSON syntax error built a program instead of failing")
		}
	})

	// An option's value outside the values it takes is not a refusal (#wvgxtey, ruled by @system_cohere): the
	// graph builds and the diagnostic is the program's, so it is reported and fails the run instead of
	// building against silently wrong options.
	test.Run("an invalid option value builds and is reported", func(test *testing.T) {
		test.Parallel()
		directory := writeProject(test, map[string]string{
			"tsconfig.json": `{"compilerOptions": {"target": "NotARealTarget"}}`,
			"main.ts":       "export const value = 1;\n",
		})
		graph, buildError := program.Build(
			program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory},
		)
		if buildError != nil {
			test.Fatalf("an invalid option value refused the build: %v", buildError)
		}
		reported := false
		for _, diagnostic := range graph.ConfigDiagnostics(context.Background()) {
			reported = reported || (diagnostic.Code() == 6046 && graph.IsOptionsDiagnostic(diagnostic))
		}
		if !reported {
			test.Fatal("the invalid option value was not reported as an options diagnostic (TS6046)")
		}
	})
}

// TestWalkRefusesAnEmptyRun asserts that walking nothing is an error rather than a clean result.
//
// A caller that filtered its file set down to nothing, or that forgot to register any rules, must
// hear about it. Returning an empty Result with a nil error would render both as a passing run.
func TestWalkRefusesAnEmptyRun(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "export const value = 1;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	if _, walkError := graph.Walk(
		context.Background(),
		nil,
		rule_testing.EntriesOf(test, rule.Rule[rule.NoOptions]{Name: "any"}),
	); walkError == nil {
		test.Error("walking an empty file set returned a clean result")
	}
	if _, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), nil); walkError == nil {
		test.Error("walking with no rules returned a clean result")
	}
}

// TestDiagnosticsReportsRealTypeErrors proves the graph reports TypeScript's own findings, in both
// directions: a broken file produces the error, and a correct one produces silence.
func TestDiagnosticsReportsRealTypeErrors(test *testing.T) {
	test.Parallel()
	test.Run("reports a real error", func(test *testing.T) {
		test.Parallel()
		directory := writeProject(test, map[string]string{
			"tsconfig.json": minimalConfiguration,
			"main.ts":       "const value: number = 'not a number';\n",
		})

		graph, buildError := program.Build(
			program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory},
		)
		if buildError != nil {
			test.Fatalf("building: %v", buildError)
		}

		diagnostics := graph.Diagnostics(context.Background(), graph.ProjectFiles()[0])
		if len(diagnostics) == 0 {
			test.Fatal("assigning a string to a number produced no diagnostic")
		}

		foundAssignabilityError := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Code() == 2322 {
				foundAssignabilityError = true
			}
		}
		if !foundAssignabilityError {
			test.Errorf("expected TS2322, got %v", codesOf(diagnostics))
		}
	})

	// The half a violations-only corpus never has. A checker that reported errors everywhere would
	// pass the case above and fail this one.
	test.Run("stays silent on correct code", func(test *testing.T) {
		test.Parallel()
		directory := writeProject(test, map[string]string{
			"tsconfig.json": minimalConfiguration,
			"main.ts":       "const value: number = 42;\nexport default value;\n",
		})

		graph, buildError := program.Build(
			program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory},
		)
		if buildError != nil {
			test.Fatalf("building: %v", buildError)
		}

		diagnostics := graph.Diagnostics(context.Background(), graph.ProjectFiles()[0])
		if len(diagnostics) != 0 {
			test.Errorf("correct code reported %v", codesOf(diagnostics))
		}
	})
}

// TestCheckerResolvesAcrossFiles proves the graph is one program rather than a set of isolated
// parses: a type defined in one file is known when another file imports it.
func TestCheckerResolvesAcrossFiles(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"Shape.ts":      "export interface Shape { sides: number; }\n",
		"main.ts":       "import type { Shape } from './Shape.ts';\nconst square: Shape = { sides: 'four' };\nexport default square;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
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
		test.Error("a cross-file type violation went unreported, so the files are not one program")
	}
}

func codesOf(diagnostics []*ast.Diagnostic) []int32 {
	codes := make([]int32, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code())
	}
	return codes
}

// A rule that panics loses its own verdict on the file and nothing else.
//
// Rules walk a tree they did not build, and the compiler's own accessors panic rather than error on
// shapes they do not handle: Node.Text() panics on any kind outside its switch, and the rules reach
// it from 220 call sites. Files are walked in goroutines, a panic in a goroutine cannot be recovered
// by its parent, and nothing else in this codebase recovers.
//
// The boundary used to be the file, so one rule's panic cost every rule its verdict there:
// prefer-arrow-callback's shared arm crashed 71 files and took all 446 rules down on each, under an
// ordinary-looking summary (#qa9nttp). The panicking rules here are type-aware, one crashing in a
// listener after reading the checker and one crashing in Run, and the rule beside them must still
// report on every file.
func TestAPanickingRuleLosesOnlyItsOwnVerdictOnTheFile(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "const counted: number = 41 + 1;\n",
		"other.ts":      "const named = 'ahra';\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	panicsInAListener := rule.Rule[rule.NoOptions]{
		Name:             "test-typed-panics-in-a-listener",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindVariableDeclaration: func(node *ast.Node) {
					if ruleContext.TypeChecker == nil {
						test.Error("a type-aware rule ran without a checker, so this is not the typed path")
					}
					panic("simulated accessor panic on an unhandled kind")
				},
			}
		},
	}
	panicsInRun := rule.Rule[rule.NoOptions]{
		Name:             "test-typed-panics-in-run",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			panic("simulated panic before any listener")
		},
	}
	witness := rule.Rule[rule.NoOptions]{
		Name: "test-reports-every-declaration",
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindVariableDeclaration: func(node *ast.Node) {
					ruleContext.ReportNode(node, rule.Message{Id: "sawDeclaration", Description: "saw a declaration"})
				},
			}
		},
	}

	result, walkError := graph.Walk(
		context.Background(),
		graph.ProjectFiles(),
		rule_testing.EntriesOf(test, panicsInAListener, panicsInRun, witness),
	)
	if walkError != nil {
		test.Fatalf("a panic in one rule ended the whole walk: %v", walkError)
	}

	if len(result.Coverage.FilesCrashed) != 0 {
		test.Errorf("a rule's panic cost its file every rule's verdict: %v", result.Coverage.FilesCrashed)
	}

	reported := map[string]int{}
	for _, diagnostic := range result.Diagnostics {
		reported[filepath.Base(diagnostic.SourceFile.FileName().AsString())]++
	}
	for _, file := range []string{"main.ts", "other.ts"} {
		if reported[file] != 1 {
			test.Errorf("the rule beside the panicking ones reported %d times on %s, want 1", reported[file], file)
		}
	}

	crashed := map[string]int{}
	for _, crash := range result.Coverage.RulesCrashed {
		if crash.FileName == "" {
			test.Error("a rule crash was recorded without its file, so a reader cannot find it")
		}
		if crash.Cause == nil || !strings.Contains(crash.Cause.Error(), "simulated") {
			test.Errorf("rule %s crashed without its panic as the cause: %v", crash.RuleName, crash.Cause)
		}
		crashed[crash.RuleName]++
	}
	for _, name := range []string{panicsInAListener.Name, panicsInRun.Name} {
		if crashed[name] != 2 {
			test.Errorf("rule %s is named as crashing on %d files, want both", name, crashed[name])
		}
	}
	if crashed[witness.Name] != 0 {
		test.Errorf("the rule that never panicked is named as crashing")
	}
}

// A rule's Program is a view the walk keeps in the rule's slot and points at each file in turn
// (#9xfg09f), so it is good for its file only. A rule that keeps it past the file reads nothing through it:
// every Program a rule kept is ended once the walk is over, and a read panics naming the rule. Reads made
// during the file go through, which is the half that shows the view was live when the rule was given it.
func TestAProgramARuleKeepsPastItsFileIsEnded(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "const counted: number = 41 + 1;\n",
		"other.ts":      "const other: number = 2;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	var keptLock sync.Mutex
	var kept []rule.Program
	keeper := rule.Rule[rule.NoOptions]{
		Name:         "test-keeps-its-program",
		ProgramReads: rule.ReadsCompilerOptions,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			if ruleContext.Program.Options() == nil {
				test.Error("the program view read no options during its own file")
			}
			keptLock.Lock()
			kept = append(kept, ruleContext.Program)
			keptLock.Unlock()
			return rule.Listeners{
				ast.KindVariableDeclaration: func(node *ast.Node) { ruleContext.Program.Options() },
			}
		},
	}

	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, keeper))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}
	if len(result.Coverage.RulesCrashed) != 0 {
		test.Fatalf("reading the program during its own file crashed the rule: %v", result.Coverage.RulesCrashed)
	}
	if len(kept) != 2 {
		test.Fatalf("the rule kept %d programs, want one for each of the 2 files", len(kept))
	}

	for index, view := range kept {
		func() {
			defer func() {
				message := fmt.Sprint(recover())
				if !strings.Contains(message, "after the file it was given ended") ||
					!strings.Contains(message, keeper.Name) {
					test.Errorf(
						"program %d, kept past its file, read without the ended refusal naming the rule: %s",
						index,
						message,
					)
				}
			}()
			view.Options()
		}()
	}
}

// The other direction: a walk with no panic reports no crashed file.
//
// Without this, a boundary that recorded a crash for every file would pass the test above while
// making the crash line meaningless, which is the same vacuous shape as a probe that cannot fail.
func TestAWalkWithoutAPanicReportsNoCrashedFile(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "const counted: number = 41 + 1;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	quiet := rule.Rule[rule.NoOptions]{
		Name: "test-reports-nothing",
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindVariableDeclaration: func(node *ast.Node) {
					// Listens and reports nothing, so the walk runs it and nothing in it can panic.
				},
			}
		},
	}

	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, quiet))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}

	if len(result.Coverage.FilesCrashed) != 0 || len(result.Coverage.RulesCrashed) != 0 {
		test.Errorf(
			"a clean walk reported %d crashed files and %d rule crashes",
			len(result.Coverage.FilesCrashed),
			len(result.Coverage.RulesCrashed),
		)
	}
}

// A real compiler diagnostic renders its message through Localize, and not through MessageText.
//
// Every diagnostic the checker produces carries a message template and its arguments separately.
// `messageText` is populated only for external diagnostics that arrive already localized, so reading
// it printed `error TS2322: ` with nothing after the colon on a genuine type error. That is worse
// than not printing: the reader is told a file is broken and not told how.
//
// This matters more than one format string, because types gate lint. A type error stops the run
// before any rule looks at anything, so the sentence it prints is the entire output of a failing
// verification.
func TestACompilerDiagnosticRendersThroughLocalize(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"main.ts":       "export const counted: string = 41 + 1;\n",
	})

	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	diagnostics := graph.AllDiagnostics(context.Background())
	if len(diagnostics) == 0 {
		test.Fatal("the fixture produced no diagnostics, so this test proves nothing about rendering one")
	}

	diagnostic := diagnostics[0]
	if text := diagnostic.MessageText(); text != "" {
		test.Errorf("MessageText is populated for a compiler diagnostic, which would make Localize "+
			"unnecessary and this test stale: %q", text)
	}

	rendered := diagnostic.Localize(locale.Locale{})
	if rendered == "" {
		test.Fatal("a real compiler diagnostic rendered as the empty string")
	}
	if !strings.Contains(rendered, "number") || !strings.Contains(rendered, "string") {
		test.Errorf("the rendered message did not carry its arguments: %q", rendered)
	}
}
