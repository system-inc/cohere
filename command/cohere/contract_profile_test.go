//go:build unix

// The stand-in cohere-swift here is a /bin/sh script, as combined_footer_unix_test.go's is, so the file builds on Unix.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/pprof/profile"

	"github.com/system-inc/cohere/internal/settings"
	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// A child's profile sits beside the front door's, the child's name before the extension (#r33n96f), absolute
// against where the front door created its own.
func TestAChildProfileIsNamedBesideTheFrontDoorsByItsLanguage(test *testing.T) {
	test.Parallel()
	for _, named := range []struct{ given, want string }{
		{"run.pprof", "/work/run.go.pprof"},
		{"run", "/work/run.go"},
		{"profiles/run.cpu.pprof", "/work/profiles/run.cpu.go.pprof"},
		{"/elsewhere/run.pprof", "/elsewhere/run.go.pprof"},
		// A dot in a directory's name is no extension of the file's.
		{"/elsewhere.d/run", "/elsewhere.d/run.go"},
	} {
		if got := contractProfilePath("/work", named.given, "go"); got != named.want {
			test.Errorf("--profile %s names the child's %s, want %s", named.given, got, named.want)
		}
	}
}

// A run without `--profile` sends cohere-go the command line it always did, so nothing a replay could key on
// moves (#r33n96f); a profiled one adds the child's own file; and a child that takes no `--profile`, Swift's, is
// never sent one.
func TestOnlyAProfiledRunSendsTheChildAProfile(test *testing.T) {
	test.Parallel()
	location := projectLocation{Root: "/project", ArgumentBase: "/project/below", WorkingDirectory: "/work"}
	arguments := func(flags contractFlags, given map[string]bool, values map[string]string) []string {
		test.Helper()
		built, _, contractArgumentsError := contractArguments(goLanguage("module"), flags, location, given,
			func(name string) string { return values[name] }, nil)
		if contractArgumentsError != nil {
			test.Fatalf("refused: %v", contractArgumentsError)
		}
		return built
	}
	unprofiled := arguments(goFlags, map[string]bool{"no-fix": true}, map[string]string{"no-fix": "true"})
	if want := []string{"--contract", "4", "--root", "/project", "--no-fix"}; !slices.Equal(unprofiled, want) {
		test.Errorf("an unprofiled run sends %q, want %q", unprofiled, want)
	}
	profiled := arguments(goFlags, map[string]bool{"no-fix": true, "profile": true},
		map[string]string{"no-fix": "true", "profile": "run.pprof"})
	withProfile := []string{"--contract", "4", "--root", "/project", "--no-fix", "--profile", "/work/run.go.pprof"}
	if !slices.Equal(profiled, withProfile) {
		test.Errorf("a profiled run sends %q, want %q", profiled, withProfile)
	}
	swift := arguments(swiftFlags, map[string]bool{"profile": true}, map[string]string{"profile": "run.pprof"})
	if slices.Contains(swift, "--profile") {
		test.Errorf("cohere-swift, which takes no --profile, is sent %q", swift)
	}
}

// cohere-go refuses a profile it can't create with exit 2 and no record, before the run reads anything, so a
// caller never finds a run that read as profiled and left no profile (#r33n96f).
func TestTheGoChildRefusesAProfileItCannotCreate(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{goModuleMarker: "module example.com/refused\n\ngo 1.27\n"})
	command := exec.Command(binary, goLanguageVerb, "--contract", "4", "--root", root, "--no-fix", "--profile",
		filepath.Join(root, "missing", "run.pprof"))
	command.Env = append(os.Environ(), goChildVariable+"=1")
	var standardOutput strings.Builder
	var standardError strings.Builder
	command.Stdout, command.Stderr = &standardOutput, &standardError
	runError := command.Run()
	if command.ProcessState == nil || command.ProcessState.ExitCode() != 2 || standardOutput.Len() != 0 ||
		!strings.Contains(standardError.String(), "Creating the profile") {
		test.Errorf("the child ended %v, wrote %d bytes of records and said %q, want exit 2, none and the profile "+
			"named", runError, standardOutput.Len(), standardError.String())
	}
}

// A profiled run writes one parseable CPU profile for each process that did the work, at a mixed tree's Go module,
// Swift package and TypeScript program (#r33n96f): the front door's own at the file named, cohere-go's beside it
// named `go`, the TypeScript program's in the front door's file, since it runs in that process, and cohere-swift's
// not at all, said in a line rather than left as an empty file. A profiled run is never replayed, and the unprofiled
// run after it replays the one before it, so its key did not move and the profiled run recorded nothing over it.
func TestAProfiledRunWritesAProfileForEachProcessThatDidTheWork(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	fixture, absoluteError := filepath.Abs(filepath.Join(contractFixtureDirectory, "Clean.jsonl"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	swiftBinary := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(swiftBinary, []byte("#!/bin/sh\ncat '"+fixture+"'\n"),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	// Each project a repository of its own, so a bare run in one checks it alone, and the profiles outside all three,
	// so writing them moves no input a replay reads.
	goRoot, swiftRoot, typeScriptRoot := filepath.Join(base, "go"), filepath.Join(base, "swift"),
		filepath.Join(base, "web")
	profiles := filepath.Join(base, "profiles")
	writeTree(test, goRoot, map[string]string{
		goModuleMarker:   "module example.com/profiled\n\ngo 1.27\n",
		"alpha/alpha.go": "// Package alpha is one.\npackage alpha\n\n// MaxSize is a size.\nconst MaxSize = 1\n",
	})
	writeTree(test, swiftRoot, map[string]string{"Package.swift": "// swift-tools-version: 6.0\n"})
	writeTree(test, typeScriptRoot, map[string]string{
		".gitignore":     ".cache/\n",
		"tsconfig.json":  "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, \"include\": [\"source\"] }\n",
		"source/Tidy.ts": "export const tidy = 1;\n",
	})
	writeTree(test, profiles, map[string]string{".keep": ""})
	for _, root := range []string{goRoot, swiftRoot, typeScriptRoot} {
		initialize := exec.Command("git", "init", "--quiet")
		initialize.Dir = root
		if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
			test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
		}
	}
	// The child mark is emptied, since a front door that inherited it would refuse to start the child at all.
	environment := []string{goChildVariable + "=", "COHERE_SWIFT_BINARY=" + swiftBinary}
	// The footer's marker, since the account says "not replayed" of a run that was not.
	replayedMarker := "• replayed"
	written := func(test *testing.T, prefix string) []string {
		test.Helper()
		found, globError := filepath.Glob(filepath.Join(profiles, prefix+"*"))
		if globError != nil {
			test.Fatal(globError)
		}
		return found
	}

	// Each project, its profiles and its runs are its own, so the three run at once.
	test.Run("the Go module", func(test *testing.T) {
		test.Parallel()
		if output, exitCode := runCohereWithEnvironment(test, binary, goRoot, environment, "--no-fix"); exitCode != 0 ||
			strings.Contains(output, replayedMarker) {
			test.Fatalf("the first unprofiled run exited %d, want 0, checked rather than replayed:\n%s", exitCode,
				output)
		}
		frontDoor := filepath.Join(profiles, "go-run.pprof")
		if output, exitCode := runCohereWithEnvironment(test, binary, goRoot, environment, "--no-fix", "--profile",
			frontDoor); exitCode != 0 || strings.Contains(output, replayedMarker) {
			test.Fatalf("the profiled run exited %d, want 0, checked rather than replayed:\n%s", exitCode, output)
		}
		// In the glob's order, which is sorted.
		both := []string{filepath.Join(profiles, "go-run.go.pprof"), frontDoor}
		if found := written(test, "go-run"); !slices.Equal(found, both) {
			test.Errorf("the profiled run wrote %q, want the front door's and the child's, %q", found, both)
		}
		for _, path := range both {
			requireCpuProfile(test, path)
		}
		if output, exitCode := runCohereWithEnvironment(test, binary, goRoot, environment, "--no-fix"); exitCode != 0 ||
			!strings.Contains(output, replayedMarker) {
			test.Errorf("the unprofiled run after the profiled one exited %d, want 0, the first run replayed:\n%s",
				exitCode, output)
		}
	})

	test.Run("the Swift package", func(test *testing.T) {
		test.Parallel()
		frontDoor := filepath.Join(profiles, "swift-run.pprof")
		output, exitCode := runCohereWithEnvironment(test, binary, swiftRoot, environment, "--no-fix", "--profile",
			frontDoor)
		if exitCode != 0 || !strings.Contains(output, "cohere-swift takes no --profile, so "+frontDoor+
			" profiles the front door's process alone, and no Swift profile was written") {
			test.Errorf("the profiled Swift run exited %d, want 0 and the missing profile said:\n%s", exitCode, output)
		}
		if found := written(test, "swift-run"); !slices.Equal(found, []string{frontDoor}) {
			test.Errorf("the profiled Swift run wrote %q, want the front door's alone", found)
		}
		requireCpuProfile(test, frontDoor)
	})

	test.Run("the TypeScript program", func(test *testing.T) {
		test.Parallel()
		frontDoor := filepath.Join(profiles, "web-run.pprof")
		if output, exitCode := runCohereWithEnvironment(test, binary, typeScriptRoot, environment, "--no-fix",
			"--profile", frontDoor); exitCode != 0 {
			test.Errorf("the profiled TypeScript run exited %d, want 0:\n%s", exitCode, output)
		}
		if found := written(test, "web-run"); !slices.Equal(found, []string{frontDoor}) {
			test.Errorf("the profiled TypeScript run wrote %q, want the front door's alone, which ran the program",
				found)
		}
		requireCpuProfile(test, frontDoor)
	})
}

// A profiled run whose folder check sends its project to a run of its own profiles both processes, each to a file of
// its own (#r33n96f): the run that walked the folders to the file named, and the project's run beside it named
// `project`. Before, the project's run was handed the same file, and two processes writing one file left neither
// profile whole.
func TestAProfiledRunsProjectRunProfilesToAFileOfItsOwn(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".gitignore": ".cache/\n",
		"apps/web/CohereSettings.json": "{\n    \"extends\": \"cohere:typescript\",\n    \"rules\": {\n" +
			"        \"" + settings.AbbreviatedFolderName + "\": \"error\"\n    }\n}\n",
		"apps/web/tsconfig.json": "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, " +
			"\"include\": [\"source\"] }\n",
		"apps/web/source/tidy.ts": "export const tidy = 1;\n",
		"apps/web/docs/README.md": "# Documentation\n",
	})
	profiles := test.TempDir()
	frontDoor := filepath.Join(profiles, "run.pprof")
	output, exit := runCohere(test, binary, root, "--no-fix", "--directory", "apps/web", "--profile", frontDoor)
	if exit != 1 || !strings.Contains(output, "1 folder finding") {
		test.Fatalf("over docs/ the profiled run exited %d, want 1 counting the folder:\n%s", exit, output)
	}
	project := filepath.Join(profiles, "run.project.pprof")
	for _, path := range []string{frontDoor, project} {
		requireCpuProfile(test, path)
	}
}

// requireCpuProfile fails the test unless path holds a CPU profile `go tool pprof` reads, parsed whole, so a missing,
// empty, truncated or interleaved profile fails.
func requireCpuProfile(test *testing.T, path string) {
	test.Helper()
	file, openError := os.Open(path)
	if openError != nil {
		test.Errorf("no profile at %s: %v", path, openError)
		return
	}
	read, parseError := profile.Parse(file)
	_ = file.Close() // Ignored: the file was only read, and what it held is parsed above.
	if parseError != nil {
		test.Errorf("the profile at %s does not parse: %v", path, parseError)
		return
	}
	if len(read.SampleType) == 0 || read.SampleType[len(read.SampleType)-1].Type != "cpu" {
		test.Errorf("the profile at %s samples %v, want CPU", path, read.SampleType)
	}
}
