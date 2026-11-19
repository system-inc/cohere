package go_engine

import (
	"encoding/json"
	"fmt"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
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
// for its syntax alone. It shares appendParsed with the engine, the one parse both must make alike, and the scan for a
// line directive in each file of a package both have that the two do not share (build_dependence.HoldsLineDirective).
func libraryDependence(test *testing.T, root string, hostEnvironment []string, buildEnvironment []string) Counts {
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
		group.Go(func() { graph.AddPackage(loaded, workspace(loaded)) })
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
				differing.BuildOnlyFiles = appendParsed(differing.BuildOnlyFiles, fileSet, file)
				differing.LineDirective = differing.LineDirective ||
					isShared && build_dependence.HoldsLineDirective(file, os.ReadFile)
			}
		}
		if isShared {
			for _, file := range hostPackage.GoFiles {
				if slices.Contains(buildPackage.GoFiles, file) {
					differing.SharedFiles = append(differing.SharedFiles, file)
				} else {
					differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
					differing.LineDirective = differing.LineDirective ||
						build_dependence.HoldsLineDirective(file, os.ReadFile)
				}
			}
		}
		if !isShared || len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 {
			difference.Packages[identifier] = differing
		}
	}
	counts := graph.Against(difference).Counts()
	return Counts{Workspace: dependenceCounts(counts.Workspace), Dependencies: dependenceCounts(counts.Dependencies)}
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
// 5, Steady independent there too. The second build to ask about gen_other.go reads the first's scan of it
// (hostDependence.lineDirectives). On windows, with no scan the count is 1, Plain alone, which its host-only file
// seeds, with none of the host-only files 2, Plain and Steady, and with none of the build-only file 5. The run's one
// finding is printf's of Helper, a string on windows and freebsd, formatted with %d.
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
func TestTheEnginesScanAnswersFromTheHeldBytesOnceAndFailsWide(test *testing.T) {
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
func TestTheStandardLibrarysGraphIsJudgedWithTheEnginesScan(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence-line-directive"))
	const identifier = "example.com/dependenceline/p"
	host := loadedFromSource(test, root, "darwin", identifier)
	built := loadedFromSource(test, root, "windows", identifier)
	standardLibrary := build_dependence.NewGraph()
	standardLibrary.AddPackage(host, false)
	// The standard library's graph has ended, as every build waits for it to.
	done := make(chan struct{})
	close(done)
	engine := &pipeline{described: &describedProject{}, dependence: &hostDependence{
		graph:    build_dependence.NewGraph(),
		files:    map[string][]string{},
		paths:    map[string]string{},
		restored: map[string]bool{identifier: true},
		standardLibrary: &standardLibraryDependence{done: done, graph: standardLibrary,
			files: map[string][]string{identifier: host.GoFiles}},
	}}
	record, _ := engine.dependenceOf(&load.Loaded{Roots: []*packages.Package{built}})
	if record == nil || record.Restored == nil {
		test.Fatalf("want a record of the restored standard library's judgment, got %+v", record)
	}
	if record.Restored.StandardLibraryUnits != 3 || record.Restored.StandardLibraryDependentUnits != 3 {
		test.Errorf("want all 3 of the standard library's units dependent on windows, p's Helper, Plain and Shared, "+
			"got %d of %d", record.Restored.StandardLibraryDependentUnits, record.Restored.StandardLibraryUnits)
	}
}

// A package the build checks only as its in-package test variant is judged with the variant's build-only files that
// are the package's own, those not named as test files (#9paaen9). In the build-dependence-variant fixture windows
// alone compiles v's in-package test, so the host checks v and windows checks v [v.test] in its place, and nothing
// imports v, so windows's load leaves v out (load.Caching's AnalyzedRootsOnly). v's shape_windows.go, which windows
// alone compiles, declares String on Shape, a type both builds have, so Shape's method set moves: Shape and Printed,
// which formats it, are dependent, and Doubled, which reads nothing of it, is not. u has an in-package test in every
// build, so the host checks u and u [u.test], and windows u [u.test] alone, and kind_windows.go declares String on
// Kind: the host's u, compared with windows's variant as v is, is judged as u [u.test], compared with its own
// counterpart, is, Kind and Printed dependent in each, Doubled and the test not.
func TestABuildOnlyFileOfAPackageTheBuildChecksAsItsVariantIsJudged(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence-variant"))
	engine, _ := runOverConfigured(test, root, exitClean, nil, "--no-cache", "--build-dependence")
	if len(engine.buildRuns) != 2 || engine.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d builds", len(engine.buildRuns))
	}
	const shapes = "example.com/dependencevariant/v"
	if engine.buildRuns[0].analyzed[shapes+" ["+shapes+".test]"] || !engine.buildRuns[1].analyzed[shapes+" ["+shapes+
		".test]"] {
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
		shapes:                            "2 of 3",
		"example.com/dependencevariant/u": "2 of 3",
		"example.com/dependencevariant/u [example.com/dependencevariant/u.test]": "2 of 4",
	}
	if !maps.Equal(got, want) {
		test.Errorf("the dependent units of each package on windows are %v, want %v", got, want)
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
