//go:build unix

// The stand-in Go engines here are /bin/sh scripts, as the Swift engine's are, so the file builds on Unix.

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// typedTrue reads every flag as given and true, the way a caller who typed it would.
func typedTrue(string) string { return "true" }

// goTestLocation is a Go module root with a lint config named on the command line.
func goTestLocation(root string) projectLocation {
	return projectLocation{
		Root:               root,
		Engine:             engineGo,
		LintConfigFileName: filepath.Join(root, "Settings.json"),
		ArgumentBase:       root,
		WorkingDirectory:   root,
	}
}

// TestGoEngineArgumentsForwardWhatTheGoEngineReads: the command line is the one agreed with the engine on
// #a0cgmh6, the contract and the root first, each typed switch it reads, the lint config by its absolute
// path, and each path made absolute, while the flags the front door answers never reach the engine.
func TestGoEngineArgumentsForwardWhatTheGoEngineReads(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	given := map[string]bool{
		"no-fix": true, "lint": true, "single-threaded": true, "lint-config": true,
		"json": true, "verbose": true, "coverage": true, "phases": true,
	}
	arguments, mode, argumentsError := engineArguments(
		goLanguage("module"), goFlags, goTestLocation(root), given, typedTrue, []string{"a.go", "/elsewhere/b.go"})
	if argumentsError != nil {
		test.Fatal(argumentsError)
	}
	expected := []string{
		"--contract", "3", "--root", root, "--no-fix", "--lint", "--single-threaded",
		"--lint-config", filepath.Join(root, "Settings.json"), filepath.Join(root, "a.go"), "/elsewhere/b.go",
	}
	if !slices.Equal(arguments, expected) {
		test.Errorf("arguments are\n  %q\nexpected\n  %q", arguments, expected)
	}
	if mode != engineModeCheck {
		test.Errorf("mode %s, expected %s", mode, engineModeCheck)
	}
	_, mode, listingError := engineArguments(goLanguage("module"), goFlags, goTestLocation(root),
		map[string]bool{"rules-enabled": true, "version": true}, typedTrue, nil)
	if listingError != nil {
		test.Fatal(listingError)
	}
	if mode != engineModeRulesEnabled {
		test.Errorf("--rules-enabled with --version asked mode %s, expected %s, as a TypeScript run reads it",
			mode, engineModeRulesEnabled)
	}
}

// TestGoEngineArgumentsRefuseWhatTheGoEngineDoesNotImplement: every flag the Go engine has no meaning for is
// refused by name, naming Go, rather than dropped into a run that looks like it honored it. `--no-cache` left this
// list when the run cache came to Go (see TestGoEngineArgumentsForwardNoCache).
func TestGoEngineArgumentsRefuseWhatTheGoEngineDoesNotImplement(test *testing.T) {
	test.Parallel()
	for _, refused := range []string{
		"unused", "unused-all", "unused-deep", "timing", "explain", "fix-passes", "print-config",
	} {
		test.Run(refused, func(test *testing.T) {
			test.Parallel()
			_, _, argumentsError := engineArguments(goLanguage("module"), goFlags, goTestLocation(test.TempDir()),
				map[string]bool{"no-fix": true, refused: true}, typedTrue, nil)
			expected := "--" + refused + " is not implemented for Go"
			if argumentsError == nil || !strings.Contains(argumentsError.Error(), expected) {
				test.Fatalf("--%s was not refused by name for Go: %v", refused, argumentsError)
			}
		})
	}
}

// TestGoEngineArgumentsForwardNoCache: `--no-cache` is taken, not refused, and forwarded to the engine, so a cache
// the engine keeps of its own is off along with the front door's run cache.
func TestGoEngineArgumentsForwardNoCache(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	arguments, mode, argumentsError := engineArguments(goLanguage("module"), goFlags, goTestLocation(root),
		map[string]bool{"no-fix": true, "no-cache": true}, typedTrue, nil)
	if argumentsError != nil {
		test.Fatalf("--no-cache was refused for Go: %v", argumentsError)
	}
	expected := []string{"--contract", "3", "--root", root, "--no-fix", "--no-cache"}
	if !slices.Equal(arguments, expected) || mode != engineModeCheck {
		test.Errorf("arguments are\n  %q in mode %s\nexpected\n  %q in mode %s", arguments, mode, expected,
			engineModeCheck)
	}
}

// TestOtherBuildsReachTheGoEngineAlone: `--other-builds` is forwarded to the Go engine, which checks every build the
// project's files name (#72n9mq0). Swift's run refuses it by name, and discovery hands it to no project's run but a Go
// one's, so at a root holding a Swift package beside a Go module the Swift run is not refused over it.
func TestOtherBuildsReachTheGoEngineAlone(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	arguments, mode, argumentsError := engineArguments(goLanguage("module"), goFlags, goTestLocation(root),
		map[string]bool{"no-fix": true, "other-builds": true}, typedTrue, nil)
	if argumentsError != nil {
		test.Fatalf("--other-builds was refused for Go: %v", argumentsError)
	}
	expected := []string{"--contract", "3", "--root", root, "--no-fix", "--other-builds"}
	if !slices.Equal(arguments, expected) || mode != engineModeCheck {
		test.Errorf("arguments are\n  %q in mode %s\nexpected\n  %q in mode %s", arguments, mode, expected,
			engineModeCheck)
	}

	_, _, swiftError := engineArguments(swiftLanguage, swiftFlags, goTestLocation(root),
		map[string]bool{"no-fix": true, "other-builds": true}, typedTrue, nil)
	if swiftError == nil || !strings.Contains(swiftError.Error(), "--other-builds is not implemented for Swift") {
		test.Errorf("--other-builds was not refused by name for Swift: %v", swiftError)
	}

	given := []string{"--no-fix", "--other-builds", "-other-builds=true", "--json", "--lint-config", "/a/b.json"}
	if kept := withoutGoOnlySwitches(given); !slices.Equal(kept,
		[]string{"--no-fix", "--json", "--lint-config", "/a/b.json"}) {
		test.Errorf("another engine's child is handed %q, want every flag but --other-builds", kept)
	}
}

// TestOtherBuildsAreRefusedAtARootWithNoGoProject: discovery hands `--other-builds` to a Go project's run alone, so
// at a root of only TypeScript and Swift projects it would reach no run and the run would read as one that checked
// what was asked. Such a root refuses it before any project runs, the way a run at a TypeScript project does, while
// a root holding a Go module takes it, and `--other-builds=false` asks for nothing to refuse.
func TestOtherBuildsAreRefusedAtARootWithNoGoProject(test *testing.T) {
	test.Parallel()
	typeScriptAndSwift := map[string]string{
		"web/tsconfig.json":   discoveryTsconfig,
		"web/source/a.ts":     "export const a = 1;\n",
		"tools/Package.swift": discoveryPackage,
	}
	withGo := map[string]string{"service/go.mod": "module example.com/service\n\ngo 1.27\n"}
	for path, contents := range typeScriptAndSwift {
		withGo[path] = contents
	}
	for name, testCase := range map[string]struct {
		files   map[string]string
		value   string
		refused bool
	}{
		"TypeScript and Swift, the switch on":    {files: typeScriptAndSwift, value: "true", refused: true},
		"TypeScript and Swift, the switch off":   {files: typeScriptAndSwift, value: "false"},
		"a Go module beside them, the switch on": {files: withGo, value: "true"},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			found, discoverProjectsError := discoverProjects(newRepository(test, testCase.files), nil)
			if discoverProjectsError != nil {
				test.Fatal(discoverProjectsError)
			}
			refuseError := refuseGoOnlySwitchesWithoutGo(
				found,
				map[string]bool{"other-builds": true},
				func(string) string { return testCase.value },
			)
			if refused := refuseError != nil; refused != testCase.refused {
				test.Errorf("refused is %v (%v), want %v for the projects %+v", refused, refuseError,
					testCase.refused, found.Projects)
			}
		})
	}

	// The real binary at a root of two TypeScript projects, so the refusal is proven where discovery calls it,
	// before either project's run starts.
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"web/tsconfig.json":   discoveryTsconfig,
		"web/source/a.ts":     "export const a = 1;\n",
		"admin/tsconfig.json": discoveryTsconfig,
		"admin/source/b.ts":   "export const b = 2;\n",
	})
	output, exitCode := runCohere(test, binary, root, "--no-fix", "--other-builds")
	if exitCode == 0 || !strings.Contains(output, "--other-builds checks the other builds of a Go project's files") ||
		strings.Contains(output, "(TypeScript) ==") || strings.Contains(output, "✓") {
		test.Errorf("a root with no Go project exited %d over --other-builds, want it refused before any run:\n%s",
			exitCode, output)
	}
}

// TestGoContainerNamesAWorkspaceOrAModule: a root holding a go.work is a workspace in the run's words, and
// one holding only a go.mod is a module.
func TestGoContainerNamesAWorkspaceOrAModule(test *testing.T) {
	test.Parallel()
	module := test.TempDir()
	moduleFile := filepath.Join(module, goModuleMarker)
	if writeError := os.WriteFile(moduleFile, []byte("module example.com/a\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if container := goContainer(module); container != "module" {
		test.Errorf("a go.mod root is a %q, expected a module", container)
	}
	workspace := test.TempDir()
	workspaceFile := filepath.Join(workspace, goWorkspaceMarker)
	if writeError := os.WriteFile(workspaceFile, []byte("go 1.27\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if container := goContainer(workspace); container != "workspace" {
		test.Errorf("a go.work root is a %q, expected a workspace", container)
	}
}

// TestRunGoEngineRefusesATsconfig: --tsconfig names a TypeScript program, which a Go root does not have.
func TestRunGoEngineRefusesATsconfig(test *testing.T) {
	test.Parallel()
	_, runError := runGoEngine(goTestLocation(test.TempDir()), map[string]bool{"tsconfig": true}, nil)
	if runError == nil || !strings.Contains(runError.Error(), "is a Go module, so there is no tsconfig to use") {
		test.Fatalf("--tsconfig at a Go root was not refused: %v", runError)
	}
}

// runRealCohereAtAGoRoot runs the real cohere binary, not a stand-in, at a directory holding only a go.mod,
// with the environment entries given, and a deadline. A front door that started engines without end would
// otherwise hang the test and the pool with it, so running past the deadline fails the test instead.
func runRealCohereAtAGoRoot(test *testing.T, environment ...string) (string, int) {
	test.Helper()
	binary := buildCohere(test)
	root := test.TempDir()
	moduleFile := filepath.Join(root, goModuleMarker)
	moduleContents := []byte("module example.com/lonely\n\ngo 1.27\n")
	if writeError := os.WriteFile(moduleFile, moduleContents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	// One finding only the engine's own rules can make, so a run that reports it is a run the engine did.
	sourceContents := []byte("package lonely\n\nimport \"fmt\"\n\nfunc Greet() string {\n" +
		"\treturn fmt.Sprintf(\"%d\", \"text\")\n}\n")
	if writeError := os.WriteFile(filepath.Join(root, "lonely.go"), sourceContents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(deadline, binary, "--no-fix")
	command.Dir = root
	// Never a replay: the run record lives in the root's own .cache/cohere, and the root is new.
	command.Env = withoutVariable(os.Environ(), goEngineChildVariable)
	command.Env = append(command.Env, environment...)
	output, runError := command.CombinedOutput()
	if deadline.Err() != nil {
		test.Fatalf("cohere at a Go root was still running after 2 minutes, so it was killed:\n%s", output)
	}
	var exitError *exec.ExitError
	if runError != nil && !errors.As(runError, &exitError) {
		test.Fatalf("cohere did not run: %v", runError)
	}
	exitCode := 0
	if exitError != nil {
		exitCode = exitError.ExitCode()
	}
	return string(output), exitCode
}

// TestARealCohereAtAGoRootStartsOneEngine: the front door starts the Go engine once, the hidden dispatch
// answers it, and the engine checks the module: vet's printf rule finds the mismatched verb, and the run fails
// for it. The guard against a second engine never trips, because the dispatch is what answered.
func TestARealCohereAtAGoRootStartsOneEngine(test *testing.T) {
	test.Parallel()
	output, exitCode := runRealCohereAtAGoRoot(test)
	if exitCode != 1 || !strings.Contains(output, "lonely.go:6:") || !strings.Contains(output, "cohere-go/printf") {
		test.Fatalf("exit %d, expected 1 with the engine's printf finding on lonely.go:6:\n%s", exitCode, output)
	}
	if strings.Contains(output, "nothing was checked") {
		test.Errorf("the engine's run read as one that checked nothing:\n%s", output)
	}
	if strings.Contains(output, goEngineChildVariable) {
		test.Errorf("the guard against a second engine tripped, so the dispatch did not answer:\n%s", output)
	}
}

// TestAFrontDoorStartedAsAGoEngineStartsNoOther: a process carrying the child mark that reaches the front
// door's Go branch refuses to start an engine and exits 2, the contract's "did not run".
func TestAFrontDoorStartedAsAGoEngineStartsNoOther(test *testing.T) {
	test.Parallel()
	output, exitCode := runRealCohereAtAGoRoot(test, goEngineChildVariable+"=1")
	if exitCode != 2 || !strings.Contains(output, "was about to start another, so it refused") {
		test.Fatalf("exit %d, expected 2 with the guard's sentence:\n%s", exitCode, output)
	}
	if strings.Contains(output, "cohere-go/printf") {
		test.Errorf("an engine was started though the guard should have refused first:\n%s", output)
	}
}

// runFakeGoEngine runs a shell script standing in for `cohere go-engine` through the process boundary every
// Go run crosses, rendered in Go's words.
func runFakeGoEngine(test *testing.T, body string) (string, int, error) {
	test.Helper()
	path := filepath.Join(test.TempDir(), "cohere")
	if writeError := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	var out bytes.Buffer
	var standardError bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, goLanguage("module"), "", time.Now())
	exitCode, runError := runEngineBinary(path, []string{goEngineVerb, "--contract", "3"}, run, &standardError)
	flushStream(test, stream)
	return out.String(), exitCode, runError
}

// Not parallel: its subtests drive check runs through engineRun, whose record acceptors set the
// package-level activeSummary.
func TestAGoEngineThatDoesNotFinishChecksNothing(test *testing.T) {
	fixture, absoluteError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract", "GoClean.jsonl"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}

	// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
	test.Run("the control: a whole clean stream is green", func(test *testing.T) {
		out, exitCode, runError := runFakeGoEngine(test, "cat '"+fixture+"'")
		if runError != nil || exitCode != 0 {
			test.Fatalf("a clean Go engine run exited %d, err %v\n%s", exitCode, runError, out)
		}
		if !strings.Contains(out, "module described in") || strings.Contains(out, "Swift") {
			test.Errorf("the run was not rendered in Go's words:\n%s", out)
		}
	})

	cases := []struct {
		name string
		body string
	}{
		{"a stream cut off after the fix phase", "head -4 '" + fixture + "'"},
		{"an engine that printed nothing", "exit 0"},
		{"an engine killed by a signal", "head -2 '" + fixture + "'; kill -9 $$"},
	}
	for _, testCase := range cases {
		// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
		test.Run(testCase.name, func(test *testing.T) {
			out, exitCode, runError := runFakeGoEngine(test, testCase.body)
			if exitCode != 1 || runError == nil || !strings.Contains(runError.Error(), "the Go engine") ||
				!strings.Contains(runError.Error(), "nothing was checked") {
				test.Fatalf("exit %d, err %v\n%s", exitCode, runError, out)
			}
			if strings.Contains(out, "Swift") {
				test.Errorf("a Go run named Swift:\n%s", out)
			}
		})
	}
}
