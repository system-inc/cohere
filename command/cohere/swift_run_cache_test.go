package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/types/program"
)

// TestSwiftRunCacheFactsMoveWithTheToolchainAndTheEngine: every fact a Swift run depends on that is no file moves the
// run's key, and a variable no Swift run reads, or the same environment in another order, leaves it where it was. A
// key that stayed put under a moved fact would replay a verdict another toolchain or engine gave: which `swift`,
// xcrun and SDK answer decides what the compiler says, and how the engine is chosen decides which rules ran.
func TestSwiftRunCacheFactsMoveWithTheToolchainAndTheEngine(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	anchor := program.NewPathAnchor(root)
	settingsFile := filepath.Join(root, "CohereSettings.json")
	toolchains := test.TempDir()
	tool := func(name string) string {
		path := filepath.Join(toolchains, name)
		if writeError := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); writeError != nil {
			test.Fatal(writeError)
		}
		return path
	}
	firstSwift, secondSwift := tool("first-swift"), tool("second-swift")
	found := func(path string) func(string) (string, error) {
		return func(string) (string, error) { return path, nil }
	}
	selected := func(developer string) func(string) (string, error) {
		return func(string) (string, error) { return developer, nil }
	}
	const firstXcode = "/Applications/Xcode.app/Contents/Developer"
	base := []string{"PATH=/usr/bin", "HOME=/home/someone", "EDITOR=vi", "MACOSX_DEPLOYMENT_TARGET=15.0"}
	type facts struct {
		environment []string
		lookPath    func(string) (string, error)
		readLink    func(string) (string, error)
		yield       string
		choice      string
		modified    bool
	}
	key := func(given facts) string {
		test.Helper()
		treeModified := func() bool { return given.modified }
		parts, partsError := program.RunCacheKeyPartsOf(nil, anchor.Resolved(), swiftRunCacheFacts(anchor,
			settingsFile, false, given.yield, given.choice, treeModified, given.environment, given.lookPath,
			given.readLink)...)
		if partsError != nil {
			test.Fatal(partsError)
		}
		return parts.Key()
	}
	baseline := facts{base, found(firstSwift), selected(firstXcode), "", swiftEngineFromWorkingTree, false}
	with := func(change func(*facts)) facts {
		changed := baseline
		changed.environment = slices.Clone(base)
		change(&changed)
		return changed
	}
	replaced := func(name string, value string) []string {
		environment := []string{}
		for _, variable := range base {
			if !strings.HasPrefix(variable, name+"=") {
				environment = append(environment, variable)
			}
		}
		return append(environment, name+"="+value)
	}

	moves := []struct {
		name  string
		facts facts
	}{
		{"another swift found on PATH", with(func(given *facts) { given.lookPath = found(secondSwift) })},
		{"no swift found on PATH", with(func(given *facts) {
			given.lookPath = func(string) (string, error) { return "", os.ErrNotExist }
		})},
		{"xcode-select pointed at another Xcode", with(func(given *facts) {
			given.readLink = selected("/Applications/Xcode-beta.app/Contents/Developer")
		})},
		{"DEVELOPER_DIR set", with(func(given *facts) {
			given.environment = append(given.environment, "DEVELOPER_DIR=/Applications/Xcode-beta.app")
		})},
		{"TOOLCHAINS set", with(func(given *facts) {
			given.environment = append(given.environment, "TOOLCHAINS=swift")
		})},
		{"SDKROOT set", with(func(given *facts) { given.environment = append(given.environment, "SDKROOT=iphoneos") })},
		{"a deployment target changed", with(func(given *facts) {
			given.environment = replaced("MACOSX_DEPLOYMENT_TARGET", "26.0")
		})},
		{"a SwiftPM variable set", with(func(given *facts) {
			given.environment = append(given.environment, "SWIFTPM_ENABLE_PLUGINS=0")
		})},
		{"HOME changed", with(func(given *facts) { given.environment = replaced("HOME", "/home/another") })},
		{"CC set", with(func(given *facts) { given.environment = append(given.environment, "CC=clang") })},
		{"the run now yields a project below it", with(func(given *facts) { given.yield = "a1b2" })},
		{"an engine named by the override", with(func(given *facts) { given.choice = "named /tmp/cohere-swift" })},
		{"an engine built from a commit", with(func(given *facts) { given.choice = "built from commit 0123abcd" })},
		{"a released engine", with(func(given *facts) { given.choice = "shipped beside this cohere" })},
		// The engine built from the working tree is built and stamped with its state, and reports it as the run's
		// modified-build gap, so a replay across the state would show another engine's (#s5z3n30).
		{"the working tree now has uncommitted changes", with(func(given *facts) { given.modified = true })},
	}
	for _, moved := range moves {
		if key(moved.facts) == key(baseline) {
			test.Errorf("%s and the key stayed put", moved.name)
		}
	}

	reordered := slices.Clone(base)
	slices.Reverse(reordered)
	stays := []struct {
		name  string
		facts facts
	}{
		{"a variable no Swift run reads changed", with(func(given *facts) {
			given.environment = replaced("EDITOR", "nano")
		})},
		{"PATH changed and finds the same tools", with(func(given *facts) {
			given.environment = replaced("PATH", "/opt/bin:/usr/bin")
		})},
		{"the same environment in another order", with(func(given *facts) { given.environment = reordered })},
	}
	for _, stayed := range stays {
		if key(stayed.facts) != key(baseline) {
			test.Errorf("%s and the key moved, so an unchanged run would never replay", stayed.name)
		}
	}
}

// TestOnlyAnEngineBuiltFromTheWorkingTreeAsksTheTreesState: the tree's clean or dirty state is a fact only for an
// engine built from the working tree, whose bit follows it (#s5z3n30). A named, shipped or committed engine's bit is
// fixed in its bytes, and its binary is an input, so the state is never asked for one, since asking is a git process
// a replay would pay for nothing.
func TestOnlyAnEngineBuiltFromTheWorkingTreeAsksTheTreesState(test *testing.T) {
	test.Parallel()
	anchor := program.NewPathAnchor(test.TempDir())
	none := func(string) (string, error) { return "", os.ErrNotExist }
	others := []string{"named /tmp/cohere-swift", "shipped beside this cohere", "built from commit 0123abcd"}
	for _, choice := range others {
		asked := false
		swiftRunCacheFacts(anchor, "", false, "", choice, func() bool { asked = true; return true }, nil, none, none)
		if asked {
			test.Errorf("an engine %s asked for the tree's state, which its bit does not follow", choice)
		}
	}
	asked := false
	swiftRunCacheFacts(anchor, "", false, "", swiftEngineFromWorkingTree, func() bool { asked = true; return false },
		nil, none, none)
	if !asked {
		test.Error("an engine built from the working tree never asked for the tree's state, which its bit follows")
	}
}

// TestSwiftEngineChoiceFollowsTheResolvesOrder: the choice keyed is the one resolveSwiftEngineBinary makes, the
// override first, then a release's own engine, then a clean commit's, then the working tree's, so a replay never
// crosses from an engine chosen one way to one chosen another.
func TestSwiftEngineChoiceFollowsTheResolvesOrder(test *testing.T) {
	test.Parallel()
	released := release.Provenance{Version: "1.2.3", SelfCommit: "0123456789abcdef0123456789abcdef01234567"}
	clean := release.Provenance{Version: "dev", SelfCommit: "0123456789abcdef0123456789abcdef01234567"}
	modified := release.Provenance{Version: "dev", SelfCommit: clean.SelfCommit, SourceTreeModified: true}
	cases := []struct {
		name       string
		override   string
		provenance release.Provenance
		want       string
	}{
		{"an override wins over a release", "/tmp/engine", released, "named /tmp/engine"},
		{"a release runs its own", "", released, "shipped beside this cohere"},
		{"a clean commit builds from it", "", clean, "built from commit " + clean.SelfCommit},
		{"a modified tree builds from the working tree", "", modified, swiftEngineFromWorkingTree},
	}
	for _, testCase := range cases {
		if got := swiftEngineChoice(testCase.override, testCase.provenance); got != testCase.want {
			test.Errorf("%s: the choice is %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// TestASwiftRecordKeepsNoHoldingDirectoryItDidNotList: a Swift run names its git config, which lives in `.git`, a
// directory every git command writes a lock file into, and the toolchain's files, whose directories move whenever a
// tool is installed beside them. Recorded, either directory would make nearly every later run a miss that nothing
// says. The engine lists every directory whose listing it read, so only those are recorded, and its files outside
// them are decided by their own signatures. And the declaration adds the engine binary the run ran, which is what
// makes a rebuilt engine a miss.
// Not parallel: it sets the package-level activeRunCache for its length.
func TestASwiftRecordKeepsNoHoldingDirectoryItDidNotList(test *testing.T) {
	previous := activeRunCache
	defer func() { activeRunCache = previous }()
	root := test.TempDir()
	write := func(name string) string {
		path := filepath.Join(root, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(name), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		return path
	}
	source, configuration := write("Sources/App/App.swift"), write(".git/config")
	engine := write("engine/cohere-swift")
	complete, exitCode, findings := true, 0, 0
	run := &engineRun{
		language: swiftLanguage,
		summary:  &engineSummaryRecord{Findings: &findings, Complete: &complete, ExitCode: &exitCode},
		inputs: &engineInputsRecord{
			Files:       []string{source, configuration},
			Directories: []string{root, filepath.Join(root, "Sources"), filepath.Join(root, "Sources", "App")},
		},
	}
	activeRunCache = &runCacheSession{
		key:          "key",
		anchor:       program.NewPathAnchor(root),
		recorder:     program.NewInputRecorder(),
		projectRoots: []string{root},
		summary:      []byte("{}"),
	}
	declareSwiftRunCacheInputs(run, 0, filepath.Join(root, "CohereSettings.json"), engine, "named "+engine)
	session := activeRunCache
	if !session.declared || session.declined != "" {
		test.Fatalf("a verdict with inputs was not declared (declined %q)", session.declined)
	}
	recorded := session.record(0)
	if recorded == nil {
		test.Fatal("the run was not recorded")
	}
	inputs := map[string]program.RunCacheInput{}
	for _, input := range recorded.Inputs {
		inputs[input.Path] = input
	}
	for _, kept := range []string{source, configuration, engine, root, filepath.Join(root, "Sources", "App")} {
		if _, found := inputs[kept]; !found {
			test.Errorf("the record left out %s", kept)
		}
	}
	for _, left := range []string{filepath.Join(root, ".git"), filepath.Join(root, "engine")} {
		if _, found := inputs[left]; found {
			test.Errorf("the record kept %s, a directory the engine never listed", left)
		}
	}
	if absent, found := inputs[filepath.Join(root, "CohereSettings.json")]; !found || absent.Exists {
		test.Error("the settings file the front door handed the engine was not recorded absent")
	}
}

// TestASwiftRecordMayNameItsScratchsPinsAndNothingElseOfTheCache: an Xcode root's build starts from pins only the
// engine's scratch holds, so a Swift record may name files there, and is not refused for it. Anything else in the cache
// directory, the table the front door writes after every run above all, or a directory in the scratch, whose listing
// the engine's own builds move, still refuses the record, as it does for every engine.
func TestASwiftRecordMayNameItsScratchsPinsAndNothingElseOfTheCache(test *testing.T) {
	test.Parallel()
	root := "/work/App"
	roots := []string{root}
	pins := filepath.Join(cacheDirectory(root), "swift", "xcode-Package.resolved")
	scratch := swiftLanguage.NamedScratch
	inputs := func(files []string, directories []string) *engineInputsRecord {
		return &engineInputsRecord{Files: files, Directories: directories}
	}
	if reason := engineInputsUnreplayable("the Swift engine", inputs([]string{pins}, []string{root}), roots,
		scratch); reason != "" {
		test.Errorf("a record naming the scratch's pins was refused: %s", reason)
	}
	refused := []struct {
		name   string
		inputs *engineInputsRecord
	}{
		{"the table", inputs([]string{filepath.Join(cacheDirectory(root), "run-0123.gob")}, []string{root})},
		{"a directory in the scratch", inputs([]string{pins}, []string{root, filepath.Dir(pins)})},
	}
	for _, refusal := range refused {
		if engineInputsUnreplayable("the Swift engine", refusal.inputs, roots, scratch) == "" {
			test.Errorf("a record naming %s was kept", refusal.name)
		}
	}
	if engineInputsUnreplayable("the Go engine", inputs([]string{pins}, []string{root}), roots, "") == "" {
		test.Error("an engine that names no scratch was let name one")
	}
}
