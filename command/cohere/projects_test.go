//go:build unix

// Every project here is checked through a stand-in Swift engine, a /bin/sh script, so the file builds on
// Unix, as swift_engine_test.go does (#tejf9bc).

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// mixedRepository writes a repository holding a TypeScript program at its root and a Swift package below
// it, the shape a run from the root checks both of (#f9nftxz).
//
// Its files are in the house format, since a run formats them and the Swift engine refuses --no-format
// (#nqb3mjv), so these runs check formatting too.
func mixedRepository(test *testing.T, swiftDirectory string) string {
	test.Helper()
	root := newRepository(test, map[string]string{
		"tsconfig.json":       incrementalFixtureConfiguration + "\n",
		"CohereSettings.json": "{ \"rules\": {} }\n",
		"index.ts":            "export const value = 1;\n",
		".gitignore":          "tsconfig.tsbuildinfo\n",
	})
	writeTree(test, filepath.Join(root, swiftDirectory), map[string]string{
		"Package.swift":         discoveryPackage,
		"Sources/Toy/Toy.swift": "let toy = 1\n",
	})
	return root
}

// outsideAnyRepository removes a fixture's `.git`, so a test can hold discovery to what it does inside one: the
// Swift no project claims is a scripts-only root's either way, since the engine walks by the tree's own ignore files
// outside a repository (#13nbb1s).
func outsideAnyRepository(test *testing.T, root string) string {
	test.Helper()
	if removeError := os.Remove(filepath.Join(root, ".git")); removeError != nil {
		test.Fatal(removeError)
	}
	return root
}

// A mixed repository is checked whole from its root: each project in its own section under its own
// engine, one line per project, and one summary. The run is green only when both are, a Swift finding
// turns it red, and its exit code is the one the failing project's own run gives.
func TestAMixedRepositoryIsCheckedWholeAndTheWorstExitWins(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	clean := fakeSwiftEngine(test, "cat "+filepath.Join(contractDirectory(test), "Clean.jsonl"))
	findings := fakeSwiftEngine(test, "cat "+filepath.Join(contractDirectory(test), "Findings.jsonl")+"\nexit 1")

	root := mixedRepository(test, "apple/Toy")
	output, exitCode := runWithEngine(test, binary, clean, root, "--no-fix")
	if exitCode != 0 {
		test.Fatalf("a clean mixed repository exited %d:\n%s", exitCode, output)
	}
	for _, want := range []string{
		"== . (TypeScript) ==",
		"== apple/Toy (Swift) ==",
		"  . (TypeScript): green",
		"  apple/Toy (Swift): green",
		"projects: 2 checked (1 TypeScript, 1 Swift), every one green",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the report lacks %q:\n%s", want, output)
		}
	}

	output, exitCode = runWithEngine(test, binary, findings, root, "--no-fix")
	_, alone := runWithEngine(
		test,
		binary,
		findings,
		root,
		"--no-fix",
		"--directory",
		filepath.Join(root, "apple", "Toy"),
	)
	if exitCode == 0 || exitCode != alone {
		test.Fatalf(
			"a Swift finding exited %d from the root and %d from the package alone, want the same nonzero code:\n%s",
			exitCode,
			alone,
			output,
		)
	}
	if !strings.Contains(output, "apple/Toy (Swift): failed, exit") || !strings.Contains(output, "1 failed") {
		test.Errorf("the report does not name the failing project:\n%s", output)
	}
	if !strings.Contains(output, "  . (TypeScript): green") {
		test.Errorf("the TypeScript project's own verdict is lost:\n%s", output)
	}
}

// Both markers in one directory are two projects, each checked by its own engine; and projects nested two
// levels down are found from the root.
func TestBothMarkersInOneDirectoryAndProjectsNestedDeepAreEachChecked(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	clean := fakeSwiftEngine(test, "cat "+filepath.Join(contractDirectory(test), "Clean.jsonl"))

	together := mixedRepository(test, ".")
	output, exitCode := runWithEngine(test, binary, clean, together, "--no-fix")
	if exitCode != 0 ||
		!strings.Contains(output, "== . (TypeScript) ==") ||
		!strings.Contains(output, "== . (Swift) ==") {
		test.Fatalf("a directory holding both markers was not checked by both engines (exit %d):\n%s", exitCode, output)
	}

	deep := newRepository(test, nil)
	writeTree(test, filepath.Join(deep, "web", "app"), map[string]string{
		"tsconfig.json":       incrementalFixtureConfiguration + "\n",
		"CohereSettings.json": "{ \"rules\": {} }\n",
		"index.ts":            "export const value = 1;\n",
	})
	writeTree(
		test,
		filepath.Join(deep, "apple", "Toy"),
		map[string]string{"Package.swift": discoveryPackage, "Sources/Toy/Toy.swift": "let toy = 1\n"},
	)
	output, exitCode = runWithEngine(test, binary, clean, deep, "--no-fix")
	if exitCode != 0 ||
		!strings.Contains(output, "== apple/Toy (Swift) ==") ||
		!strings.Contains(output, "== web/app (TypeScript) ==") {
		test.Fatalf("projects two levels down were not both checked (exit %d):\n%s", exitCode, output)
	}
}

// A project whose run fails without a verdict, here an engine that dies before reporting, fails the run
// from the root; and a root holding no project at all fails loudly rather than passing over nothing.
func TestACrashedProjectOrNoProjectFailsTheRun(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	crashed := fakeSwiftEngine(test, "exit 2")
	root := mixedRepository(test, "apple/Toy")
	output, exitCode := runWithEngine(test, binary, crashed, root, "--no-fix")
	if exitCode == 0 || !strings.Contains(output, "apple/Toy (Swift): failed") {
		test.Fatalf("a Swift engine that died passed the run (exit %d):\n%s", exitCode, output)
	}

	empty := newRepository(test, map[string]string{"README.md": "nothing to check\n"})
	output, exitCode = runWithEngine(test, binary, crashed, empty)
	if exitCode == 0 || !strings.Contains(output, "no project here to check") {
		test.Fatalf("a repository with no project passed (exit %d):\n%s", exitCode, output)
	}
}

// Under --format-only a Swift package runs in the engine's own format-only mode (#nqb3mjv, c024df4b): the
// engine gets --format-only, its types and lint skipped as not requested are no gap, and its format finding
// fails the run, beside a TypeScript program and in the package alone. Before, the engine had no such mode,
// so the package was skipped with its gap named, and the flag against it alone was refused.
func TestFormatOnlyRunsTheSwiftEngineInItsFormatOnlyMode(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	arguments := filepath.Join(test.TempDir(), "arguments")
	engine := fakeSwiftEngine(
		test,
		"echo \"$@\" >> '"+arguments+"'\ncat "+filepath.Join(contractDirectory(test), "FormatOnly.jsonl")+"\nexit 1",
	)
	root := mixedRepository(test, "apple/Toy")

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix", "--format-only")
	if exitCode == 0 ||
		!strings.Contains(output, "apple/Toy (Swift): failed, exit") ||
		!strings.Contains(output, "  . (TypeScript): green") ||
		!strings.Contains(output, "projects: 2 checked (1 TypeScript, 1 Swift), 1 failed") {
		test.Errorf(
			"beside a TypeScript program, the Swift format finding did not fail the run (exit %d):\n%s",
			exitCode,
			output,
		)
	}
	if strings.Contains(output, "not requested") && strings.Contains(output, "did not run") {
		test.Errorf("the run counted types or lint skipped as not requested as a gap:\n%s", output)
	}

	output, alone := runWithEngine(
		test,
		binary,
		engine,
		root,
		"--no-fix",
		"--format-only",
		"--directory",
		filepath.Join(root, "apple", "Toy"),
	)
	if alone == 0 ||
		alone != exitCode ||
		!strings.Contains(output, "notFormatted") && !strings.Contains(output, "Doubler.swift") {
		test.Errorf(
			"the package alone did not fail on its format finding as the mixed run did (exit %d, mixed %d):\n%s",
			alone,
			exitCode,
			output,
		)
	}
	logged, readError := os.ReadFile(arguments)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		if !strings.Contains(" "+line+" ", " --no-fix ") || !strings.Contains(" "+line+" ", " --format-only ") {
			test.Errorf("the engine was run without --no-fix --format-only: %q", line)
		}
	}
	if count := len(strings.Split(strings.TrimSpace(string(logged)), "\n")); count != 2 {
		test.Errorf("the engine ran %d times, want twice:\n%s", count, logged)
	}
}

// coveringRepository writes a repository whose root package has three below it: Libraries/Lib, a path
// dependency the root's run checks; Vendor/Third, a vendored one it lists as not checked; and Tools/Sim,
// which nothing depends on, the shape of ahraos-presence's VRMKit and RigSimulator (#sxs26s3).
func coveringRepository(test *testing.T) string {
	test.Helper()
	root := newRepository(test, nil)
	for _, directory := range []string{".", "Libraries/Lib", "Vendor/Third", "Tools/Sim"} {
		writeTree(test, filepath.Join(root, filepath.FromSlash(directory)), map[string]string{
			"Package.swift":         discoveryPackage,
			"Sources/Toy/Toy.swift": "let toy = 1\n",
		})
	}
	return root
}

// coveringSwiftEngine is a stand-in engine for coveringRepository. It logs every command line to the file
// it returns, answers `--covered-packages` with coverage, and otherwise runs check: both shell, with the
// `--root` it was given in $root. A check run reports Lib's findings from Lib's own run and from the root's,
// which checks Lib too, so a test can count how often they appear.
func coveringSwiftEngine(test *testing.T, coverage string) (string, string) {
	test.Helper()
	log := filepath.Join(test.TempDir(), "arguments")
	findings := filepath.Join(contractDirectory(test), "Findings.jsonl")
	engine := fakeSwiftEngine(test, "echo \"$@\" >> '"+log+"'\n"+
		"root=''; covering=''; previous=''\n"+
		"for argument in \"$@\"; do\n"+
		"  if [ \"$previous\" = --root ]; then root=$argument; fi\n"+
		"  if [ \"$argument\" = --covered-packages ]; then covering=1; fi\n"+
		"  previous=$argument\n"+
		"done\n"+
		"if [ -n \"$covering\" ]; then\n"+coverage+"\nfi\n"+
		"case \"$root\" in */Libraries/Lib) cat '"+findings+"'; exit 1;; esac\n"+
		"if [ -f \"$root/Libraries/Lib/Package.swift\" ]; then cat '"+findings+"'; exit 1; fi\n"+
		"cat '"+filepath.Join(contractDirectory(test), "Clean.jsonl")+"'")
	return engine, log
}

// coverageAnswerThrough is the shell that answers `--covered-packages` the way the engine does for coveringRepository:
// each package checks itself, the root checks Lib as well and vendors Third, and Sim is in neither list.
// Paths are printed through alias, a symbolic link to the repository, so the front door must resolve links
// on both sides to match them, as it must for the engine's `/tmp` against its own `/private/tmp`.
func coverageAnswerThrough(repository string, alias string, provenance string) string {
	return "real=$(cd \"$root\" && pwd -P); base=$(cd '" +
		repository +
		"' && pwd -P); printed=\"" +
		alias +
		"${real#\"$base\"}\"\n" +
		"checked=\"\\\"$printed\\\"\"; vendored=''\n" +
		"if [ -f \"$root/Libraries/Lib/Package.swift\" ]; then " +
		"checked=\"$checked,\\\"$printed/Libraries/Lib\\\"\"; fi\n" +
		"if [ -f \"$root/Vendor/Third/Package.swift\" ]; then vendored=\"\\\"$printed/Vendor/Third\\\"\"; fi\n" +
		"head -1 '" +
		provenance +
		"'\n" +
		"echo \"{\\\"kind\\\":\\\"coveredPackages\\\",\\\"root\\\":\\\"$printed\\\"," +
		"\\\"checked\\\":[$checked],\\\"vendored\\\":[$vendored]}\"\n" +
		"exit 0"
}

// A package the root's run checks, or vendors, is not run again as its own project, and says why; its
// findings appear once and its run is not counted. A package below the root that nothing depends on is no
// part of the root's run, so it runs, whatever its nesting on disk says (#sxs26s3).
func TestASwiftPackageAnotherRunCoversIsNotCheckedTwice(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := coveringRepository(test)
	alias := filepath.Join(test.TempDir(), "alias")
	if symlinkError := os.Symlink(root, alias); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	engine, log := coveringSwiftEngine(
		test,
		coverageAnswerThrough(root, alias, filepath.Join(contractDirectory(test), "Clean.jsonl")),
	)

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode == 0 {
		test.Errorf("the root's findings, Lib's among them, did not fail the run:\n%s", output)
	}
	for _, want := range []string{
		"== . (Swift) ==",
		"== Tools/Sim (Swift) ==",
		"Libraries/Lib not run: checked as part of .",
		"Vendor/Third not run: vendored in ., whose run lists it as not checked",
		"projects: 2 checked (2 Swift), 1 failed; not run: 2 Swift packages another package's run covers",
		"2 projects",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the report lacks %q:\n%s", want, output)
		}
	}
	for _, skipped := range []string{"== Libraries/Lib (Swift) ==", "== Vendor/Third (Swift) =="} {
		if strings.Contains(output, skipped) {
			test.Errorf("%s ran though the root's run covers it:\n%s", skipped, output)
		}
	}
	if count := strings.Count(output, "ByteRing.swift:159"); count != 1 {
		test.Errorf("Lib's finding appears %d times, want once:\n%s", count, output)
	}
	logged, readError := os.ReadFile(log)
	if readError != nil {
		test.Fatal(readError)
	}
	if count := strings.Count(string(logged), "--covered-packages"); count != 4 {
		test.Errorf("the engine was asked its coverage %d times, want once per package, 4:\n%s", count, logged)
	}
}

// A coverage query that fails derives nothing: every package runs, the dependency's findings counted twice,
// which is visible, rather than a package hidden, which is not, and the failure is named with the engine's
// reason.
func TestAFailedCoverageQueryRunsEveryPackageAndSaysSo(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := coveringRepository(test)
	engine, _ := coveringSwiftEngine(test, "echo 'cohere-swift: the manifest would not load' >&2; exit 2")

	output, _ := runWithEngine(test, binary, engine, root, "--no-fix")
	for _, want := range []string{
		"== . (Swift) ==",
		"== Libraries/Lib (Swift) ==",
		"== Tools/Sim (Swift) ==",
		"== Vendor/Third (Swift) ==",
		"projects: 4 checked (4 Swift)",
		".: the Swift coverage query failed, so every package it might check runs as its own project too: the engine exited 2: cohere-swift: the manifest would not load",
		// Said on the verdict line too, which every view prints, since notes are --verbose's alone.
		"⚠ 4 Swift coverage queries failed, so some files may be counted twice",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the report lacks %q:\n%s", want, output)
		}
	}
	if count := strings.Count(output, "the Swift coverage query failed"); count != 4 {
		test.Errorf("the failed query is named %d times, want once per package, 4:\n%s", count, output)
	}
	if strings.Contains(output, "not run: checked as part of") {
		test.Errorf("a package was skipped on a coverage nobody answered:\n%s", output)
	}
}

// One Swift package has nothing to share, so the engine is never asked its coverage, and the run is the one
// it was before.
func TestOneSwiftPackageAsksNoCoverage(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	engine, log := coveringSwiftEngine(test, "echo 'asked' >&2; exit 2")
	root := mixedRepository(test, "apple/Toy")

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode != 0 || !strings.Contains(output, "projects: 2 checked (1 TypeScript, 1 Swift), every one green") {
		test.Errorf("a repository with one Swift package did not run as it did (exit %d):\n%s", exitCode, output)
	}
	logged, readError := os.ReadFile(log)
	if readError != nil {
		test.Fatal(readError)
	}
	if strings.Contains(string(logged), "--covered-packages") || !strings.Contains(string(logged), "--root") {
		test.Errorf("want one check run and no coverage query, the engine was run with:\n%s", logged)
	}
}

// Swift files no package holds, here an app's App/ beside apple/Toy, the shape of ahraos-presence's
// CapturePhone, are a scripts-only root's, inside a repository or not (#wpd36zj, #13nbb1s): its Swift run stands at
// the root beside the TypeScript project there, the package runs as before, and a clean run stays green.
func TestSwiftNoPackageHoldsIsAScriptsOnlyRootsInOrOutOfARepository(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, inRepository := range []bool{true, false} {
		test.Run(fmt.Sprintf("in a repository: %t", inRepository), func(test *testing.T) {
			test.Parallel()
			root := mixedRepository(test, "apple/Toy")
			if !inRepository {
				outsideAnyRepository(test, root)
			}
			writeTree(test, filepath.Join(root, "App"), map[string]string{
				"Sources/App/App.swift":    "let app = 1\n",
				"Sources/App/Screen.swift": "let screen = 1\n",
				"Tests/AppTests.swift":     "let tests = 1\n",
			})
			engine, _ := coveringSwiftEngine(test,
				"echo \"{\\\"kind\\\":\\\"coveredPackages\\\",\\\"root\\\":\\\"$root\\\","+
					"\\\"checked\\\":[\\\"$root\\\"],\\\"vendored\\\":[]}\"\nexit 0")

			output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
			if exitCode != 0 {
				test.Errorf("a clean repository with an app's Swift exited %d, want 0:\n%s", exitCode, output)
			}
			for _, want := range []string{"== . (Swift) ==", "== apple/Toy (Swift) =="} {
				if !strings.Contains(output, want) {
					test.Errorf("the report lacks %q:\n%s", want, output)
				}
			}
		})
	}
}

// A package another package's run checks holds its files as surely as one that runs, since the covering run
// checks them: here A's run checks its sibling B, which is not run, and B's Swift is no gap.
func TestSwiftACoveredPackageHoldsIsNoGap(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, nil)
	for _, directory := range []string{"A", "B"} {
		writeTree(test, filepath.Join(root, directory), map[string]string{
			"Package.swift":         discoveryPackage,
			"Sources/Toy/Toy.swift": "let toy = 1\n",
		})
	}
	engine, _ := coveringSwiftEngine(test, "checked=\"\\\"$root\\\"\"\n"+
		"case \"$root\" in */A) checked=\"$checked,\\\"$root/../B\\\"\";; esac\n"+
		"echo \"{\\\"kind\\\":\\\"coveredPackages\\\",\\\"root\\\":\\\"$root\\\","+
		"\\\"checked\\\":[$checked],\\\"vendored\\\":[]}\"\n"+
		"exit 0")

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode != 0 || !strings.Contains(output, "B not run: checked as part of A") {
		test.Fatalf("A's run did not cover B, so this does not test a covered package (exit %d):\n%s", exitCode, output)
	}
	if strings.Contains(output, "== . (Swift) ==") {
		test.Errorf("a covered package's files were given a scripts-only root, as though no package held them:\n%s",
			output)
	}
}

// Swift the walk never enters, under node_modules, or that .gitignore or the root's ignorePatterns match, gives no
// scripts-only root, since nobody expects it checked, inside a repository or not: discovery reads .gitignore either
// way. App/, beside them, is the control: one file the walk keeps gives the root its Swift run (#wpd36zj).
func TestSwiftIgnoredOrInADependencyGivesNoScriptsOnlyRoot(test *testing.T) {
	test.Parallel()
	for _, inRepository := range []bool{true, false} {
		test.Run(fmt.Sprintf("in a repository: %t", inRepository), func(test *testing.T) {
			test.Parallel()
			root := mixedRepository(test, "apple/Toy")
			if !inRepository {
				outsideAnyRepository(test, root)
			}
			writeTree(test, root, map[string]string{
				".gitignore":                         "tsconfig.tsbuildinfo\nGenerated/\n*.generated.swift\n",
				"Generated/Sources/Generated.swift":  "let generated = 1\n",
				"node_modules/dependency/Lib.swift":  "let dependency = 1\n",
				"Scripts/Made.generated.swift":       "let made = 1\n",
				"Legacy/Old.swift":                   "let old = 1\n",
				"App/Sources/Screen.generated.swift": "let screen = 1\n",
			})
			scriptsRoot := func() bool {
				test.Helper()
				found, discoverProjectsError := discoverProjects(root, []string{"Legacy/**"})
				if discoverProjectsError != nil {
					test.Fatal(discoverProjectsError)
				}
				return slices.Contains(found.Projects, discoveredProject{Directory: ".", Engine: engineSwift})
			}
			if scriptsRoot() {
				test.Errorf("Swift nobody expects checked gave the root a scripts-only Swift run")
			}
			writeTree(test, root, map[string]string{"App/Sources/App.swift": "let app = 1\n"})
			if !scriptsRoot() {
				test.Errorf("the control, App/ with one file .gitignore keeps, gave the root no scripts-only Swift run")
			}
		})
	}
}

// A directory holding a project.yml, or exactly one .xcodeproj, and no Package.swift is an Xcode candidate, and
// it is a Swift project when it is the nearest Swift marker above some Swift file: inside a package's
// directory, as ahraos-presence's CapturePhone is, or outside every package, where its files are then no gap.
// A candidate claiming nothing is no project, Package.swift beside one is the package alone, two .xcodeproj or
// a workspace alone is no candidate, and nested markers each claim their nearest files (#ybspptb). What no marker
// claims is the scripts-only root's, a Swift project at `.` (#wpd36zj).
func TestAnXcodeProjectIsDiscoveredByTheNearestClaim(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name           string
		files          map[string]string
		ignorePatterns []string
		projects       []discoveredProject
		refused        []string
	}{
		{
			name: "a candidate inside a package's directory with Swift beneath is a project",
			files: map[string]string{
				"Package.swift":              discoveryPackage,
				"Sources/Toy/Toy.swift":      "let toy = 1\n",
				"CapturePhone/project.yml":   "name: CapturePhone\n",
				"CapturePhone/App/App.swift": "let app = 1\n",
			},
			projects: []discoveredProject{
				{Directory: ".", Engine: engineSwift},
				{Directory: "CapturePhone", Engine: engineSwift},
			},
		},
		{
			name: "a project.yml with no Swift beneath is not a project",
			files: map[string]string{
				"Package.swift":         discoveryPackage,
				"Sources/Toy/Toy.swift": "let toy = 1\n",
				"Docs/project.yml":      "name: Docs\n",
				"Docs/README.md":        "not Swift\n",
				"Alone/project.yml":     "name: Alone\n",
			},
			projects: []discoveredProject{{Directory: ".", Engine: engineSwift}},
		},
		{
			name: "two .xcodeproj, or a workspace alone, is no candidate",
			files: map[string]string{
				"App/A.xcodeproj/project.pbxproj":           "// A\n",
				"App/B.xcodeproj/project.pbxproj":           "// B\n",
				"App/App.swift":                             "let app = 1\n",
				"Work/App.xcworkspace/contents.xcworkspace": "<Workspace/>\n",
				"Work/Work.swift":                           "let work = 1\n",
			},
			projects: []discoveredProject{{Directory: ".", Engine: engineSwift}},
		},
		{
			name: "Package.swift beside project.yml is the package alone",
			files: map[string]string{
				"Lib/Package.swift":         discoveryPackage,
				"Lib/project.yml":           "name: Lib\n",
				"Lib/Single.xcodeproj/file": "// one\n",
				"Lib/Sources/Lib/Lib.swift": "let lib = 1\n",
			},
			projects: []discoveredProject{{Directory: "Lib", Engine: engineSwift}},
		},
		{
			// The engine reads a Package.swift first wherever it is, so one the root's ignorePatterns refuse
			// leaves no Xcode project to run beside it, and its directory's Swift is claimed by nothing.
			name: "Package.swift refused beside project.yml leaves no Xcode project",
			files: map[string]string{
				"Lib/Package.swift":         discoveryPackage,
				"Lib/project.yml":           "name: Lib\n",
				"Lib/Sources/Lib/Lib.swift": "let lib = 1\n",
			},
			ignorePatterns: []string{"Lib/Package.swift"},
			projects:       []discoveredProject{{Directory: ".", Engine: engineSwift}},
			refused:        []string{"Lib/Package.swift"},
		},
		{
			name: "a candidate outside any package is a project, and its files no gap",
			files: map[string]string{
				"App/project.yml":       "name: App\n",
				"App/Sources/App.swift": "let app = 1\n",
			},
			projects: []discoveredProject{{Directory: "App", Engine: engineSwift}},
		},
		{
			name: "nested markers each claim their nearest files",
			files: map[string]string{
				"Outer/project.yml":                           "name: Outer\n",
				"Outer/Outer.swift":                           "let outer = 1\n",
				"Outer/Inner/Inner.xcodeproj/project.pbxproj": "// Inner\n",
				"Outer/Inner/Sources/Inner.swift":             "let inner = 1\n",
				"Outer/Kit/Package.swift":                     discoveryPackage,
				"Outer/Kit/Sources/Kit/Kit.swift":             "let kit = 1\n",
			},
			projects: []discoveredProject{
				{Directory: "Outer", Engine: engineSwift},
				{Directory: "Outer/Inner", Engine: engineSwift},
				{Directory: "Outer/Kit", Engine: engineSwift},
			},
		},
		{
			name: "a candidate the root's ignorePatterns match is refused, and its Swift claimed by nothing",
			files: map[string]string{
				"Refused/project.yml": "name: Refused\n",
				"Refused/App.swift":   "let app = 1\n",
			},
			ignorePatterns: []string{"Refused/project.yml"},
			projects:       []discoveredProject{{Directory: ".", Engine: engineSwift}},
			refused:        []string{"Refused/project.yml"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := newRepository(test, testCase.files)
			found, discoverProjectsError := discoverProjects(root, testCase.ignorePatterns)
			if discoverProjectsError != nil {
				test.Fatal(discoverProjectsError)
			}
			if len(found.Projects) != len(testCase.projects) ||
				len(found.Projects) > 0 && !reflect.DeepEqual(found.Projects, testCase.projects) {
				test.Errorf("found the projects %+v, want %+v", found.Projects, testCase.projects)
			}
			if len(found.Refused) != len(testCase.refused) ||
				len(found.Refused) > 0 && !reflect.DeepEqual(found.Refused, testCase.refused) {
				test.Errorf("refused %v, want %v", found.Refused, testCase.refused)
			}
		})
	}
}

// The Swift no project claims is a Swift project of its own at the root, a scripts-only root, inside a repository
// or not (#wpd36zj, #13nbb1s): the engine checks each standalone script there alone, as `swift <file>` runs it, and
// names every other file. A package's own files stay its own.
func TestUnclaimedSwiftIsAScriptsOnlyRootInOrOutOfARepository(test *testing.T) {
	test.Parallel()
	for _, inRepository := range []bool{true, false} {
		test.Run(fmt.Sprintf("in a repository: %t", inRepository), func(test *testing.T) {
			test.Parallel()
			root := newRepository(test, map[string]string{
				"modules/ebay/EbayVision.swift": "print(1)\n",
				"Kit/Package.swift":             discoveryPackage,
				"Kit/Sources/Kit/Kit.swift":     "let kit = 1\n",
			})
			if !inRepository {
				outsideAnyRepository(test, root)
			}
			found, discoverProjectsError := discoverProjects(root, nil)
			if discoverProjectsError != nil {
				test.Fatal(discoverProjectsError)
			}
			want := []discoveredProject{{Directory: ".", Engine: engineSwift}, {Directory: "Kit", Engine: engineSwift}}
			if !reflect.DeepEqual(found.Projects, want) {
				test.Errorf("found the projects %+v, want %+v", found.Projects, want)
			}
		})
	}
}

// ahra's shape: a TypeScript program at the root, a package below it, and a script under no Swift project. The
// script's Swift run is the engine's at the repository's root, through the --directory and engine a discovered
// project's run is given, labelled beside the TypeScript project there (#wpd36zj).
func TestAScriptsOnlyRootRunsTheSwiftEngineAtTheRepositoryRoot(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := mixedRepository(test, "apple/Toy")
	writeTree(test, root, map[string]string{"modules/ebay/EbayVision.swift": "print(1)\n"})
	engine, log := coveringSwiftEngine(test, "echo \"{\\\"kind\\\":\\\"coveredPackages\\\",\\\"root\\\":\\\"$root\\\","+
		"\\\"checked\\\":[\\\"$root\\\"],\\\"vendored\\\":[]}\"\nexit 0")

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode != 0 {
		test.Errorf("a clean repository with a script exited %d, want 0:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "== . (Swift) ==") {
		test.Errorf("the script is not checked by a Swift run at the root:\n%s", output)
	}
	logged, readError := os.ReadFile(log)
	if readError != nil {
		test.Fatal(readError)
	}
	resolvedRoot, resolveSymlinksError := filepath.EvalSymlinks(root)
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	checkedAtRoot := false
	for _, line := range strings.Split(string(logged), "\n") {
		if !strings.Contains(line, "--covered-packages") &&
			(strings.HasSuffix(line, "--root "+root) || strings.Contains(line, "--root "+root+" ") ||
				strings.HasSuffix(line, "--root "+resolvedRoot) || strings.Contains(line, "--root "+resolvedRoot+" ")) {
			checkedAtRoot = true
		}
	}
	if !checkedAtRoot {
		test.Errorf("no check run's --root is the repository's root %s; the engine was run with:\n%s", root, logged)
	}
}

// xcodeRepository writes the shape of ahraos-presence beside a TypeScript program: a root package holding an
// Xcode project, CapturePhone, that no target of it compiles; Tools/Sim, a nested package nothing depends on,
// which runs on its own; and Libraries/Lib, a path dependency the root's run checks. The root's tsconfig.json
// and web/'s make a TypeScript project in the root's directory that yields web/ (#ybspptb).
func xcodeRepository(test *testing.T) string {
	test.Helper()
	root := mixedRepository(test, ".")
	writeTree(test, root, map[string]string{
		"web/tsconfig.json":                   incrementalFixtureConfiguration + "\n",
		"web/CohereSettings.json":             "{ \"rules\": {} }\n",
		"web/index.ts":                        "export const web = 1;\n",
		"CapturePhone/project.yml":            "name: CapturePhone\n",
		"CapturePhone/App/App.swift":          "let app = 1\n",
		"Tools/Sim/Package.swift":             discoveryPackage,
		"Tools/Sim/Sources/Toy/Toy.swift":     "let toy = 1\n",
		"Libraries/Lib/Package.swift":         discoveryPackage,
		"Libraries/Lib/Sources/Toy/Toy.swift": "let toy = 1\n",
	})
	return root
}

// xcodeSwiftEngine is a stand-in engine for xcodeRepository, with the root's check stream given. It answers
// `--covered-packages` with each root checking itself and the root checking Libraries/Lib as well, and logs
// each check run's root, resolved, and the yield its run was handed, to the file it returns.
func xcodeSwiftEngine(test *testing.T, rootStream string) (string, string) {
	test.Helper()
	log := filepath.Join(test.TempDir(), "yields")
	provenance := filepath.Join(contractDirectory(test), "Clean.jsonl")
	clean := contractFixture(test, "Clean.jsonl")
	clean[1] = projectRecordAt(test, clean[1], "/elsewhere", nil)
	otherStream := filepath.Join(test.TempDir(), "other.jsonl")
	if writeError := os.WriteFile(otherStream, []byte(strings.Join(clean, "\n")+"\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	engine := fakeSwiftEngine(test, "root=''; covering=''; previous=''\n"+
		"for argument in \"$@\"; do\n"+
		"  if [ \"$previous\" = --root ]; then root=$argument; fi\n"+
		"  if [ \"$argument\" = --covered-packages ]; then covering=1; fi\n"+
		"  previous=$argument\n"+
		"done\n"+
		"if [ -n \"$covering\" ]; then\n"+
		"  checked=\"\\\"$root\\\"\"\n"+
		"  if [ -f \"$root/Libraries/Lib/Package.swift\" ]; then "+
		"checked=\"$checked,\\\"$root/Libraries/Lib\\\"\"; fi\n"+
		"  head -1 '"+provenance+"'\n"+
		"  echo \"{\\\"kind\\\":\\\"coveredPackages\\\",\\\"root\\\":\\\"$root\\\","+
		"\\\"checked\\\":[$checked],\\\"vendored\\\":[]}\"\n"+
		"  exit 0\n"+
		"fi\n"+
		"yield=''; if [ -n \"$COHERE_PROJECT_YIELD\" ]; then yield=$(cat \"$COHERE_PROJECT_YIELD\"); fi\n"+
		"printf '%s\\t%s\\n' \"$(cd \"$root\" && pwd -P)\" \"$yield\" >> '"+log+"'\n"+
		"if [ -f \"$root/Libraries/Lib/Package.swift\" ]; then cat '"+rootStream+"'; else cat '"+otherStream+"'; fi")
	return engine, log
}

// In ahraos-presence's shape, CapturePhone runs as its own Swift project and the root's run yields it and
// Tools/Sim, the Swift projects running below it, and not Libraries/Lib, which it checks itself, nor web/,
// which only the TypeScript project in the same directory yields. So the root's notes stop naming their files
// as checked by nothing, though the engine spells them through a symbolic link; what remains for a reason
// nobody expects is the root run's gap, on its footer and under --json; and the run is green (#ybspptb).
func TestASwiftRunYieldsTheSwiftProjectsBelowItAndCountsTheRestAsAGap(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := xcodeRepository(test)
	alias := filepath.Join(test.TempDir(), "alias")
	if symlinkError := os.Symlink(root, alias); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	stream := contractFixture(test, "Clean.jsonl")
	stream[1] = projectRecordAt(test, stream[1], alias, []excludedFile{
		{alias + "/CapturePhone/App/App.swift", "no target of this package compiles it"},
		{alias + "/Tools/Sim/Package.swift", "a package manifest"},
		{alias + "/Tools/Sim/Sources/Toy/Toy.swift", "no target of this package compiles it"},
		{alias + "/Stray/Leftover.swift", declarationsOnlyReason},
		{alias + "/Stray/odd.swift", "a reason no engine has given yet"},
		{alias + "/Package.swift", "a package manifest"},
		{alias + "/Vendor/Kit/Kit.swift", "vendored under Vendor/"},
		{alias + "/Scratch.swift", "ignored by git"},
	})
	rootStream := filepath.Join(test.TempDir(), "root.jsonl")
	if writeError := os.WriteFile(rootStream, []byte(strings.Join(stream, "\n")+"\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	engine, log := xcodeSwiftEngine(test, rootStream)

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode != 0 {
		test.Errorf("a clean repository with a gap exited %d, want 0:\n%s", exitCode, output)
	}
	for _, want := range []string{
		"== CapturePhone (Swift) ==",
		"== Tools/Sim (Swift) ==",
		"Libraries/Lib not run: checked as part of .",
		"  not checked: " + alias + "/Stray/Leftover.swift (" + declarationsOnlyReason + ")\n",
		"  not checked: " + alias + "/Stray/odd.swift (a reason no engine has given yet)\n",
		"  not checked: " + alias + "/Package.swift (a package manifest)\n",
		"  not checked: " + alias + "/Vendor/Kit/Kit.swift (vendored under Vendor/)\n",
		"  not checked: " + alias + "/Scratch.swift (ignored by git)\n",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the report lacks %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		"CapturePhone/App/App.swift",
		"Tools/Sim/Package.swift",
		"Tools/Sim/Sources/Toy/Toy.swift",
	} {
		if strings.Contains(output, unwanted) {
			test.Errorf("the report names %q, which a run of its own accounts for:\n%s", unwanted, output)
		}
	}
	// The TypeScript project in the root's directory keeps the label `.`, and the Swift project beside it is
	// `. (Swift)` (#vyjvz9k), so the gap is on that footer alone.
	footers := map[string][]string{}
	gapFooters := 0
	for _, line := range strings.Split(output, "\n") {
		if label, rest, isFooter := strings.Cut(line, "  ✓ 💎 "); isFooter {
			footers[label] = append(footers[label], rest)
			if strings.Contains(rest, "Swift files not checked") {
				gapFooters++
			}
		}
	}
	if !slices.ContainsFunc(footers[". (Swift)"], func(footer string) bool {
		return strings.HasSuffix(footer, " • ⚠ 2 Swift files not checked")
	}) ||
		gapFooters != 1 || len(footers["."]) != 1 {
		test.Errorf(
			"the footers are %q, want the root's Swift one alone to end on the gap of Leftover.swift and odd.swift",
			footers,
		)
	}
	for _, label := range []string{". (Swift)", "CapturePhone", "Tools/Sim"} {
		if len(footers[label]) != 1 {
			test.Errorf("%s printed the footers %q, want one, green", label, footers[label])
		}
	}

	resolvedRoot, resolveSymlinksError := filepath.EvalSymlinks(root)
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	logged, readError := os.ReadFile(log)
	if readError != nil {
		test.Fatal(readError)
	}
	yields := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		directory, encoded, _ := strings.Cut(line, "\t")
		var yield projectYield
		if encoded != "" {
			if decodeError := json.Unmarshal([]byte(encoded), &yield); decodeError != nil {
				test.Fatalf("the yield %q does not decode: %v", encoded, decodeError)
			}
		}
		var relatives []string
		for _, yielded := range yield.Directories {
			relative, relativeError := filepath.Rel(resolvedRoot, resolvedPath(yielded))
			if relativeError != nil {
				test.Fatalf("the yielded %q is not under the root: %v", yielded, relativeError)
			}
			relatives = append(relatives, filepath.ToSlash(relative))
		}
		label, labelError := filepath.Rel(resolvedRoot, directory)
		if labelError != nil {
			test.Fatalf("the logged directory %q is not under the root: %v", directory, labelError)
		}
		yields[filepath.ToSlash(label)] = relatives
	}
	want := map[string][]string{".": {"CapturePhone", "Tools/Sim"}, "CapturePhone": nil, "Tools/Sim": nil}
	if !reflect.DeepEqual(yields, want) {
		test.Errorf("the Swift runs yielded %v, want %v:\n%s", yields, want, logged)
	}

	output, exitCode = runWithEngine(test, binary, engine, root, "--no-fix", "--json")
	if exitCode != 0 {
		test.Errorf("under --json the run exited %d, want 0:\n%s", exitCode, output)
	}
	// Each label's gap, the files named with their reasons. The TypeScript summary in the root's directory is
	// `.` and lists none; the Swift one beside it is `. (Swift)`.
	gaps := map[string][]map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		var summary struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
			Gaps  struct {
				UncheckedSwift []map[string]string `json:"uncheckedSwift"`
			} `json:"gaps"`
		}
		if json.Unmarshal([]byte(line), &summary) != nil || summary.Kind != "summary" {
			continue
		}
		gaps[summary.Label] = append(gaps[summary.Label], summary.Gaps.UncheckedSwift...)
	}
	wantGaps := []map[string]string{
		{"file": alias + "/Stray/Leftover.swift", "reason": declarationsOnlyReason},
		{"file": alias + "/Stray/odd.swift", "reason": "a reason no engine has given yet"},
	}
	if !reflect.DeepEqual(gaps[". (Swift)"], wantGaps) || len(gaps["."]) != 0 || len(gaps["CapturePhone"]) != 0 ||
		len(gaps["Tools/Sim"]) != 0 {
		test.Errorf(
			"under --json the gaps are %v, want Leftover.swift and odd.swift for . (Swift) and none for the others:\n%s",
			gaps,
			output,
		)
	}
}

// contractDirectory is where the Swift contract's fixtures are.
func contractDirectory(test *testing.T) string {
	test.Helper()
	directory, absolutePathError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract"))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	return directory
}

// TestTheCoveredPackagesQueryKeepsANoCacheRunsPromise: describing a package writes SwiftPM's cache, which the
// engine keeps in the package unless told the run writes nothing, so a `--no-cache` run passes the flag to the
// query, and a run with its cache on leaves the engine its own. A `--no-cache` run of two Swift packages once
// left `.cache/cohere/swift` in each, since the query alone never heard the flag.
func TestTheCoveredPackagesQueryKeepsANoCacheRunsPromise(test *testing.T) {
	test.Parallel()
	for _, noCache := range []bool{true, false} {
		test.Run(map[bool]string{true: "under --no-cache", false: "with the cache on"}[noCache], func(test *testing.T) {
			test.Parallel()
			arguments := filepath.Join(test.TempDir(), "arguments")
			engine := fakeSwiftEngine(test, "echo \"$@\" > '"+arguments+"'\n"+
				`echo '{"kind":"coveredPackages","root":"/r","checked":["/r"],"vendored":[]}'`)
			if _, queryError := queryCoveredPackages(engine, test.TempDir(), noCache); queryError != nil {
				test.Fatalf("the query failed: %v", queryError)
			}
			passed, readError := os.ReadFile(arguments)
			if readError != nil {
				test.Fatalf("the stand-in engine never ran: %v", readError)
			}
			if got := strings.Contains(string(passed), "--no-cache"); got != noCache {
				test.Errorf("the engine was given --no-cache %t, expected %t: %s", got, noCache, passed)
			}
		})
	}
}
