package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/types/program"
)

// The run cache for a Swift root (#nkk91tv). A Swift run uses the TypeScript run cache whole, as a Go run does (see
// go_run_cache.go): the table in .cache/cohere, the invocations it admits (runCacheEligible), the key, the clock, the
// tee, the replay and the record. Its inputs come the way a Go run's do. The engine names what its verdict was read
// from in its inputs record (swift/Contract.md; RunInputs.swift says what it names and why), and the front door adds
// what it knows itself: the settings file it hands the engine, the engine binary it ran, the `swift` its own toolchain
// question went to, an engine built from this checkout's working tree's sources, and the facts in swiftRunCacheFacts.
//
// The engine declares rather than the front door walking for itself because only the engine knows the walk. Which
// files a target compiles is SwiftPM's answer, which the front door could learn only by running SwiftPM, the cost a
// replay exists to skip; which files are ours is decided by the engine's own port of git's ignore rules; and an Xcode
// root's targets and linked packages come from its project file and the last build's package state. A second walk
// here would be a second copy of all three to keep in step, and the copy that drifted would replay stale verdicts.
//
// The replay is decided before the engine is resolved, so a run with nothing changed hashes no engine source, asks no
// toolchain anything and starts no process: one stat of each recorded input. The one exception is an engine built from
// the working tree, whose key takes the tree's clean or dirty state from git (see swiftRunCacheFacts).

// beginSwiftRunCache replays a recorded Swift run and exits when every input is unchanged, and otherwise starts
// recording this one. It does nothing for a run the run cache does not admit, `--no-cache` among them.
func beginSwiftRunCache(location projectLocation) {
	if !runCacheEligible() {
		return
	}
	anchor := program.NewPathAnchor(location.Root)
	facts := swiftRunCacheFacts(anchor, location.LintConfigFileName, activeOutput.Style.color,
		activeProjectYieldFingerprint(), swiftEngineChoice(os.Getenv(dispatch.SwiftEngineOverrideVariable),
			release.Current()), workingTreeModified, os.Environ(), exec.LookPath, os.Readlink)
	// No section beyond the run's own: the findings, shapes and types sections serve a TypeScript graph's walk.
	session := beginRunCacheSession(location.Root, anchor, program.CacheTableSections{}, facts...)
	if session != nil {
		session.projectRoots = []string{anchor.Root(), anchor.Resolved()}
	}
}

// swiftKeyedVariables are the variables a Swift run's key takes by name, beside every one whose name starts with
// `SWIFT` or ends in `_DEPLOYMENT_TARGET`. See swiftRunCacheFacts for why each is there.
var swiftKeyedVariables = []string{"DEVELOPER_DIR", "TOOLCHAINS", "SDKROOT", "HOME", "XDG_CONFIG_HOME", "CC", "CXX"}

// xcodeSelectLink is the link `xcode-select --switch` points at the active developer directory, which xcrun follows
// when no variable names one.
const xcodeSelectLink = "/var/db/xcode_select_link"

// swiftEngineFromWorkingTree is the choice of an engine built from the working tree's sources, the one choice whose
// binary is decided by files the front door can name.
const swiftEngineFromWorkingTree = "built from the working tree's sources"

// swiftRunCacheFacts is what a Swift run's verdict depends on that is no file the engine names, each a fact in the
// run's key. Computed with no subprocess, since a replay pays for them and `swift --version` alone costs more than the
// whole replay may.
//
// The engine's name, so a root that holds another project kind never replays one engine's run as the other's. The
// root resolved and the settings file, as a TypeScript run keys them. Whether the output is colored, since a recording
// made for a terminal carries escape codes a replay into a pipe must not print. What the run leaves to the Swift
// projects below it, which arrives by a variable the arguments do not show (see ownership.go). How the engine was
// chosen (swiftEngineChoice), so a run of a named engine never replays one built from sources, nor one built from a
// commit one built from the working tree.
//
// For an engine built from the working tree, also whether the engine's own inputs there have uncommitted changes, the
// state the engine it would be served is built and stamped with (dispatch.SwiftEngineTreeModified). That engine
// reports it as `sourceTreeModified`, the run's modified-build gap, and the declared sources record their bytes but
// not whether git calls them changed, so a replay would otherwise show the state of whichever engine wrote the record
// (#s5z3n30). It is the one fact a subprocess answers,
// about 14 ms of git on cohere's checkout, and it is asked only for that choice: a named, shipped or committed
// engine's bit is fixed in its bytes, and its binary is an input.
//
// Then which toolchain answers, which a replay never asks. The `swift`, `xcrun`, `xcodebuild` and `xcodegen` PATH
// finds, absolute and with their links resolved, since the engine runs each by name through `/usr/bin/env`. The
// developer directory `xcode-select` points xcrun at. `DEVELOPER_DIR` and `TOOLCHAINS`, which point it elsewhere, and
// `SDKROOT`. `HOME` and `XDG_CONFIG_HOME`, where git's global settings and excludes file are found, which decide what
// is ours. `CC` and `CXX`, which a package's C targets compile with. And every variable whose name starts with
// `SWIFT` or ends in `_DEPLOYMENT_TARGET`, by name and value, sorted: SwiftPM and the compiler read those, and one
// nobody listed here is still covered. The engine names the toolchain's own files (RunInputs.swift), so a new Xcode in
// the same place is a miss too.
func swiftRunCacheFacts(
	anchor program.PathAnchor,
	settingsFile string,
	color bool,
	yield string,
	engineChoice string,
	treeModified func() bool,
	environment []string,
	lookPath func(string) (string, error),
	readLink func(string) (string, error),
) []string {
	facts := []string{
		"engine=Swift",
		"root=" + anchor.Resolved(),
		"lint-config=" + anchor.Stable(settingsFile),
		fmt.Sprintf("color=%t", color),
		"yield=" + yield,
		"swift-engine=" + engineChoice,
	}
	if engineChoice == swiftEngineFromWorkingTree {
		facts = append(facts, fmt.Sprintf("swift-engine-tree-modified=%t", treeModified()))
	}
	for _, tool := range []string{"swift", "xcrun", "xcodebuild", "xcodegen"} {
		facts = append(facts, tool+"="+toolOnPath(tool, lookPath))
	}
	selected, readLinkError := readLink(xcodeSelectLink)
	if readLinkError != nil {
		selected = "no link"
	}
	facts = append(facts, "xcode-select="+selected)
	variables := []string{}
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		if slices.Contains(swiftKeyedVariables, name) || strings.HasPrefix(name, "SWIFT") ||
			strings.HasSuffix(name, "_DEPLOYMENT_TARGET") {
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

// workingTreeModified is whether the checkout an engine built from the working tree comes from has uncommitted changes,
// the state that engine is built and stamped with. A checkout that can't be found reads as modified, as a git that
// can't answer does in dispatch.SwiftEngineTreeModified: nothing then vouches for the tree.
func workingTreeModified() bool {
	moduleDirectory, findModuleDirectoryError := dispatch.FindModuleDirectory()
	if findModuleDirectoryError != nil {
		return true
	}
	return dispatch.SwiftEngineTreeModified(moduleDirectory)
}

// swiftEngineChoice is how this run will choose its engine, in the order resolveSwiftEngineBinary chooses, read with
// no hashing and no process: the binary the override names, the one a release ships beside cohere, one built from the
// commit this cohere was built from, or one built from the working tree's sources. Which binary that is, is declared
// once the run has it (declareSwiftRunCacheInputs); the choice is keyed so a replay never crosses from one to another.
func swiftEngineChoice(override string, provenance release.Provenance) string {
	switch {
	case override != "":
		return "named " + override
	case !provenance.IsDevelopment():
		return "shipped beside this cohere"
	case committedSwiftSource(provenance) != "":
		return "built from commit " + committedSwiftSource(provenance)
	}
	return swiftEngineFromWorkingTree
}

// declareSwiftRunCacheInputs declares a finished Swift run's inputs to the run cache, as declareEngineRunCacheInputs
// does for every engine, with what only the front door knows. The engine binary it ran, whose bytes are the rules. The
// `swift` PATH finds, which answered this front door's own `swift --version`. And for an engine built from the working
// tree's sources (engineSourceCommit empty, no override, a development cohere), those sources: an edit to one is a new
// engine, built on the next run that resolves it, and a replay would otherwise repeat the old engine's verdict.
//
// No directory holding a file is recorded beyond those the engine listed: it lists every directory whose listing it
// read, so a file outside them, a git config in `.git` or a toolchain's binary, is decided by its own signature, and
// its directory, which moves with every lock file git writes or every tool installed beside it, is no input.
func declareSwiftRunCacheInputs(run *engineRun, exitCode int, settingsFile string, binaryPath string, choice string) {
	if !declareEngineRunCacheInputs(run, exitCode, settingsFile, "") {
		return
	}
	session := activeRunCache
	session.recordedWithin = []string{}
	declareFileOrAbsent(binaryPath)
	if swift, lookPathError := exec.LookPath("swift"); lookPathError == nil {
		if absolute, absoluteError := filepath.Abs(swift); absoluteError == nil {
			declareFileOrAbsent(absolute)
		}
	}
	if choice == swiftEngineFromWorkingTree {
		if reason := declareSwiftEngineSources(); reason != "" {
			declineRunCache(reason)
		}
	}
}

// declareSwiftEngineSources declares what an engine built from the working tree is built from, as
// dispatch.ResolveSwiftEngine hashes it: the engine package's manifest and resolved file, and every file and directory
// under its `Sources`. Empty when declared, and otherwise why not.
func declareSwiftEngineSources() string {
	moduleDirectory, findModuleDirectoryError := dispatch.FindModuleDirectory()
	if findModuleDirectoryError != nil {
		return "the Swift engine's sources could not be found to declare: " + findModuleDirectoryError.Error()
	}
	engineDirectory := dispatch.DefaultPaths(moduleDirectory).SwiftEngineDirectory()
	declareFileOrAbsent(filepath.Join(engineDirectory, "Package.swift"))
	declareFileOrAbsent(filepath.Join(engineDirectory, "Package.resolved"))
	session := activeRunCache
	walkError := filepath.WalkDir(
		filepath.Join(engineDirectory, "Sources"),
		func(path string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			if entry.IsDir() {
				session.extraDirectories = append(session.extraDirectories, path)
			} else {
				session.extraFiles = append(session.extraFiles, path)
			}
			return nil
		},
	)
	if walkError != nil {
		return "the Swift engine's sources could not be listed to declare: " + walkError.Error()
	}
	return ""
}
