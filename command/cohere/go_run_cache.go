package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/types/program"
)

// The run cache for a Go root (#c387bta). A Go run uses the TypeScript run cache whole: the table in
// .cache/cohere, the invocations it admits (runCacheEligible), the key, the clock, the tee, the replay and the
// record. What differs is where the inputs come from. A TypeScript run's are observed by the recording
// filesystem its graph build reads through. A Go run's are read by `go list` and the loader in the engine's
// process, out of the front door's sight, so the engine declares them in its inputs record and the front door
// adds what it knows itself: the settings file it hands the engine, and the facts in goRunCacheFacts.
//
// The replay is decided here, before the engine is started, so a run with nothing changed costs one stat of each
// recorded input and never a process. Starting the engine at all would cost more than the whole budget.

// beginGoRunCache replays a recorded Go run and exits when every input is unchanged, and otherwise starts
// recording this one. It does nothing for a run the run cache does not admit, `--no-cache` among them.
func beginGoRunCache(location projectLocation) {
	if !runCacheEligible() {
		return
	}
	anchor := program.NewPathAnchor(location.Root)
	facts := goRunCacheFacts(anchor, location.LintConfigFileName, activeOutput.Style.color, os.Environ(),
		exec.LookPath)
	// No section beyond the run's own: the findings, shapes and types sections serve a TypeScript graph's walk,
	// and the Go engine walks none of them.
	session := beginRunCacheSession(location.Root, anchor, program.CacheTableSections{}, facts...)
	if session == nil {
		return
	}
	session.projectRoots = []string{anchor.Root(), anchor.Resolved()}
	// Decided before the run, from what the run is given: the record could not name what it would read.
	if reason := goEnvironmentUnnameable(os.Environ()); reason != "" {
		declineRunCache(reason)
		session.note("the run cache does not record this run: " + reason)
	}
}

// goEnvironmentUnnameable is why a run under environment reads what no inputs record can name, empty when it does
// not: a `GO*` variable, `GOFLAGS` among them, carrying `-overlay`, whose file replaces source files by others the
// record never sees, so editing one of those would replay a verdict about the files it replaced.
func goEnvironmentUnnameable(environment []string) string {
	for _, variable := range environment {
		name, value, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(name, "GO") && strings.Contains(value, "-overlay") {
			return fmt.Sprintf("%s carries -overlay, whose replacement files the record cannot name", name)
		}
	}
	return ""
}

// goKeyedVariables are the variables a Go run's key takes by name, beside every `GO*` and `CGO_*` one. See
// goRunCacheFacts for why each is there.
var goKeyedVariables = []string{"HOME", "XDG_CONFIG_HOME", "CC", "CXX", "PKG_CONFIG", "BURNTSUSHI_TOML_110"}

// goRunCacheFacts is what a Go run's verdict depends on that is no file the engine reads, each a fact in the run's
// key. Computed with no subprocess, since a replay pays for them and `go env` alone costs more than the replay may.
//
// The engine's name, so a root that holds a tsconfig too never replays one engine's run as the other's. The root
// resolved and the settings file the engine reads, as a TypeScript run keys them. Whether the output is colored,
// since a recording made for a terminal carries escape codes a replay into a pipe must not print. Every variable
// whose name starts with `GO` or `CGO_`, by name and value, sorted: `GOFLAGS` (its `-tags` among it), `GOOS`,
// `GOARCH`, `CGO_ENABLED`, `GOWORK`, `GOENV`, `GOTOOLCHAIN` and the rest each change what `go list` loads, and
// one the go command reads that nobody listed here is still covered. `HOME` and `XDG_CONFIG_HOME`, since the go
// command finds its own settings file, its default `GOPATH` and with it the module cache under them. `CC`, `CXX`
// and `PKG_CONFIG`, since a package with cgo of its own is loaded through the cgo tool, whose compiler and headers
// no record can name. `BURNTSUSHI_TOML_110`, which the TOML reader of staticcheck's config consults. And the `go`
// the loader will run, as PATH finds it and as its links resolve, so another toolchain on PATH misses even where
// the engine's own record of the binary would not show it.
func goRunCacheFacts(
	anchor program.PathAnchor,
	settingsFile string,
	color bool,
	environment []string,
	lookPath func(string) (string, error),
) []string {
	facts := []string{
		"engine=Go",
		"root=" + anchor.Resolved(),
		"lint-config=" + anchor.Stable(settingsFile),
		fmt.Sprintf("color=%t", color),
		"go=" + goOnPath(lookPath),
	}
	variables := []string{}
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(name, "GO") || strings.HasPrefix(name, "CGO_") || slices.Contains(goKeyedVariables, name) {
			variables = append(variables, variable)
		}
	}
	// Sorted, so the same environment in another order is the same key.
	slices.Sort(variables)
	for _, variable := range variables {
		facts = append(facts, "environment "+variable)
	}
	return facts
}

// goOnPath is the `go` PATH finds, absolute, and where its links lead, or a word saying there is none. A go the
// loader cannot find fails the load, and a run that failed is never recorded, so the word only has to differ from
// every path.
func goOnPath(lookPath func(string) (string, error)) string {
	found, lookPathError := lookPath("go")
	if lookPathError != nil {
		return "not found"
	}
	absolute, absoluteError := filepath.Abs(found)
	if absoluteError != nil {
		return "not absolute: " + found
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		return absolute + " (links unresolved)"
	}
	return absolute + " resolved " + resolved
}

// declareGoRunCacheInputs declares a finished Go run's inputs to the run cache, when its exit is a verdict a later
// run may replay, and otherwise leaves it unrecorded. A run with no session (not admitted, `--no-cache`, a listing)
// has nothing to declare.
//
// A run whose engine wrote no inputs record is never declared, so never recorded, the way a TypeScript run that
// never built its graph is not: with no inputs there is nothing to prove a later tree unchanged against. What is
// declared is the engine's files, the directories it listed as extra directories, its absent paths as absent, and
// the settings file this front door handed it, present or absent, which the engine names in no record of its own.
// The directory holding a file is recorded only within the project's root and the directories the engine listed,
// which lie in the roots it walked: a file outside them, the git index in a worktree's git directory among them, is
// decided by its own signature, since its directory moves with every lock file git writes there.
func declareGoRunCacheInputs(run *engineRun, exitCode int, settingsFile string) {
	session := activeRunCache
	if session == nil || run.inputs == nil {
		return
	}
	if reason := goRunUnrecordable(run, exitCode); reason != "" {
		declineRunCache(reason)
		return
	}
	if run.cgoTranslated {
		// Said, as below: a project with cgo of its own would otherwise never replay, with nothing to say why.
		reason := "the load translated cgo, whose C headers and compiler the record cannot name"
		declineRunCache(reason)
		session.note("the run cache did not record this run: " + reason)
		return
	}
	if reason := goInputsUnreplayable(run.inputs, session.projectRoots); reason != "" {
		// Said, and kept for the next run, since a record like this would never replay and nothing else would say
		// why: it is the engine's record that is wrong, and only a cold run every time shows it.
		declineRunCache(reason)
		session.note("the run cache did not record this run: " + reason)
		return
	}
	declareRunCacheInputs(run.inputs.Files...)
	session.extraDirectories = append(session.extraDirectories, run.inputs.Directories...)
	session.recordedWithin = slices.Concat(session.projectRoots, run.inputs.Directories)
	session.extraAbsent = append(session.extraAbsent, run.inputs.Absent...)
	if _, statError := os.Stat(settingsFile); statError == nil {
		session.extraFiles = append(session.extraFiles, settingsFile)
	} else {
		session.extraAbsent = append(session.extraAbsent, settingsFile)
	}
}

// goInputsUnreplayable is why an inputs record could never be replayed, empty when it can: a path inside a root's
// cache directory, which this run writes after its record is taken (the table, and any cache of the engine's kept
// beside it), or a listed directory above a root, whose listing moves with everything beside the project. Each
// would make every later run a miss, so the record is refused rather than kept, and the engine's record is what
// needs mending.
func goInputsUnreplayable(inputs *engineInputsRecord, roots []string) string {
	for _, paths := range [][]string{inputs.Files, inputs.Directories, inputs.Absent} {
		for _, path := range paths {
			for _, root := range roots {
				if pathWithin(path, cacheDirectory(root)) {
					return fmt.Sprintf("the Go engine named %s, inside the cache directory the run writes, so "+
						"no later run could replay it", path)
				}
			}
		}
	}
	for _, directory := range inputs.Directories {
		for _, root := range roots {
			if directory != root && pathWithin(root, directory) {
				return fmt.Sprintf("the Go engine listed %s, a directory above the root %s whose listing moves "+
					"with everything beside the project, so no later run could replay it", directory, root)
			}
		}
	}
	return ""
}

// pathWithin reports whether path is directory or below it.
func pathWithin(path string, directory string) bool {
	relative, relativeError := filepath.Rel(directory, path)
	return relativeError == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// goRunUnrecordable is why a finished Go run's exit is no verdict to replay, empty when it is one.
//
// A verdict is exit 0 or 1 from a run that called itself complete. acceptSummary refuses a complete summary whose
// own records show a requested phase that did not run, a crash, a file with no compiler record or a file it could
// not read, so complete is every phase ran and nothing went unchecked, held to the records rather than taken on
// the engine's word. And a run whose fix phase rewrote a file is declined, as a TypeScript run that rewrote one
// is: its verdict describes the tree it read, and the record would sign the tree it wrote.
func goRunUnrecordable(run *engineRun, exitCode int) string {
	switch {
	case exitCode != 0 && exitCode != 1:
		return fmt.Sprintf("the run exited %d, which is no verdict about the tree", exitCode)
	case run.summary == nil || run.summary.Complete == nil:
		return "the run ended without a summary"
	case !*run.summary.Complete:
		return "the run did not check everything, and a run that fell short is checked again rather than replayed"
	case run.filesRewritten > 0:
		return fmt.Sprintf("the fix phase rewrote %s, so the tree it read is not the tree on disk",
			counted(run.filesRewritten, "file", "files"))
	}
	return ""
}
