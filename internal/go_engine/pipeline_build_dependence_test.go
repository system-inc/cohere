package go_engine

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// dependenceModule is the build-dependence fixture's module, whose packages are its workspace.
const dependenceModule = "example.com/dependence"

// lintRecordOf is the stream's lint record.
func lintRecordOf(test *testing.T, stream string) contract.LintRecord {
	test.Helper()
	var lint contract.LintRecord
	for line := range strings.Lines(stream) {
		if strings.HasPrefix(line, `{"kind":"lint"`) {
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
			return lint
		}
	}
	test.Fatal("the stream has no lint record")
	return lint
}

// findingLines are the stream's finding records, sorted, as the census compares two runs' findings.
func findingLines(stream string) []string {
	var found []string
	for line := range strings.Lines(stream) {
		if strings.HasPrefix(line, `{"kind":"finding"`) {
			found = append(found, line)
		}
	}
	slices.Sort(found)
	return found
}

// libraryDependence is the library's own judgment of the fixture at root: the host and the build loaded whole with
// go/packages under the environments the engine's run loaded them under, the graph built from every package of the
// host's, the module's the workspace, and the difference of the two loads' files as written, a build-only file parsed
// for its syntax alone. It shares appendParsed with the engine, the one parse both must make alike, and marks a
// package both have as the engine does: by a build-only file that parse cannot read or parse, and by a line directive
// in a file the two do not share (build_dependence.HoldsLineDirective).
func libraryDependence(test *testing.T, root string, hostEnvironment []string, buildEnvironment []string) Counts {
	test.Helper()
	counts := libraryJudgment(test, root, hostEnvironment, buildEnvironment).judged.Counts()
	return Counts{Workspace: dependenceCounts(counts.Workspace), Dependencies: dependenceCounts(counts.Dependencies)}
}

// libraryJudgment is libraryDependence's judgment of the fixture at root, before it is counted.
func libraryJudgment(
	test *testing.T,
	root string,
	hostEnvironment []string,
	buildEnvironment []string,
) libraryJudged {
	test.Helper()
	load := func(environment []string) map[string]*packages.Package {
		roots, loadError := packages.Load(&packages.Config{
			Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo,
			Tests: true,
			Dir:   root,
			Env:   environment,
		}, "./...")
		if loadError != nil {
			test.Fatal(loadError)
		}
		loaded := map[string]*packages.Package{}
		packages.Visit(roots, nil, func(visited *packages.Package) { loaded[visited.ID] = visited })
		return loaded
	}
	host, built := load(hostEnvironment), load(buildEnvironment)
	workspace := func(loaded *packages.Package) bool { return strings.HasPrefix(loaded.PkgPath, dependenceModule) }
	graph := build_dependence.NewGraph()
	var group sync.WaitGroup
	for _, loaded := range host {
		group.Go(func() { graph.AddPackage(loaded, workspace(loaded), nil) })
	}
	group.Wait()
	difference := build_dependence.Difference{
		Packages:          map[string]build_dependence.PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	}
	for identifier := range host {
		if _, isShared := built[identifier]; !isShared {
			difference.HostOnlyPackages[identifier] = true
		}
	}
	fileSet := token.NewFileSet()
	for identifier, buildPackage := range built {
		hostPackage, isShared := host[identifier]
		var differing build_dependence.PackageDifference
		if !isShared {
			difference.BuildOnlyPackages[identifier] = true
			differing.Workspace = workspace(buildPackage)
		}
		// Parsed for syntax alone, comments left out, as the engine and the counter parse a build-only file, so its
		// nodes count alike.
		for _, file := range buildPackage.GoFiles {
			if !isShared || !slices.Contains(hostPackage.GoFiles, file) {
				var parsed bool
				differing.BuildOnlyFiles, parsed = appendParsed(differing.BuildOnlyFiles, fileSet, file, os.ReadFile)
				differing.EveryUnitDependent = differing.EveryUnitDependent ||
					isShared && (!parsed || build_dependence.HoldsLineDirective(file, os.ReadFile))
			}
		}
		if isShared {
			for _, file := range hostPackage.GoFiles {
				if slices.Contains(buildPackage.GoFiles, file) {
					differing.SharedFiles = append(differing.SharedFiles, file)
				} else {
					differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
					differing.EveryUnitDependent = differing.EveryUnitDependent ||
						build_dependence.HoldsLineDirective(file, os.ReadFile)
				}
			}
		}
		if !isShared || len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 ||
			differing.EveryUnitDependent {
			difference.Packages[identifier] = differing
		}
	}
	return libraryJudged{judged: graph.Against(difference), difference: difference}
}

// Counts are a build's dependence counts by scope, as the record writes them, without its milliseconds.
type Counts struct {
	Workspace    contract.DependenceCounts
	Dependencies contract.DependenceCounts
}

// Under `--build-dependence`, the windows build's record holds the counts the library's own judgment
// gives the same fixture, loaded whole: of the workspace's four units, Separator, which reads os.PathSeparator, a
// dependency's constant windows declares apart, Mode, which reads the module's own platform constant, and that
// constant, in a file windows does not compile, are dependent, and Joined is not. The host's record writes none. The
// findings are the run's without the switch, line for line, and that run builds no graph and writes no counts.
func TestEachBuildsDependenceIsTheLibrarysJudgment(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence"))
	on, onStream := runOver(test, root, exitClean, "--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean)

	lint := lintRecordOf(test, onStream)
	if len(lint.Builds) != 2 || len(on.buildRuns) != 2 {
		test.Fatalf("want the host's build and windows's, got the records %+v", lint.Builds)
	}
	if lint.Builds[0].Dependence != nil {
		test.Errorf("the host's record writes the dependence %+v, want none", lint.Builds[0].Dependence)
	}
	recorded := lint.Builds[1].Dependence
	if recorded == nil {
		test.Fatal("the windows build's record writes no dependence")
	}
	// The process's environment under the run's own, a later variable taking the place of an earlier one, as the
	// engine's loads read them.
	hostEnvironment := append(os.Environ(), on.described.workspace.Environment()...)
	buildEnvironment := append(slices.Clone(hostEnvironment), on.buildRuns[1].build.Environment()...)
	want := libraryDependence(test, root, hostEnvironment, buildEnvironment)
	got := Counts{Workspace: recorded.Workspace, Dependencies: recorded.Dependencies}
	if got != want {
		test.Errorf("the record's dependence is\n%+v\nwant the library's\n%+v", got, want)
	}
	if recorded.Workspace.Units != 4 || recorded.Workspace.DependentUnits != 3 {
		test.Errorf("want 3 of the workspace's 4 units dependent on windows, got %d of %d",
			recorded.Workspace.DependentUnits, recorded.Workspace.Units)
	}
	if recorded.Dependencies.DependentUnits == 0 {
		test.Error("want the dependencies' platform units dependent on windows, got none")
	}

	if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
		test.Errorf("the findings with the switch are\n%q\nwant those without it\n%q", findingLines(onStream),
			findingLines(offStream))
	}
	if off.dependence != nil {
		test.Error("a run without the switch built the host's graph")
	}
	for _, build := range lintRecordOf(test, offStream).Builds {
		if build.Dependence != nil {
			test.Errorf("a run without the switch wrote the dependence %+v", build.Dependence)
		}
	}
}

// The engine's own scan for a line directive reaches each build's judgment (#9paaen9): under `--build-dependence`, the
// build-dependence-line-directive fixture's 7 workspace units are judged on windows and on freebsd, each of which
// declares p's Helper a string. p's gen_other.go, which neither compiles, declares Helper an int under
// `//line shared.go:1`, so its unit is named by a file every build compiles, and only the scan of that host-only file
// seeds p whole: Helper, Plain and Shared, and with them q's Prints and PrintsShared, which read them. plain_other.go,
// another host-only file, read after it, holds no directive and leaves p marked. r's extra_windows.go, which windows
// alone compiles, holds `/*line steady.go:1*/`, the block comment's spelling, and only the scan of that build-only file
// seeds r's Steady. So windows counts 6 dependent, every unit but q's Alone, which reads nothing of either, and freebsd
// 5, Steady independent there too. Both builds ask about gen_other.go, and the builds run at once, so either may scan
// it first and the other read that scan (hostDependence.lineDirectives), or both scan it, alike. On windows, with no
// scan the count is 1, Plain alone, which its host-only file seeds, with none of the host-only files 2, Plain and
// Steady, and with none of the build-only file 5. The run's one finding is printf's of Helper, a string on windows and
// freebsd, formatted with %d.
//
// The scan reads each file as the run holds it (pipeline.held), the bytes the load built the graph from, never the
// disk again: with gen_other.go rewritten on disk without its directive once the run has read it, as a save in the
// middle of a run leaves it, the graph still names Helper by shared.go, the scan still seeds p, and the counts are the
// same.
func TestALineDirectiveInAnUnsharedFileSeedsItsPackageInTheEngine(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		name         string
		rewritesDisk bool
	}{
		{name: "as read", rewritesDisk: false},
		{name: "rewritten on disk once read", rewritesDisk: true},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "build-dependence-line-directive"))
			var configure func(*pipeline)
			if each.rewritesDisk {
				// Given the pipeline once describeProject has read every file, before the run's first phase.
				configure = func(*pipeline) {
					writeTestFile(test, filepath.Join(root, "p", "gen_other.go"),
						"//go:build !windows && !freebsd\n\npackage p\n\nfunc Helper() int { return 1 }\n")
				}
			}
			engine, stream := runOverConfigured(test, root, exitFindings, configure, "--no-cache", "--build-dependence")
			lint := lintRecordOf(test, stream)
			if len(lint.Builds) != 3 || len(engine.buildRuns) != 3 {
				test.Fatalf("want the host's build, windows's and freebsd's, got the records %+v", lint.Builds)
			}
			for index, want := range []struct {
				operatingSystem string
				dependent       int
				independent     string
			}{
				{operatingSystem: "windows", dependent: 6, independent: "q's Alone"},
				{operatingSystem: "freebsd", dependent: 5, independent: "q's Alone and r's Steady"},
			} {
				if got := engine.buildRuns[index+1].build.OperatingSystem; got != want.operatingSystem {
					test.Fatalf("want extra build %d on %s, got %s", index+1, want.operatingSystem, got)
				}
				recorded := lint.Builds[index+1].Dependence
				if recorded == nil {
					test.Fatalf("the %s build's record writes no dependence", want.operatingSystem)
				}
				if recorded.Workspace.Units != 7 || recorded.Workspace.DependentUnits != want.dependent {
					test.Errorf("want %d of the workspace's 7 units dependent on %s, every one but %s, got %d of %d",
						want.dependent, want.operatingSystem, want.independent, recorded.Workspace.DependentUnits,
						recorded.Workspace.Units)
				}
			}
		})
	}
}

// The engine's scan reads a file the run holds from the held bytes, never the disk's, and answers a second ask from
// its first scan (hostDependence.lineDirectives), as a second build asks; a file the run holds nothing for, read from
// disk, that cannot be read there is said to hold a directive, failing wide.
func TestTheLineDirectiveScanAnswersFromTheHeldBytesOnceAndFailsWide(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	held := filepath.Join(directory, "held.go")
	writeTestFile(test, held, "package p\n\nfunc Helper() int { return 1 }\n")
	engine := &pipeline{dependence: &hostDependence{}, described: &describedProject{contents: map[string][]byte{
		held: []byte("package p\n\n//line shared.go:1\nfunc Helper() int { return 1 }\n"),
	}}}
	for _, ask := range []string{"first", "again"} {
		if !engine.holdsLineDirective(held) {
			test.Errorf("asked %s, want the directive the run holds of held.go, which its disk lacks", ask)
		}
	}
	if !engine.holdsLineDirective(filepath.Join(directory, "unread.go")) {
		test.Error("want unread.go, which neither the run nor the disk holds, said to hold a directive")
	}
}

// The standard library's graph is judged with the engine's scan too (standardLibraryDifference): a package of it with
// a host-only file under a line directive naming a shared file is seeded whole. No file of the standard library the
// builds do not share holds a directive today, so the build-dependence-line-directive fixture's p stands in for one:
// its load on darwin is the standard library's graph, which the host restored, and its load on windows the build's.
// All 3 of its units, Helper, Plain and Shared, are dependent; with no scan only Plain, in its host-only file, is.
//
// A package whose one difference is a build-only file that cannot be read or parsed is seeded whole too, since what
// the file declares is unknown, though appendParsed leaves it out. With p's host files named as shared.go alone and
// the build's as shared.go and unread_windows.go, a file on no disk, or broken_windows.go, which holds no directive and
// does not parse, written once the loads have read p, all 3 units are dependent.
func TestTheStandardLibrarysGraphIsJudgedWithTheLineDirectiveScan(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence-line-directive"))
	const identifier = "example.com/dependenceline/p"
	host := loadedFromSource(test, root, "darwin", identifier)
	built := loadedFromSource(test, root, "windows", identifier)
	shared := filepath.Join(root, "p", "shared.go")
	broken := filepath.Join(root, "p", "broken_windows.go")
	writeTestFile(test, broken, "package p\n\nfunc Broken( {\n")
	for _, each := range []struct {
		name      string
		hostFiles []string
		built     *packages.Package
	}{
		{name: "a host-only file under a directive", hostFiles: host.GoFiles, built: built},
		{name: "a build-only file that cannot be read", hostFiles: []string{shared}, built: &packages.Package{
			ID:      identifier,
			PkgPath: identifier,
			GoFiles: []string{shared, filepath.Join(root, "p", "unread_windows.go")},
		}},
		{name: "a build-only file that does not parse", hostFiles: []string{shared}, built: &packages.Package{
			ID:      identifier,
			PkgPath: identifier,
			GoFiles: []string{shared, broken},
		}},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			standardLibrary := build_dependence.NewGraph()
			standardLibrary.AddPackage(host, false, nil)
			// The standard library's graph has ended, as every build waits for it to.
			done := make(chan struct{})
			close(done)
			engine := &pipeline{described: &describedProject{}, dependence: &hostDependence{
				graph:    build_dependence.NewGraph(),
				files:    map[string][]string{},
				paths:    map[string]string{},
				restored: map[string]bool{identifier: true},
				standardLibrary: &standardLibraryDependence{done: done, graph: standardLibrary,
					files: map[string][]string{identifier: each.hostFiles}},
			}}
			record, _ := engine.dependenceOf(&load.Loaded{Roots: []*packages.Package{each.built}})
			if record == nil || record.Restored == nil {
				test.Fatalf("want a record of the restored standard library's judgment, got %+v", record)
			}
			if record.Restored.StandardLibraryUnits != 3 || record.Restored.StandardLibraryDependentUnits != 3 {
				test.Errorf("want all 3 of the standard library's units dependent on windows, p's Helper, Plain and "+
					"Shared, got %d of %d", record.Restored.StandardLibraryDependentUnits,
					record.Restored.StandardLibraryUnits)
			}
		})
	}
}

// A package whose one difference is a build-only file that does not parse is seeded whole (#9paaen9): the
// build-dependence-unparsed fixture's s is given broken_windows.go, which windows alone compiles and no parser reads,
// written into the copy, since a file of the repository's own that does not parse fails the walks that parse them all.
// appendParsed leaves a file it cannot parse out, so s has no build-only file to count, and what that file declares is
// unknown, so Steady, in steady.go, which both builds compile, is dependent on windows, whether the file holds
// `//line steady.go:1`, which the scan reads in its bytes, or no directive at all.
func TestAPackageWhoseBuildOnlyFileIsUnparsedIsSeededWhole(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		name   string
		broken string
	}{
		{name: "under a directive", broken: "package s\n\n//line steady.go:1\nfunc Broken( {\n"},
		{name: "with no directive", broken: "package s\n\nfunc Broken( {\n"},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "build-dependence-unparsed"))
			writeTestFile(test, filepath.Join(root, "s", "broken_windows.go"), each.broken)
			engine, _ := runOverConfigured(test, root, exitFindings, nil, "--no-cache", "--build-dependence")
			if len(engine.buildRuns) != 2 || engine.buildRuns[1].build.OperatingSystem != "windows" {
				test.Fatalf("want the host's build and windows's, got %d builds", len(engine.buildRuns))
			}
			if engine.buildRuns[1].judged == nil {
				test.Fatal("the windows build judged no dependence")
			}
			var workspace []build_dependence.Unit
			for _, unit := range engine.buildRuns[1].judged.dependence.Units() {
				if unit.Workspace {
					workspace = append(workspace, unit)
				}
			}
			if len(workspace) != 1 || !workspace[0].Dependent {
				test.Errorf("want s's one unit, Steady, dependent on windows, got the workspace's units %+v",
					workspace)
			}
		})
	}
}

// A package the build checks only as its in-package test variant is judged with the variant's build-only files that
// are the package's own, those not named as test files (#9paaen9). In the build-dependence-variant fixture windows
// alone compiles v's in-package test, so the host checks v and windows checks v [v.test] in its place, and nothing
// imports v, so windows's load leaves v out (load.Caching's AnalyzedRootsOnly). v's shape_windows.go, which windows
// alone compiles, declares String on Shape, a type both builds have, so Shape's method set moves: Shape and Printed,
// which formats it, are dependent, and Doubled, which reads nothing of it, is not. v's shape_windows_test.go declares
// String on Label, but a test file is the variant's, never v's, so Label and Labelled, which formats it, are not
// dependent. u has an in-package test in every build, so the host checks u and u [u.test], and windows u [u.test]
// alone, and kind_windows.go declares String on Kind: the host's u, compared with windows's variant as v is, is judged
// as u [u.test], compared with its own counterpart, is, Kind and Printed dependent in each, Doubled and the test not.
//
// A build-only file is parsed from the bytes the run holds of it (pipeline.held), as the build's load parsed it, never
// the disk's again: with shape_windows.go rewritten on disk without String once the run has read it, Shape's method
// set still moves, and the counts are the same.
func TestABuildOnlyFileOfAPackageTheBuildChecksAsItsVariantIsJudged(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		name         string
		rewritesDisk bool
	}{
		{name: "as read", rewritesDisk: false},
		{name: "rewritten on disk once read", rewritesDisk: true},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "build-dependence-variant"))
			var configure func(*pipeline)
			if each.rewritesDisk {
				// Given the pipeline once describeProject has read every file, before the run's first phase.
				configure = func(*pipeline) {
					writeTestFile(test, filepath.Join(root, "v", "shape_windows.go"), "package v\n")
				}
			}
			engine, _ := runOverConfigured(test, root, exitClean, configure, "--no-cache", "--build-dependence")
			if len(engine.buildRuns) != 2 || engine.buildRuns[1].build.OperatingSystem != "windows" {
				test.Fatalf("want the host's build and windows's, got %d builds", len(engine.buildRuns))
			}
			const shapes = "example.com/dependencevariant/v"
			if engine.buildRuns[0].analyzed[shapes+" ["+shapes+".test]"] ||
				!engine.buildRuns[1].analyzed[shapes+" ["+shapes+".test]"] {
				test.Fatalf("want v [v.test] analyzed on windows alone, got the host's %v and windows's %v",
					engine.buildRuns[0].analyzed, engine.buildRuns[1].analyzed)
			}
			if engine.buildRuns[1].judged == nil {
				test.Fatal("the windows build judged no dependence")
			}
			// Each package's workspace units, dependent and in all, as "dependent of units".
			got := map[string]string{}
			counts := map[string][2]int{}
			for _, unit := range engine.buildRuns[1].judged.dependence.Units() {
				if !unit.Workspace {
					continue
				}
				counted := counts[unit.PackageId]
				if unit.Dependent {
					counted[0]++
				}
				counted[1]++
				counts[unit.PackageId] = counted
			}
			for identifier, counted := range counts {
				got[identifier] = fmt.Sprintf("%d of %d", counted[0], counted[1])
			}
			want := map[string]string{
				shapes:                            "2 of 5",
				"example.com/dependencevariant/u": "2 of 3",
				"example.com/dependencevariant/u [example.com/dependencevariant/u.test]": "2 of 4",
			}
			if !maps.Equal(got, want) {
				test.Errorf("the dependent units of each package on windows are %v, want %v", got, want)
			}
			// The build-only units are counted from each build-only file's syntax as the run holds it: v [v.test]'s
			// 8, a package the host does not have, counted whole, and String in each of plain v's, plain u's and
			// u [u.test]'s comparisons, 11.
			if buildOnly := engine.buildRuns[1].judged.dependence.Counts().Workspace.BuildOnlyUnits; buildOnly != 11 {
				test.Errorf("want 11 build-only units on windows, got %d", buildOnly)
			}
			// The variants' own GoFiles, which the judgment compared less their test files, are the build's as it
			// loaded them, each test file among the files windows has a say on.
			for _, file := range []string{filepath.Join(root, "v", "shape_windows_test.go"),
				filepath.Join(root, "u", "kind_test.go")} {
				if !engine.buildRuns[1].said[file] {
					test.Errorf("want windows to have a say on %s, got %v", file, engine.buildRuns[1].said)
				}
			}
		})
	}
}

// `--build-dependence` under `--host-build-only` has no extra build to judge, so it builds no graph and the run's
// stream is that of the host's build alone (#qqyq61s).
func TestBuildDependenceOfTheHostsBuildAloneChangesNothing(test *testing.T) {
	test.Parallel()

	switched, switchedStream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")),
		exitClean, "--host-build-only", "--build-dependence")
	_, plainStream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean,
		"--host-build-only")
	if switched.dependence != nil {
		test.Error("the switch under --host-build-only built the host's graph")
	}
	if switchedStream != plainStream {
		test.Errorf("the stream with the switch under --host-build-only is\n%s\nwant the run's without it\n%s",
			switchedStream, plainStream)
	}
}

// filesDiffer says where a cgo tool translated a package on either side, which no file's name tells. The host's cgo
// tool translated native.go, so a build that translates it too shares every file as written with the host, has no file
// of its own or the host's alone, and is said to keep cgo on, by which the graph seeds the host's units of the cgo file
// and of the tool's generated file. A build with cgo off lists no cgo file: native.go is then a file only the host
// compiles, and cgo is still said kept on, since the file the host's tool generated is in no file list and only that
// seeds its units. Where the host translated nothing and the build has cgo off, nothing is said of cgo.
func TestFilesDifferSaysWhereTheBuildKeepsCgoOn(test *testing.T) {
	test.Parallel()

	plainFile, cgoFile := "/native/plain.go", "/native/native.go"
	hostFiles := []string{plainFile, cgoFile}
	translated := &packages.Package{GoFiles: hostFiles,
		CompiledGoFiles: []string{plainFile, "/cache/generated", "/cache/translation"}}
	withoutCgo := &packages.Package{GoFiles: []string{plainFile}, CompiledGoFiles: []string{plainFile}}
	hostCgoFiles := build_dependence.CgoFiles(translated)
	// No file here is on disk, and none is read: no build has a file of its own, and no host-only file holds a
	// directive.
	holdsNoDirective := func(string) bool { return false }

	kept := filesDiffer(hostFiles, hostCgoFiles, translated, translated.GoFiles, token.NewFileSet(), os.ReadFile,
		holdsNoDirective)
	if !kept.KeepsCgo || len(kept.HostOnlyFiles) > 0 || len(kept.BuildOnlyFiles) > 0 {
		test.Errorf("a build that translates the package: want cgo kept on and no file differing, got %+v", kept)
	}
	off := filesDiffer(hostFiles, hostCgoFiles, withoutCgo, withoutCgo.GoFiles, token.NewFileSet(), os.ReadFile,
		holdsNoDirective)
	if !off.KeepsCgo || !slices.Equal(off.HostOnlyFiles, []string{cgoFile}) || len(off.BuildOnlyFiles) > 0 {
		test.Errorf("a build with cgo off: want %s the host's alone and cgo kept on, got %+v", cgoFile, off)
	}
	untranslated := filesDiffer(hostFiles, nil, withoutCgo, withoutCgo.GoFiles, token.NewFileSet(), os.ReadFile,
		holdsNoDirective)
	if untranslated.KeepsCgo || !slices.Equal(untranslated.HostOnlyFiles, []string{cgoFile}) {
		test.Errorf("a build with cgo off where the host translated nothing: want %s the host's alone and nothing "+
			"said of cgo, got %+v", cgoFile, untranslated)
	}
}

// checkedByHand is the package at path of files, each a name and its source, parsed and type-checked here with no
// loader and no C compiler, package "C" faked for a file that imports it, as build_dependence.Graph.AddPackage takes a
// loaded package. Its listing, the files as written and those compiled, is the caller's to set.
func checkedByHand(test *testing.T, path string, files [][2]string) *packages.Package {
	test.Helper()
	fileSet := token.NewFileSet()
	var syntax []*ast.File
	for _, file := range files {
		parsed, parseError := parser.ParseFile(fileSet, file[0], file[1], 0)
		if parseError != nil {
			test.Fatal(parseError)
		}
		syntax = append(syntax, parsed)
	}
	information := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Instances:  map[*ast.Ident]types.Instance{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	checked, checkError := (&types.Config{FakeImportC: true}).Check(path, fileSet, syntax, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return &packages.Package{ID: path, PkgPath: path, Fset: fileSet, Syntax: syntax, Types: checked,
		TypesInfo: information}
}

// dependenceOf seeds a package whose only difference is cgo, on the host's graph and on the standard library's, by
// what the host's listing and the build's say of the cgo tool, with every file as written shared and none of either
// side's alone. The host's graph holds native as the host's cgo tool left it, built by hand: a plain file, the file
// the tool generates and a translation that opens with the tool's `//line` directive, Slots in the translation reading
// `_Ciconst_SLOTS` of the generated file. The standard library's graph holds a package checked as written, as its side
// load checks every one, which the host's listing says the host's tool translated. Where the build translates both
// again, the cgo units of each are seeds. Where it checks them as written, only the host's cgo files say cgo is kept
// on, and the units are seeds still. Where the host translated nothing and the build checks them as written, nothing
// of either moves. Steady, of native's plain file, moves in none.
func TestDependenceOfSeedsAPackageWhoseOnlyDifferenceIsCgo(test *testing.T) {
	test.Parallel()

	const nativePath = "example.com/native"
	const standardPath = "standardcgo"
	nativePlain, nativeCgo := "/native/plain.go", "/native/native.go"
	generated, translation := "/cache/host/_cgo_gotypes.go", "/cache/host/native.cgo1.go"
	native := checkedByHand(test, nativePath, [][2]string{
		{nativePlain, "package native\n\nconst Steady = 1\n"},
		{generated, "package native\n\nconst _Ciconst_SLOTS = 4\n"},
		{translation, "//line " + nativeCgo + ":1:1\npackage native\n\nconst Slots = _Ciconst_SLOTS\n"},
	})
	native.GoFiles = []string{nativePlain, nativeCgo}
	native.CompiledGoFiles = []string{nativePlain, generated, translation}
	standardPlain, standardCgo := "/standard/plain.go", "/standard/native.go"
	standard := checkedByHand(test, standardPath, [][2]string{
		{standardPlain, "package standardcgo\n"},
		{standardCgo, "package standardcgo\n\nimport \"C\"\n\nvar Slots = C.SLOTS\n"},
	})
	standard.GoFiles = []string{standardPlain, standardCgo}
	standard.CompiledGoFiles = standard.GoFiles
	graph, standardGraph := build_dependence.NewGraph(), build_dependence.NewGraph()
	graph.AddPackage(native, true, nil)
	standardGraph.AddPackage(standard, false, nil)
	sideLoaded := make(chan struct{})
	close(sideLoaded)

	translatedCgoFiles := map[string][]string{nativePath: build_dependence.CgoFiles(native),
		standardPath: {standardCgo}}
	if !slices.Equal(translatedCgoFiles[nativePath], []string{nativeCgo}) {
		test.Fatalf("want %s alone translated on the host, got %q", nativeCgo, translatedCgoFiles[nativePath])
	}
	// What a build compiles of a package's files as written: the cgo tool's outputs in the cgo file's place where it
	// translates the package, and the files themselves where it checks them as written.
	translating := func(plain string) []string {
		return []string{plain, "/cache/build/_cgo_gotypes.go", "/cache/build/native.cgo1.go"}
	}
	asWritten := func(plain string, cgo string) []string { return []string{plain, cgo} }
	for _, build := range []struct {
		name             string
		hostCgoFiles     map[string][]string
		nativeCompiled   []string
		standardCompiled []string
		want             bool
	}{
		{"a build that translates what the host translated", translatedCgoFiles, translating(nativePlain),
			translating(standardPlain), true},
		{"a build that checks as written what the host translated", translatedCgoFiles,
			asWritten(nativePlain, nativeCgo), asWritten(standardPlain, standardCgo), true},
		{"a build that checks as written what the host did too", map[string][]string{},
			asWritten(nativePlain, nativeCgo), asWritten(standardPlain, standardCgo), false},
	} {
		engine := &pipeline{dependence: &hostDependence{
			graph:    graph,
			files:    map[string][]string{nativePath: native.GoFiles, standardPath: standard.GoFiles},
			cgoFiles: build.hostCgoFiles,
			paths:    map[string]string{nativePath: nativePath, standardPath: standardPath},
			restored: map[string]bool{standardPath: true},
			standardLibrary: &standardLibraryDependence{done: sideLoaded, graph: standardGraph,
				files: map[string][]string{standardPath: standard.GoFiles}},
		}}
		_, judged := engine.dependenceOf(&load.Loaded{Roots: []*packages.Package{
			{ID: nativePath, PkgPath: nativePath, GoFiles: native.GoFiles, CompiledGoFiles: build.nativeCompiled},
			{ID: standardPath, PkgPath: standardPath, GoFiles: standard.GoFiles,
				CompiledGoFiles: build.standardCompiled},
		}})
		if judged == nil || judged.dependence == nil || judged.standardLibrary == nil {
			test.Fatalf("%s: want a judgment of the host's graph and of the standard library's, got %+v", build.name,
				judged)
		}
		for _, name := range []struct {
			graph      string
			dependence *build_dependence.Dependence
			path       string
			local      string
			want       bool
		}{
			{"the host's graph", judged.dependence, nativePath, "Slots", build.want},
			{"the host's graph", judged.dependence, nativePath, "_Ciconst_SLOTS", build.want},
			{"the host's graph", judged.dependence, nativePath, "Steady", false},
			{"the standard library's graph", judged.standardLibrary, standardPath, "Slots", build.want},
		} {
			if bits, isDeclared := name.dependence.NameBits(name.path, name.local); !isDeclared ||
				bits.Name != name.want {
				test.Errorf("%s: %s of %s: want its name bit %t in %s, got %+v, declared %t", build.name, name.local,
					name.path, name.want, name.graph, bits, isDeclared)
			}
		}
	}
}

// buildDependenceGraph keeps the host's cgo files of each package from its listing (build_dependence.CgoFiles), so a
// build's judgment knows the host's cgo tool translated a package where nothing else says so: a build that checks as
// written every file the host's tool translated, as one whose own translation failed does, shares every file as
// written with the host and compiles none apart, and only the host's cgo files say cgo is kept on. The host's load
// holds native as its cgo tool left it, built by hand, and the graph is built from that load as a run that judges
// dependence builds it, with one extra build beside the host's. Slots, of the translation, and `_Ciconst_SLOTS`, of
// the generated file, are seeds in that build; Steady, of the plain file, is not. So they are where the build checks
// native only as its in-package test variant, "native [native.test]", which a build's load of analyzed roots keeps in
// native's place where nothing imports native and a test file only that build compiles is native's (load's
// reachedRoots), and the host, with no such test file, has no variant of native: the host's native is compared with
// the build's variant, and its cgo files are those the host's graph keeps under native's own ID.
func TestTheHostsGraphKeepsItsCgoFilesForEveryBuildsJudgment(test *testing.T) {
	test.Parallel()

	const nativePath = "example.com/native"
	nativePlain, nativeCgo := "/native/plain.go", "/native/native.go"
	generated, translation := "/cache/host/_cgo_gotypes.go", "/cache/host/native.cgo1.go"
	native := checkedByHand(test, nativePath, [][2]string{
		{nativePlain, "package native\n\nconst Steady = 1\n"},
		{generated, "package native\n\nconst _Ciconst_SLOTS = 4\n"},
		{translation, "//line " + nativeCgo + ":1:1\npackage native\n\nconst Slots = _Ciconst_SLOTS\n"},
	})
	native.GoFiles = []string{nativePlain, nativeCgo}
	native.CompiledGoFiles = []string{nativePlain, generated, translation}

	engine := &pipeline{
		request: invocation{switches: map[string]bool{"build-dependence": true}},
		described: &describedProject{
			builds: []project.Build{
				{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true},
				{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true, Tags: []string{"wide"}},
			},
			inScopeSet: map[string]bool{nativePlain: true, nativeCgo: true},
		},
	}
	engine.buildDependenceGraph(&load.Loaded{Roots: []*packages.Package{native}}, nil)
	if engine.dependence == nil {
		test.Fatal("want the host's graph built from its load")
	}
	if cgoFiles := engine.dependence.cgoFiles[nativePath]; !slices.Equal(cgoFiles, []string{nativeCgo}) {
		test.Errorf("want the host's graph to keep %s as native's cgo file, got %q", nativeCgo, cgoFiles)
	}
	variantFiles := append(slices.Clone(native.GoFiles), "/native/native_tagged_test.go")
	for _, build := range []struct {
		name string
		root *packages.Package
	}{
		{"a build that checks the cgo file as written", &packages.Package{ID: nativePath, PkgPath: nativePath,
			GoFiles: native.GoFiles, CompiledGoFiles: native.GoFiles}},
		{"a build that checks the cgo file as written in native's test variant alone", &packages.Package{
			ID: nativePath + " [" + nativePath + ".test]", PkgPath: nativePath, ForTest: nativePath,
			GoFiles: variantFiles, CompiledGoFiles: variantFiles}},
	} {
		_, judged := engine.dependenceOf(&load.Loaded{Roots: []*packages.Package{build.root}})
		if judged == nil || judged.dependence == nil {
			test.Fatalf("%s: want a judgment of the host's graph, got %+v", build.name, judged)
		}
		for local, want := range map[string]bool{"Slots": true, "_Ciconst_SLOTS": true, "Steady": false} {
			if bits, isDeclared := judged.dependence.NameBits(nativePath, local); !isDeclared || bits.Name != want {
				test.Errorf("%s: want its name bit %t under %s, got %+v, declared %t", local, want, build.name, bits,
					isDeclared)
			}
		}
	}
}

// withheldFixture writes the withheld-through-another-package fixture under a directory of its own and returns its
// project's root. The project's app imports d, of a dependency module beside it, outside the workspace. The
// dependency's e declares Value in a cgo file, `C.int(4)`, which the faked package "C" leaves invalid, and, under the
// tag `purego`, in a plain file as an int32 in its place. Its d has a cgo file of its own, so the load withholds what
// of d holds an invalid expression, and a plain file, the same in every build, whose Doubled and Size read e.Value.
// Neither converts it: `int(e.Value)` is a type error the host reports, which reaches nothing of d's own cgo files,
// and a load with one builds no graph.
func withheldFixture(test *testing.T) string {
	test.Helper()
	return writtenFixture(test, map[string]string{
		"project/go.mod": "module example.com/project\n\ngo 1.21\n\nrequire example.com/dependency v0.0.0\n\n" +
			"replace example.com/dependency => ../dependency\n",
		"project/app/app.go": "package app\n\nimport \"example.com/dependency/d\"\n\n" +
			"// Twice is d's doubled value.\nvar Twice = d.Doubled\n",
		"dependency/go.mod": "module example.com/dependency\n\ngo 1.21\n",
		"dependency/e/e_cgo.go": "//go:build !purego\n\npackage e\n\nimport \"C\"\n\n" +
			"// Value is four, of C.\nvar Value = C.int(4)\n",
		"dependency/e/e_purego.go": "//go:build purego\n\npackage e\n\n// Value is four.\nvar Value int32 = 4\n",
		"dependency/d/d_cgo.go": "package d\n\n// static int one(void) { return 1; }\nimport \"C\"\n\n" +
			"func one() int { return int(C.one()) }\n",
		"dependency/d/d.go": "package d\n\nimport \"example.com/dependency/e\"\n\n// Doubled is twice e's value.\n" +
			"var Doubled = e.Value * 2\n\n// Size is e's value.\nfunc Size() any { return e.Value }\n",
	})
}

// writtenFixture writes files, each a path under a directory of its own and its contents, and returns its project's
// root, the directory "project" under it.
func writtenFixture(test *testing.T, files map[string]string) string {
	test.Helper()
	root := test.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatal(directoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return filepath.Join(root, "project")
}

// A withheld declaration after a `//line` directive is placed where both the load and the graph name it, past the
// directive, so its unit alone is marked: the load writes a withheld declaration's position past any //line directive
// (load's withholdCgoDeclarations) and the graph reads a unit's the same way (collector.newUnit). The project's app
// imports l, of a dependency module beside it, outside the workspace, which the host fakes: l's cgo file declares raw,
// of a C type, and its plain file declares Steady, then names generated.go by a directive, then Raw, whose initializer
// calls raw, so the load withholds it, and nothing else of l. Under a difference that says nothing of l, Raw is a seed
// and Steady is not: were either side to read the position as parsed, ignoring the directive, the two would disagree,
// Raw's position would place no unit, and every unit of l would be marked (Graph.addPackage), Steady among them.
func TestAWithheldDeclarationPastALineDirectiveIsPlaced(test *testing.T) {
	test.Parallel()

	projectRoot := writtenFixture(test, map[string]string{
		"project/go.mod": "module example.com/project\n\ngo 1.21\n\nrequire example.com/dependency v0.0.0\n\n" +
			"replace example.com/dependency => ../dependency\n",
		"project/app/app.go": "package app\n\nimport \"example.com/dependency/l\"\n\n" +
			"// Twice is l's raw value.\nvar Twice = l.Raw\n",
		"dependency/go.mod":     "module example.com/dependency\n\ngo 1.21\n",
		"dependency/l/l_cgo.go": "package l\n\nimport \"C\"\n\nfunc raw() C.int { return 4 }\n",
		"dependency/l/l.go": "package l\n\n// Steady is one.\nconst Steady = 1\n\n//line generated.go:100\n" +
			"// Raw is C's.\nvar Raw = raw()\n",
	})
	host, hostError := load.Packages(projectRoot, []string{"./..."}, []string{"GOWORK=off", "CGO_ENABLED=1"}, nil)
	if hostError != nil {
		test.Fatal(hostError)
	}
	if len(host.Diagnostics) > 0 {
		test.Fatalf("want the load clean, got %v", host.Diagnostics)
	}
	const lPath = "example.com/dependency/l"
	var withheld []string
	packages.Visit(host.Roots, nil, func(each *packages.Package) {
		if each.ID == lPath {
			for _, position := range host.Withheld[each].Declarations {
				withheld = append(withheld, filepath.Base(position))
			}
		}
	})
	slices.Sort(withheld)
	// A directive with no column leaves every column after it unknown, and the position names none.
	if !slices.Equal(withheld, []string{"generated.go:101"}) {
		test.Errorf("want l's Raw withheld, placed past the directive, got %q", withheld)
	}

	engine := &pipeline{
		request: invocation{switches: map[string]bool{"build-dependence": true}},
		described: &describedProject{
			builds: []project.Build{
				{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true},
				{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true, Tags: []string{"other"}},
			},
			inScopeSet: map[string]bool{filepath.Join(projectRoot, "app", "app.go"): true},
		},
	}
	engine.buildDependenceGraph(host, nil)
	if engine.dependence == nil {
		test.Fatal("want the host's graph built from its load")
	}
	dependence := engine.dependence.graph.Against(build_dependence.Difference{})
	for local, want := range map[string]bool{"Raw": true, "Steady": false} {
		if bits, isDeclared := dependence.NameBits(lPath, local); !isDeclared || bits.Name != want {
			test.Errorf("l's %s: want its name bit %t under a build that says nothing of l, got %+v, declared %t",
				local, want, bits, isDeclared)
		}
	}
}

// What the host's load withholds is a seed in every build, whatever the build's difference says (#9paaen9, the
// Square's Must B). The host fakes both e and d, cgo on with no cgo of the project's own, and the load withholds d's
// Doubled and Size, whose value and body hold e.Value, an expression the faked package "C" left invalid in e
// (load's holdsInvalid), though they name nothing of d's own cgo file. A build under the tag `purego`, also faking,
// compiles e's plain file in its cgo file's place, so e differs and its Value is a seed, but d's files are the same
// in both builds and d is in no difference: Doubled and Size, with no value, no body and no edge to e.Value, are
// checked in the build for the first time, and only what the host withheld says so. No C compiler runs: go list
// reads cgo files with cgo on and none.
func TestWhatTheHostWithheldIsASeedInEveryBuild(test *testing.T) {
	test.Parallel()

	projectRoot := withheldFixture(test)
	environment := []string{"GOWORK=off", "CGO_ENABLED=1"}
	host, hostError := load.Packages(projectRoot, []string{"./..."}, environment, nil)
	if hostError != nil {
		test.Fatal(hostError)
	}
	built, builtError := load.Packages(projectRoot, []string{"./..."}, append(slices.Clone(environment),
		"GOFLAGS=-tags=purego"), nil)
	if builtError != nil {
		test.Fatal(builtError)
	}
	if len(host.Diagnostics)+len(built.Diagnostics) > 0 {
		test.Fatalf("want both loads clean, got %v and %v", host.Diagnostics, built.Diagnostics)
	}
	const dPath = "example.com/dependency/d"
	// What the load withheld of d's plain file, d's cgo file's one being withheld in both builds.
	withheldOf := func(loaded *load.Loaded) []string {
		var withheld []string
		packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
			if each.ID != dPath {
				return
			}
			for _, position := range loaded.Withheld[each].Declarations {
				if strings.Contains(position, string(filepath.Separator)+"d.go:") {
					withheld = append(withheld, position[strings.LastIndex(position, "d.go:"):])
				}
			}
		})
		return withheld
	}
	if hostWithheld, builtWithheld := withheldOf(host), withheldOf(built); !slices.Equal(hostWithheld,
		[]string{"d.go:6:5", "d.go:9:1"}) || len(builtWithheld) != 0 {
		test.Fatalf("want d's Doubled and Size withheld on the host alone, got %q and %q", hostWithheld,
			builtWithheld)
	}

	engine := &pipeline{
		request: invocation{switches: map[string]bool{"build-dependence": true}},
		described: &describedProject{
			builds: []project.Build{
				{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true},
				{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true, Tags: []string{"purego"}},
			},
			inScopeSet: map[string]bool{filepath.Join(projectRoot, "app", "app.go"): true},
		},
	}
	engine.buildDependenceGraph(host, nil)
	if engine.dependence == nil {
		test.Fatal("want the host's graph built from its load")
	}
	_, judged := engine.dependenceOf(built)
	if judged == nil || judged.dependence == nil {
		test.Fatalf("want a judgment of the host's graph, got %+v", judged)
	}
	if bits, isDeclared := judged.dependence.NameBits("example.com/dependency/e", "Value"); !isDeclared || !bits.Name {
		test.Errorf("e's Value: want its name bit, e's files differing, got %+v, declared %t", bits, isDeclared)
	}
	if bits, isDeclared := judged.dependence.NameBits(dPath, "Doubled"); !isDeclared || !bits.Name {
		test.Errorf("d's Doubled: want its name bit, its value withheld on the host, got %+v, declared %t", bits,
			isDeclared)
	}
	if index, isDeclared := judged.dependence.UnitDeclaringName(dPath, "Size"); !isDeclared ||
		!judged.dependence.Units()[index].Dependent {
		test.Errorf("d's Size: want its unit dependent, its body withheld on the host, declared %t", isDeclared)
	}
}
