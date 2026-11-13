package go_engine

import (
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/contract"
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
// for its syntax alone. It shares appendParsed with the engine, the one parse both must make alike.
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
			}
		}
		if isShared {
			for _, file := range hostPackage.GoFiles {
				if !slices.Contains(buildPackage.GoFiles, file) {
					differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
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

// Under `--other-builds --build-dependence`, the windows build's record holds the counts the library's own judgment
// gives the same fixture, loaded whole: of the workspace's four units, Separator, which reads os.PathSeparator, a
// dependency's constant windows declares apart, Mode, which reads the module's own platform constant, and that
// constant, in a file windows does not compile, are dependent, and Joined is not. The host's record writes none. The
// findings are the run's without the switch, line for line, and that run builds no graph and writes no counts.
func TestEachBuildsDependenceIsTheLibrarysJudgment(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "build-dependence"))
	on, onStream := runOver(test, root, exitClean, "--other-builds", "--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean,
		"--other-builds")

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

// `--build-dependence` without `--other-builds` has no extra build to judge, so it builds no graph and the run's
// stream is the plain run's.
func TestBuildDependenceAloneChangesNothing(test *testing.T) {
	test.Parallel()

	switched, switchedStream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")),
		exitClean, "--build-dependence")
	_, plainStream := runOver(test, copyFixture(test, filepath.Join("testdata", "build-dependence")), exitClean)
	if switched.dependence != nil {
		test.Error("the switch without --other-builds built the host's graph")
	}
	if switchedStream != plainStream {
		test.Errorf("the stream with the switch alone is\n%s\nwant the plain run's\n%s", switchedStream, plainStream)
	}
}
