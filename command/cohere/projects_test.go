//go:build unix

// Every project here is checked through a stand-in Swift engine, a /bin/sh script, so the file builds on
// Unix, as swift_engine_test.go does (#tejf9bc).

package main

import (
	"os"
	"path/filepath"
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
		"tsconfig.json":       incrementalFixtureConfig + "\n",
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
		"tsconfig.json":       incrementalFixtureConfig + "\n",
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
		"if [ -f \"$root/Libraries/Lib/Package.swift\" ]; then checked=\"$checked,\\\"$printed/Libraries/Lib\\\"\"; fi\n" +
		"if [ -f \"$root/Vendor/Third/Package.swift\" ]; then vendored=\"\\\"$printed/Vendor/Third\\\"\"; fi\n" +
		"head -1 '" +
		provenance +
		"'\n" +
		"echo \"{\\\"kind\\\":\\\"coveredPackages\\\",\\\"root\\\":\\\"$printed\\\",\\\"checked\\\":[$checked],\\\"vendored\\\":[$vendored]}\"\n" +
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
