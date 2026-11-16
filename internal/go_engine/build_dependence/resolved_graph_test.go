package build_dependence

import (
	"fmt"
	"go/types"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

// sameStartRecord is a decoded package's record of two units of one file that start at the same offset, the second
// ending later, where only the unit at dependentUnit reads an object of a package the graph does not hold.
func sameStartRecord(identifier string, dependentUnit int) *packageRecord {
	path := "example.com/" + identifier
	record := &packageRecord{identifier: identifier, path: path, imports: map[string]string{},
		objects: []recordedObject{{path: "example.com/elsewhere", local: "Moved"}}}
	for index, local := range []string{"First", "Second"} {
		unit := recordedUnit{file: "same.go", start: 10, end: 20 + 10*index, nodes: 3,
			declares: []recordedObject{{path: path, local: local}}}
		if index == dependentUnit {
			unit.reads = []recordedRead{{target: 0}}
		}
		record.units = append(record.units, unit)
	}
	return record
}

// Two units of one file that start at the same offset are one place to UnitDependent, which answers for the later of
// the two, in either order of their verdicts: Moved, of a package the graph does not hold, is dependent, and only the
// unit that reads it is.
func TestTheLaterOfTwoUnitsAtOneStartAnswersForThePlace(test *testing.T) {
	test.Parallel()

	graph := NewGraph()
	graph.add(sameStartRecord("later", 1))
	graph.add(sameStartRecord("earlier", 0))
	dependence := graph.Against(Difference{
		DependentObject: func(object types.Object) (bool, bool) { return false, false },
		DependentName: func(path string, local string) (bool, bool, bool) {
			return path == "example.com/elsewhere" && local == "Moved", false, true
		},
	})
	for identifier, want := range map[string]bool{"later": true, "earlier": false} {
		if got := dependence.UnitDependent(identifier, "same.go", 10); got != want {
			test.Errorf("%s same.go:10: want dependent %t, the later unit's verdict, got %t", identifier, want, got)
		}
	}
}

// A build's judgment asks its DependentObject once per object, whether a unit reads the object or ObjectBits meets it
// after, and its DependentName once per name: the layout fixture's workspace, restored from export data, judged on
// linux by its dependency's graph, from its live packages and from their records read back, then asked ObjectBits of
// every object the workspace's units read of the dependency.
func TestABuildAsksOfEachObjectOnce(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	restored := restoredFromExportData(test, judged.host)
	dependencyGraph := graphOf(judged.host, func(loaded *packages.Package) bool { return fixtureWorkspace(loaded) })
	counterparts := NewCounterparts(dependencyGraph, dependencyGraph.Against(judged.difference))
	live := graphOf(restored, nil)
	for _, graph := range []*Graph{live, recordedGraphOf(test, live)} {
		objectsAsked := map[types.Object]int{}
		namesAsked := map[ObjectName]int{}
		difference := judged.difference
		difference.DependentObject = func(object types.Object) (bool, bool) {
			objectsAsked[object]++
			return counterparts.DependentObject(object)
		}
		difference.DependentName = func(path string, local string) (bool, bool, bool) {
			namesAsked[ObjectName{Path: path, Local: local}]++
			return counterparts.DependentName(path, local)
		}
		dependence := graph.Against(difference)
		for _, external := range graph.resolve().externalObjects {
			if external.object != nil {
				dependence.ObjectBits(external.object)
			}
		}
		if len(objectsAsked)+len(namesAsked) == 0 {
			test.Fatal("the build asked of no object, so the case proves nothing")
		}
		for object, asked := range objectsAsked {
			if asked > 1 {
				test.Errorf("DependentObject was asked of %s %d times", object, asked)
			}
		}
		for name, asked := range namesAsked {
			if asked > 1 {
				test.Errorf("DependentName was asked of %v %d times", name, asked)
			}
		}
	}
}

// chainLoad is the build-facts-chain fixture of the engine's testdata, its own module, loaded whole under build with
// its tests: one package of its own and the part of the standard library it reaches.
func chainLoad(benchmark *testing.B, build fixtureBuild) loadedBuild {
	benchmark.Helper()
	roots, loadError := packages.Load(&packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo,
		Tests: true,
		Dir:   filepath.Join("..", "testdata", "build-facts-chain"),
		Env:   build.environment,
	}, "./...")
	if loadError != nil {
		benchmark.Fatal(loadError)
	}
	loaded := loadedBuild{}
	packages.Visit(roots, nil, func(visited *packages.Package) { loaded[visited.ID] = visited })
	for _, visited := range loaded {
		if len(visited.Errors) > 0 {
			benchmark.Fatal(fmt.Errorf("%s under %s: %v", visited.ID, build.name, visited.Errors))
		}
	}
	return loaded
}

// The time and memory to resolve a graph and to judge a build on it, both of which sit before every build's verdict:
// every package the build-facts-chain fixture reaches on one graph, resolved anew each round, and windows, where the
// standard library's files differ most, judged on the resolution each round.
func BenchmarkResolveAndAgainstOnTheChainFixture(benchmark *testing.B) {
	host := chainLoad(benchmark, hostBuild)
	windows := chainLoad(benchmark, fixtureBuild{name: "windows", environment: buildEnvironment("windows", "")})
	graph := graphOf(host, nil)
	difference := differenceOf(host, windows)
	benchmark.Run("resolve", func(benchmark *testing.B) {
		for benchmark.Loop() {
			graph.lock.Lock()
			graph.resolved = nil
			graph.lock.Unlock()
			graph.resolve()
		}
	})
	benchmark.Run("against", func(benchmark *testing.B) {
		graph.resolve()
		for benchmark.Loop() {
			graph.Against(difference)
		}
	})
}
