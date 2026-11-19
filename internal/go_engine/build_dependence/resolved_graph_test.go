package build_dependence

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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

// A decoded read of an object of a package the graph does not hold, which the answering graph declares no unit of, is
// judged by what the read reaches through the object's type, each read by its own (#gf9qrzf): unit Reader reads
// Opaque, which no unit of the answering graph declares and whose bits are unset, through its type to Inner, set, and
// unit Later, after it, reads Plain, which a unit there declares, unset, through nothing. Reader is dependent through
// Inner, and Later is independent. A read's reach dropped makes Reader independent, and one moved a read along gives
// Reader Later's empty reach.
func TestAnExternalReadIsJudgedByWhatItReachesThroughItsType(test *testing.T) {
	test.Parallel()

	path := "example.com/reaching"
	record := &packageRecord{identifier: "reaching", path: path, imports: map[string]string{},
		objects: []recordedObject{{path: "example.com/elsewhere", local: "Opaque"},
			{path: "example.com/elsewhere", local: "Inner"}, {path: "example.com/elsewhere", local: "Plain"}}}
	record.units = []recordedUnit{
		{file: "reaching.go", start: 10, end: 20, nodes: 3, declares: []recordedObject{{path: path, local: "Reader"}},
			reads: []recordedRead{{target: 0, through: []recordedBit{{object: 1}}}}},
		{file: "reaching.go", start: 30, end: 40, nodes: 3, declares: []recordedObject{{path: path, local: "Later"}},
			reads: []recordedRead{{target: 2}}},
	}
	graph := NewGraph()
	graph.add(record)
	dependence := graph.Against(Difference{
		DependentObject: func(object types.Object) (bool, bool) { return false, false },
		DependentName: func(path string, local string) (bool, bool, bool) {
			return local == "Inner", false, local == "Plain"
		},
	})
	for start, want := range map[int]bool{10: true, 30: false} {
		if got := dependence.UnitDependent("reaching", "reaching.go", start); got != want {
			test.Errorf("reaching.go:%d: want dependent %t, got %t", start, want, got)
		}
	}
}

// Two decoded objects of packages the graph does not hold that share a local name are two external objects, each
// answered by its own path (#gf9qrzf): unit First reads Shared of example.com/first, which DependentName sets, and
// unit Second reads Shared of example.com/second, which it does not. First is dependent and Second is not; keyed by
// the local name alone, the two would share First's answer.
func TestTwoExternalObjectsOfOneLocalNameAreAnsweredApart(test *testing.T) {
	test.Parallel()

	path := "example.com/sharing"
	record := &packageRecord{identifier: "sharing", path: path, imports: map[string]string{},
		objects: []recordedObject{{path: "example.com/first", local: "Shared"},
			{path: "example.com/second", local: "Shared"}}}
	record.units = []recordedUnit{
		{file: "sharing.go", start: 10, end: 20, nodes: 3, declares: []recordedObject{{path: path, local: "First"}},
			reads: []recordedRead{{target: 0}}},
		{file: "sharing.go", start: 30, end: 40, nodes: 3, declares: []recordedObject{{path: path, local: "Second"}},
			reads: []recordedRead{{target: 1}}},
	}
	graph := NewGraph()
	graph.add(record)
	if external := len(graph.resolve().externalObjects); external != 2 {
		test.Errorf("want two external objects, one for each path, got %d", external)
	}
	dependence := graph.Against(Difference{
		DependentObject: func(object types.Object) (bool, bool) { return false, false },
		DependentName: func(path string, local string) (bool, bool, bool) {
			return path == "example.com/first", false, true
		},
	})
	for start, want := range map[int]bool{10: true, 30: false} {
		if got := dependence.UnitDependent("sharing", "sharing.go", start); got != want {
			test.Errorf("sharing.go:%d: want dependent %t, got %t", start, want, got)
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

// An object of a package the graph does not hold, asked of a build judged with no DependentObject, has nothing to be
// answered by, so ObjectBits calls it dependent on both bits, failing wide (#w72zxtq), and an object of the universe,
// the same in every build, has neither.
func TestAnObjectNoGraphAnswersForIsDependent(test *testing.T) {
	test.Parallel()

	elsewhere := types.NewPackage("example.com/elsewhere", "elsewhere")
	moved := types.NewTypeName(token.NoPos, elsewhere, "Moved", nil)
	types.NewNamed(moved, types.Typ[types.Int], nil)
	dependence := NewGraph().Against(Difference{})
	if bits := dependence.ObjectBits(moved); !bits.Name || !bits.Underlying {
		test.Errorf("an object no graph answers for has the bits %+v, want both set", bits)
	}
	if bits := dependence.ObjectBits(types.Universe.Lookup("len")); bits.Name || bits.Underlying {
		test.Errorf("the universe's len has the bits %+v, want none", bits)
	}
}

// parsedPackage is the package identifier of files, each a name and its source, parsed and checked here with no loader,
// as AddPackage takes a loaded package: what a //line directive makes of a file is the parser's, which a fixture
// module's load would give alike. A file may import package unsafe, which needs no export data, and nothing else.
func parsedPackage(test *testing.T, identifier string, files [][2]string) *packages.Package {
	test.Helper()
	fileSet := token.NewFileSet()
	var syntax []*ast.File
	for _, file := range files {
		parsed, parseError := parser.ParseFile(fileSet, file[0], file[1], parser.ParseComments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		syntax = append(syntax, parsed)
	}
	information := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances: map[*ast.Ident]types.Instance{}, Implicits: map[ast.Node]types.Object{}}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		return nil, fmt.Errorf("%s imports %s, and a package parsed here imports only unsafe", identifier, path)
	})}
	checked, checkError := configuration.Check("example.com/"+identifier, fileSet, syntax, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return &packages.Package{ID: identifier, PkgPath: "example.com/" + identifier, Fset: fileSet, Syntax: syntax,
		Types: checked, TypesInfo: information}
}

// expectUnitsFoundAsByMaps fails for every place in graph's units, each one's start, the offsets just inside and
// past its ends, and offsets before and after every unit, where unitStartingAt or unitContaining answers other than
// the maps the resolution held before its file table: one from each unit's package, file and start to the last unit
// written there, and one from each file to its units in the graph's order, read whole for the units whose spans hold
// the offset, of them the one starting last, and of two starting there the later.
func expectUnitsFoundAsByMaps(test *testing.T, label string, graph *Graph) {
	test.Helper()
	resolved := graph.resolve()
	unitIndex := map[unitPlace]int{}
	fileUnits := map[fileKey][]int{}
	for index := range resolved.unitCount() {
		file := fileKey{packageIdentifier: resolved.packages[resolved.unitPackage[index]].identifier,
			file: resolved.files.names[resolved.unitFile[index]]}
		unitIndex[unitPlace{file: file, offset: int(resolved.unitStart[index])}] = index
		fileUnits[file] = append(fileUnits[file], index)
	}
	containing := func(file fileKey, offset int) (int, bool) {
		found, isFound := 0, false
		for _, index := range fileUnits[file] {
			holds := int(resolved.unitStart[index]) <= offset && offset < int(resolved.unitEnd[index])
			if holds && (!isFound || resolved.unitStart[index] >= resolved.unitStart[found]) {
				found, isFound = index, true
			}
		}
		return found, isFound
	}
	for file, units := range fileUnits {
		offsets := []int{0, 1 << 20}
		for _, index := range units {
			start, end := int(resolved.unitStart[index]), int(resolved.unitEnd[index])
			offsets = append(offsets, start-1, start, start+1, end-1, end, end+1)
		}
		for _, offset := range offsets {
			wantIndex, wantFound := unitIndex[unitPlace{file: file, offset: offset}]
			gotIndex, gotFound := resolved.unitStartingAt(file.packageIdentifier, file.file, offset)
			if gotIndex != wantIndex || gotFound != wantFound {
				test.Errorf("%s: %s %s:%d starts unit %d (%t), want %d (%t)", label, file.packageIdentifier, file.file,
					offset, gotIndex, gotFound, wantIndex, wantFound)
			}
			wantIndex, wantFound = containing(file, offset)
			gotIndex, gotFound = resolved.unitContaining(file.packageIdentifier, file.file, offset)
			if gotIndex != wantIndex || gotFound != wantFound {
				test.Errorf("%s: %s %s:%d is in unit %d (%t), want %d (%t)", label, file.packageIdentifier, file.file,
					offset, gotIndex, gotFound, wantIndex, wantFound)
			}
		}
	}
}

// unitPlace is a unit's place as the resolution's map from places held it: its file and its start offset.
type unitPlace struct {
	file   fileKey
	offset int
}

// A file's units are found as the resolution's maps found them however //line directives place them. One file whose
// directives switch between two names interleaves those files' units, so the file table keeps an order of its own;
// two files parsed that name one file put that file's units out of start order, at starts that meet, so UnitDependent
// reads each of them; and the layout fixture, every file's units together and in order, keeps neither. UnitDependent
// answers for a unit of a file only the host compiles, a name //line gave, by that unit's verdict, beside one of a
// file both builds compile, another such name.
func TestUnitsAreFoundAsByMapsWhereverLineDirectivesPlaceThem(test *testing.T) {
	test.Parallel()

	interleaved := NewGraph()
	interleaved.AddPackage(parsedPackage(test, "interleaved", [][2]string{{"generated.go",
		"package interleaved\n\n//line alpha.go:1\nfunc AlphaOne() {}\n\n//line beta.go:1\nfunc BetaOne() {}\n\n" +
			"//line alpha.go:10\nfunc AlphaTwo() {}\n\n//line beta.go:10\nfunc BetaTwo() {}\n"}}), true, nil)
	if resolved := interleaved.resolve(); resolved.files.unitOrder == nil || !resolved.files.inStartOrder {
		test.Errorf("interleaved: want an order of the file table's own, its files in start order, got order %v, "+
			"in start order %t", resolved.files.unitOrder, resolved.files.inStartOrder)
	}
	expectUnitsFoundAsByMaps(test, "interleaved", interleaved)
	dependence := interleaved.Against(Difference{Packages: map[string]PackageDifference{
		"interleaved": {HostOnlyFiles: []string{"alpha.go"}, SharedFiles: []string{"beta.go"}}}})
	for index, unit := range dependence.Units() {
		if got := dependence.UnitDependent(unit.PackageId, unit.File, unit.Start); got != (unit.File == "alpha.go") {
			test.Errorf("interleaved: unit %d, %s:%d, dependent %t", index, unit.File, unit.Start, got)
		}
	}

	shared := NewGraph()
	sharedSource := func(names ...string) string {
		source := "package shared\n\n//line shared.go:1\n"
		for _, name := range names {
			source += "func " + name + "() {}\n"
		}
		return source
	}
	shared.AddPackage(parsedPackage(test, "shared", [][2]string{{"first.go", sharedSource("One", "Two", "Three")},
		{"second.go", sharedSource("Uno", "Dos")}}), true, nil)
	if resolved := shared.resolve(); resolved.files.unitOrder != nil || resolved.files.inStartOrder {
		test.Errorf("shared: want no order of the file table's own, its file out of start order, got order %v, in "+
			"start order %t", resolved.files.unitOrder, resolved.files.inStartOrder)
	}
	expectUnitsFoundAsByMaps(test, "shared", shared)

	layout := graphOf(loadFixture(test, "layout", hostBuild), nil)
	if resolved := layout.resolve(); resolved.files.unitOrder != nil || !resolved.files.inStartOrder {
		test.Errorf("layout: want no order of the file table's own, every file in start order, got order %v, in "+
			"start order %t", resolved.files.unitOrder, resolved.files.inStartOrder)
	}
	expectUnitsFoundAsByMaps(test, "layout", layout)
}

// chainLoadUnder is the build-facts-chain fixture of the engine's testdata, its own module, loaded whole under build
// with its tests: one package of its own and the part of the standard library it reaches. It is not the scratch tests'
// chainLoad, which loads the fixture once and under the host alone: the benchmark judges a windows build on it too.
func chainLoadUnder(benchmark *testing.B, build fixtureBuild) loadedBuild {
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
	host := chainLoadUnder(benchmark, hostBuild)
	windows := chainLoadUnder(benchmark, fixtureBuild{name: "windows", environment: buildEnvironment("windows", "")})
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
