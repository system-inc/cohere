//go:build unix

// The stand-in Go engines here are /bin/sh scripts, as the Swift engine's are, so the file builds on Unix.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
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
// list when the run cache came to Go (see TestGoEngineArgumentsForwardNoCache), and `--timing` when the engine came to
// write its timing record (see TestTimingReachesTheGoEngine).
func TestGoEngineArgumentsRefuseWhatTheGoEngineDoesNotImplement(test *testing.T) {
	test.Parallel()
	for _, refused := range []string{
		"unused", "unused-all", "unused-deep", "explain", "fix-passes", "print-config",
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

// TestTimingReachesTheGoEngine: `--timing` is taken, not refused, and forwarded to the Go engine, which answers it with
// its timing record (#3apgjer). Swift's run forwards it as it always did, for its engine to refuse with exit 2, which
// the front door reports as nothing checked (TestRunEngineBinaryEndings' exit 2 case).
func TestTimingReachesTheGoEngine(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	given := map[string]bool{"no-fix": true, "timing": true}
	arguments, mode, argumentsError := engineArguments(goLanguage("module"), goFlags, goTestLocation(root), given,
		typedTrue, nil)
	if argumentsError != nil {
		test.Fatalf("--timing was refused for Go: %v", argumentsError)
	}
	expected := []string{"--contract", "3", "--root", root, "--no-fix", "--timing"}
	if !slices.Equal(arguments, expected) || mode != engineModeCheck {
		test.Errorf("arguments are\n  %q in mode %s\nexpected\n  %q in mode %s", arguments, mode, expected,
			engineModeCheck)
	}

	swiftArguments, _, swiftError := engineArguments(swiftLanguage, swiftFlags, goTestLocation(root), given, typedTrue,
		nil)
	if swiftError != nil || !slices.Contains(swiftArguments, "--timing") {
		test.Errorf("Swift's run did not forward --timing for its engine to refuse: %q, %v", swiftArguments, swiftError)
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
	if writeError := executable_testing.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); writeError != nil {
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

// TestARealGoRunNamesTheFilesOnlyAnotherPlatformBuilds: the real binary at a module whose `_windows.go` file and
// `//go:build plan9` file each hold a finding the printf rule reports, and whose third file imports "C" with cgo off.
// The engine checks the host's build alone, so it loads none of the three: the run is green with the exit a clean
// module has, its footer says "⚠ 3 Go files not checked", the account names each file with the engine's reason, and
// `--json` lists each under the gaps with its reason. Windows and plan9 are never the host a unix test runs on, so
// the proof holds on Linux as on a Mac (#72n9mq0).
func TestARealGoRunNamesTheFilesOnlyAnotherPlatformBuilds(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	finding := "\treturn fmt.Sprintf(\"%d\", \"text\")\n"
	for name, contents := range map[string]string{
		goModuleMarker: "module example.com/platforms\n\ngo 1.27\n",
		"clean.go":     "package platforms\n\n// Answer answers.\nfunc Answer() int {\n\treturn 42\n}\n",
		"open_windows.go": "package platforms\n\nimport \"fmt\"\n\n// Open is a fixture.\nfunc Open() string {\n" +
			finding + "}\n",
		"plan9.go": "//go:build plan9\n\npackage platforms\n\nimport \"fmt\"\n\n// Plan is a fixture.\n" +
			"func Plan() string {\n" + finding + "}\n",
		"native.go": "package platforms\n\nimport \"C\"\n\n// Native is a fixture.\nfunc Native() int {\n\treturn 1\n}\n",
	} {
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	// Cgo off, so the file importing "C" is left out on every host. The child mark is emptied, since a front door
	// that inherited it would refuse to start the engine at all.
	environment := []string{"CGO_ENABLED=0", goEngineChildVariable + "="}
	wantReasons := map[string]string{
		"native.go":       `imports "C", and cgo is off for `,
		"open_windows.go": "excluded by build constraints on ",
		"plan9.go":        "excluded by build constraints on ",
	}

	output, exitCode := runCohereWithEnvironment(test, binary, root, environment, "--no-fix", "--no-cache")
	if exitCode != 0 || strings.Contains(output, "cohere-go/printf") {
		test.Fatalf("exit %d, want the 0 of a clean module whose only findings are in files no host build reads:\n%s",
			exitCode, output)
	}
	if !strings.Contains(output, "✓ 💎") || !strings.Contains(output, " • ⚠ 3 Go files not checked") {
		test.Errorf("the footer does not count the three files in Go's words:\n%s", output)
	}
	for name, reason := range wantReasons {
		if !strings.Contains(output, name+" ("+reason) {
			test.Errorf("the account does not name %s with the reason %q:\n%s", name, reason, output)
		}
	}

	output, exitCode = runCohereWithEnvironment(test, binary, root, environment, "--no-fix", "--no-cache", "--json")
	if exitCode != 0 {
		test.Fatalf("under --json the run exited %d, want 0:\n%s", exitCode, output)
	}
	var summary struct {
		Verdict string `json:"verdict"`
		Gaps    struct {
			Unchecked []uncheckedEngineFile `json:"unchecked"`
		} `json:"gaps"`
	}
	if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, output)), &summary); decodeError != nil {
		test.Fatal(decodeError)
	}
	if summary.Verdict != "pass" || len(summary.Gaps.Unchecked) != len(wantReasons) {
		test.Fatalf("under --json the verdict is %q and the gap %v, want a pass naming the three files:\n%s",
			summary.Verdict, summary.Gaps.Unchecked, output)
	}
	for _, file := range summary.Gaps.Unchecked {
		reason, named := wantReasons[filepath.Base(file.File)]
		if !named || !strings.HasPrefix(file.Reason, reason) || file.Language != "Go" {
			test.Errorf("under --json the gap names %+v, want one of %v as a Go file", file, wantReasons)
		}
	}
}

// An engine run whose project record excludes the module's only file, and whose summary calls the run clean, fails
// through the front door: exit 1 where the engine said 0, the summary's reasons naming the file's reason and the rule
// that decided it, and the footer saying so. The control, the same stream with the file in scope, keeps the engine's
// exit 0 (#08cv2mp).
//
// Not parallel: it drives check runs through engineRun, whose record acceptors set the package-level activeSummary.
func TestAGoRunThatExcludedEveryFileFails(test *testing.T) {
	stream := func(filesOurs int, excluded string) string {
		return strings.Join([]string{
			`{"kind":"provenance","contract":3,"engine":"cohere-go","version":"0.1.0","commit":"dev",` +
				`"sourceTreeModified":false,"toolchain":"go1.27.0 darwin/arm64"}`,
			fmt.Sprintf(`{"kind":"project","root":"/project","package":"example.com/project",`+
				`"elapsedMilliseconds":1,"filesInPackage":1,"filesOurs":%d,"filesInScope":%d,"scopeDescription":"",`+
				`"targets":[],"excluded":[%s]}`, filesOurs, filesOurs, excluded),
			`{"kind":"fix","filesConsidered":0,"filesRewritten":0,"fixesApplied":0,"fixesRefused":0,` +
				`"refusalsByReason":{},"filesReformatted":0,"filesNotFormatted":0,"notFormattedReasons":{},` +
				`"formatScope":"every Go file in the module"}`,
			`{"kind":"phase","name":"fix","outcome":"ran","elapsedMilliseconds":0,"detail":""}`,
			`{"kind":"types","diagnostics":0,"files":0,"elapsedMilliseconds":0,"filesWithoutRecord":[],"build":"test"}`,
			`{"kind":"phase","name":"types","outcome":"ran","elapsedMilliseconds":0,"detail":""}`,
			`{"kind":"lint","findings":0,"rulesRun":5,"filesWalked":0,"nodesVisited":0,"elapsedMilliseconds":0,` +
				`"rulesSilent":[],"rulesWatchedAndQuiet":5,"crashes":[],"rulesScopedOff":{},"rulesNotConfigured":[],` +
				`"configNote":""}`,
			`{"kind":"phase","name":"lint","outcome":"ran","elapsedMilliseconds":0,"detail":""}`,
			`{"kind":"phase","name":"unused","outcome":"skipped","elapsedMilliseconds":0,"detail":"not requested"}`,
			`{"kind":"summary","findings":0,"complete":true,"exitCode":0}`,
		}, "\n")
	}
	printed := func(records string) string {
		return "cat <<'RECORDS'\n" + records + "\nRECORDS"
	}

	// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
	test.Run("every file ignored by git", func(test *testing.T) {
		// A process runs once, so nothing resets the summary between two runs in one test binary.
		activeSummary = runSummary{}
		out, exitCode, runError := runFakeGoEngine(test, printed(stream(0,
			`{"file":"/project/value.go","reason":"ignored by git","source":".gitignore:1"}`)))
		if runError != nil || exitCode != 1 {
			test.Fatalf("a run that checked nothing exited %d, err %v, want 1\n%s", exitCode, runError, out)
		}
		want := []excludedReason{{Reason: "ignored by git", Count: 1, Source: ".gitignore:1"}}
		if !slices.Equal(activeSummary.Gaps.Excluded, want) || !activeSummary.failed() {
			test.Errorf("the summary's reasons are %+v and failed %t, want %+v and a failure",
				activeSummary.Gaps.Excluded, activeSummary.failed(), want)
		}
		if got := footer(activeSummary, plain, footerOptions{}); !strings.Contains(got,
			"no files checked: 1 file excluded, ignored by git (.gitignore:1)") {
			test.Errorf("the footer does not say why nothing was checked: %q", got)
		}
	})

	// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
	test.Run("the control: the file in scope", func(test *testing.T) {
		activeSummary = runSummary{}
		out, exitCode, runError := runFakeGoEngine(test, printed(stream(1, "")))
		if runError != nil || exitCode != 0 || len(activeSummary.Gaps.Excluded) != 0 {
			test.Fatalf("a run with its file in scope exited %d, err %v, reasons %+v, want 0 and none\n%s",
				exitCode, runError, activeSummary.Gaps.Excluded, out)
		}
	})
}

// A Swift package's manifest was never a source to check, so a run that checked nothing counts only its sources in
// its reasons: an all-ignored package names the ignored file alone, and a package whose only file is its manifest has
// no reason at all, a project with none of its language's files, which passes with the warning (#08cv2mp).
//
// Not parallel: it reads and sets the package-level activeSummary, as countExcludedWhenNothingChecked does.
func TestAManifestIsNoReasonANothingCheckedRunCounts(test *testing.T) {
	type excludedEntry = struct {
		File   string `json:"file"`
		Reason string `json:"reason"`
		Source string `json:"source"`
	}
	manifest := excludedEntry{File: "/package/Package.swift", Reason: "a package manifest"}
	ignored := excludedEntry{File: "/package/Sources/Lib/Value.swift", Reason: "ignored by git", Source: ".gitignore:2"}
	for _, testCase := range []struct {
		name     string
		excluded []excludedEntry
		want     []excludedReason
	}{
		{"an all-ignored package", []excludedEntry{manifest, ignored},
			[]excludedReason{{Reason: "ignored by git", Count: 1, Source: ".gitignore:2"}}},
		{"a package of its manifest alone", []excludedEntry{manifest}, nil},
	} {
		// Not parallel: it reads and sets the package-level activeSummary.
		test.Run(testCase.name, func(test *testing.T) {
			activeSummary = runSummary{}
			activeSummary.Gaps.NothingToCheck = true
			run := newEngineRun(bufio.NewWriter(&bytes.Buffer{}), engineModeCheck, swiftLanguage, "", time.Now())
			run.project = &engineProjectRecord{Excluded: testCase.excluded}
			run.countExcludedWhenNothingChecked()
			if !slices.Equal(activeSummary.Gaps.Excluded, testCase.want) {
				test.Errorf("the reasons are %+v, want %+v", activeSummary.Gaps.Excluded, testCase.want)
			}
			if activeSummary.Gaps.nothingChecked() != (testCase.want != nil) {
				test.Errorf("nothingChecked is %t with reasons %+v", activeSummary.Gaps.nothingChecked(),
					activeSummary.Gaps.Excluded)
			}
		})
	}
}

// TestADiscoveredGoProjectNamesItsBuildsAndWhatNoRuleRanOn: at a root discovery splits into a TypeScript project and
// a Go workspace, adamic's shape, `--other-builds --json` writes the `. (Go)` summary with the builds it checked and
// its lint phase run, though a windows-only file fails to type-check (#rw6tg3a, #ytnvt8k). The windows build's load
// found that type error and analyzed no package, so its files were compiled and never linted: they are named as not
// checked, with why, never as checked by the windows build.
func TestADiscoveredGoProjectNamesItsBuildsAndWhatNoRuleRanOn(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"tsconfig.json":       discoveryTsconfig,
		"source/a.ts":         "export const a = 1;\n",
		"go.work":             "go 1.27\n\nuse .\n",
		"go.mod":              "module example.com/mixed\n\ngo 1.27\n",
		"sep/sep.go":          "// Package sep joins.\npackage sep\n\n// Separator joins.\nconst Separator = \"/\"\n",
		"sep/wide_windows.go": "// Package sep joins.\npackage sep\n\n// Wide is two.\nconst Wide = 2\n",
		"sep/broken_windows.go": "// Package sep joins.\npackage sep\n\n// Broken names what nothing declares.\n" +
			"const Broken = Missing\n",
	})
	// The Go engine reads the repository's index, so the fixture is a repository git knows, not an empty `.git`.
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
	}
	output, _ := runCohere(test, binary, root, "--no-fix", "--no-cache", "--other-builds", "--json")
	type goSummaryRecord struct {
		Kind   string         `json:"kind"`
		Label  string         `json:"label"`
		Builds []checkedBuild `json:"builds"`
		Phases []struct {
			Name    string `json:"name"`
			Outcome string `json:"outcome"`
			Detail  string `json:"detail"`
		} `json:"phases"`
		Gaps struct {
			Unchecked []uncheckedEngineFile `json:"unchecked"`
		} `json:"gaps"`
	}
	var goSummary *goSummaryRecord
	for _, line := range strings.Split(output, "\n") {
		var record goSummaryRecord
		if json.Unmarshal([]byte(line), &record) == nil && record.Kind == "summary" && record.Label == ". (Go)" {
			goSummary = &record
		}
	}
	if goSummary == nil {
		test.Fatalf("no `. (Go)` summary:\n%s", output)
	}
	if len(goSummary.Builds) != 2 {
		test.Errorf("the Go summary names %d builds, want the host's and windows's:\n%s", len(goSummary.Builds), output)
	}
	for _, phase := range goSummary.Phases {
		if phase.Name == "lint" && phase.Outcome != "ran" {
			test.Errorf("the Go project's lint phase %s: %s", phase.Outcome, phase.Detail)
		}
	}
	notLinted := map[string]string{}
	for _, each := range goSummary.Gaps.Unchecked {
		notLinted[filepath.Base(each.File)] = each.Reason
	}
	for _, file := range []string{"wide_windows.go", "broken_windows.go"} {
		if reason := notLinted[file]; !strings.HasPrefix(reason, "not linted: the windows/") {
			test.Errorf("%s is named not checked for %q, want not linted by the windows build:\n%s", file,
				reason, output)
		}
	}
}

// TestANamedDirectoryHoldingSeveralProjectsChecksEach: `--directory` naming a directory that holds a tsconfig.json and
// a go.work, adamic's shape, checks both projects and the TypeScript project nested below them, as `cd` there and
// `cohere` would, never TypeScript's alone in silence (#pswhv5q). `--other-builds`, a Go switch, then reaches the Go
// project rather than being refused as no Go project.
func TestANamedDirectoryHoldingSeveralProjectsChecksEach(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"tsconfig.json":     discoveryTsconfig,
		"source/a.ts":       "export const a = 1;\n",
		"go.work":           "go 1.27\n\nuse .\n",
		"go.mod":            "module example.com/mixed\n\ngo 1.27\n",
		"sep/sep.go":        "// Package sep joins.\npackage sep\n\n// Separator joins.\nconst Separator = \"/\"\n",
		"web/tsconfig.json": discoveryTsconfig,
		"web/source/b.ts":   "export const b = 1;\n",
	})
	// The Go engine reads the repository's index, so the fixture is a repository git knows, not an empty `.git`.
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
	}
	// The TypeScript fixture's own findings fail the run, which is beside the point: each project's summary is.
	output, _ := runCohere(test, binary, test.TempDir(), "--directory", root, "--no-fix", "--no-cache",
		"--other-builds", "--json")
	var labels []string
	for _, line := range strings.Split(output, "\n") {
		var record struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Kind == "summary" {
			labels = append(labels, record.Label)
		}
	}
	slices.Sort(labels)
	if want := []string{".", ". (Go)", "web"}; !slices.Equal(labels, want) {
		test.Errorf("the run wrote the summaries %q, want %q, each project's:\n%s", labels, want, output)
	}
}

// TestAChildRunAtADirectoryOfSeveralProjectsChecksItsOwnAlone: a project's run, which names its engine as every
// child of a run over several projects does, checks that one project at the directory and starts no run of its own,
// though the directory holds a tsconfig.json beside the go.work (#pswhv5q). Asked of the one process: it writes the Go
// project's summary alone, with the label its parent gave it.
func TestAChildRunAtADirectoryOfSeveralProjectsChecksItsOwnAlone(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"tsconfig.json": discoveryTsconfig,
		"source/a.ts":   "export const a = 1;\n",
		"go.work":       "go 1.27\n\nuse .\n",
		"go.mod":        "module example.com/mixed\n\ngo 1.27\n",
		"sep/sep.go":    "// Package sep joins.\npackage sep\n\n// Separator joins.\nconst Separator = \"/\"\n",
	})
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
	}
	output, exitCode := runCohereWithEnvironment(test, binary, test.TempDir(),
		[]string{projectEngineVariable + "=" + string(engineGo), projectLabelVariable + "=. (Go)"},
		"--directory", root, "--no-fix", "--no-cache", "--json")
	var labels []string
	for _, line := range strings.Split(output, "\n") {
		var record struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Kind == "summary" {
			labels = append(labels, record.Label)
		}
	}
	if exitCode != 0 || !slices.Equal(labels, []string{". (Go)"}) {
		test.Errorf("the child run exited %d with the summaries %q, want 0 and the Go project's alone:\n%s", exitCode,
			labels, output)
	}
}

// TestARunAProjectsRunStartedNeverStartsRunsOfItsOwn: a run marked as one another run started (runDepthVariable)
// that discovery hands several projects, here a tsconfig.json beside a go.work, refuses to start a run for any, by
// name, with exit 2, and writes no project's summary (#excff9x). It is the wall behind every guard that keeps a child
// from discovering: on Nov 10 one such guard removed forked about 7,700 copies, and with this wall a broken guard
// ends one level down.
func TestARunAProjectsRunStartedNeverStartsRunsOfItsOwn(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"tsconfig.json": discoveryTsconfig,
		"source/a.ts":   "export const a = 1;\n",
		"go.work":       "go 1.27\n\nuse .\n",
		"go.mod":        "module example.com/mixed\n\ngo 1.27\n",
		"sep/sep.go":    "// Package sep joins.\npackage sep\n\n// Separator joins.\nconst Separator = \"/\"\n",
	})
	output, exitCode := runCohereWithEnvironment(test, binary, root, []string{runDepthVariable + "=1"},
		"--no-fix", "--no-cache", "--json")
	summaries := 0
	for _, line := range strings.Split(output, "\n") {
		var record struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Kind == "summary" && record.Label != "" {
			summaries++
		}
	}
	if exitCode != 2 || summaries != 0 || !strings.Contains(output, "was about to start runs for 2 projects") {
		test.Errorf("the marked run exited %d with %d project summaries, want 2, none, and the refusal named:\n%s",
			exitCode, summaries, output)
	}
}
