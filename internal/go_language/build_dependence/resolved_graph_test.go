package build_dependence

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
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
// module's load would give alike. A file may import package unsafe, which needs no export data, and each of imported,
// another package parsed here, by its path, and nothing else.
func parsedPackage(
	test *testing.T,
	identifier string,
	files [][2]string,
	imported ...*packages.Package,
) *packages.Package {
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
	imports := map[string]*packages.Package{}
	for _, each := range imported {
		imports[each.PkgPath] = each
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		if each, isImported := imports[path]; isImported {
			return each.Types, nil
		}
		return nil, fmt.Errorf("%s imports %s, and a package parsed here imports only unsafe and the packages it is "+
			"handed", identifier, path)
	})}
	checked, checkError := configuration.Check("example.com/"+identifier, fileSet, syntax, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return &packages.Package{ID: identifier, PkgPath: "example.com/" + identifier, Fset: fileSet, Syntax: syntax,
		Types: checked, TypesInfo: information, Imports: imports}
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

// Units that nest or start one byte apart are found as the resolution's maps found them: in nested.go Outer holds
// Inner, as the cgo tool's export wrapper holds the unit its //line names, so a place past Inner's end is Outer's,
// which only a row marked overlapping reads; and in adjacent.go Second starts one byte after First, so the unit
// starting at First's start is First, and no unit is read from the place after it.
func TestUnitsAreFoundAsByMapsWhereTheyNestOrStartOneByteApart(test *testing.T) {
	test.Parallel()

	path := "example.com/places"
	graph := NewGraph()
	graph.add(&packageRecord{identifier: "places", path: path, imports: map[string]string{},
		units: []recordedUnit{
			{file: "nested.go", start: 0, end: 40, nodes: 3, declares: []recordedObject{{path: path, local: "Outer"}}},
			{file: "nested.go", start: 10, end: 20, nodes: 3, declares: []recordedObject{{path: path, local: "Inner"}}},
			{file: "adjacent.go", start: 50, end: 51, nodes: 1,
				declares: []recordedObject{{path: path, local: "First"}}},
			{file: "adjacent.go", start: 51, end: 60, nodes: 3,
				declares: []recordedObject{{path: path, local: "Second"}}},
		}})
	expectUnitsFoundAsByMaps(test, "places", graph)
}

// declaringUnit is a decoded unit of file at start, five bytes long, that declares local of path and reads reads.
func declaringUnit(path string, file string, start int, local string, reads ...recordedRead) recordedUnit {
	return recordedUnit{file: file, start: start, end: start + 5, nodes: 3,
		declares: []recordedObject{{path: path, local: local}}, reads: reads}
}

// changingRecord is a decoded record of a package of path that declares Stable in stable.go and Changing in
// changing.go, so a difference naming changing.go host-only (changingFiles) seeds Changing alone.
func changingRecord(identifier string, path string) *packageRecord {
	return &packageRecord{identifier: identifier, path: path, imports: map[string]string{},
		units: []recordedUnit{declaringUnit(path, "stable.go", 10, "Stable"),
			declaringUnit(path, "changing.go", 10, "Changing")}}
}

// changingFiles are a changingRecord's files as a build that does not compile changing.go differs from the host.
var changingFiles = PackageDifference{HostOnlyFiles: []string{"changing.go"}, SharedFiles: []string{"stable.go"}}

// expectDependent fails for each start of want, a unit of the package packageId in file, whose verdict is not want's.
func expectDependent(test *testing.T, dependence *Dependence, packageId string, file string, want map[int]bool) {
	test.Helper()
	for start, wantDependent := range want {
		if got := dependence.UnitDependent(packageId, file, start); got != wantDependent {
			test.Errorf("%s %s:%d: want dependent %t, got %t", packageId, file, start, wantDependent, got)
		}
	}
}

// A reader names two objects of one package, the first package by ID, and resolution walks the reader's closure for
// the path once, the second object's lookup reading what the first resolved (resolvedPaths): that index, 0, is a
// package the graph holds like any other. Judged with no DependentObject, as a host that restored nothing judges, an
// object read as of a package the graph does not hold is answered independent, so the reader of Changing, the second
// object, must be dependent, and the reader of Stable not.
func TestALaterLookupOfThePathOfTheFirstPackageStillFindsIt(test *testing.T) {
	test.Parallel()

	dependencyPath := "example.com/dependency"
	readerPath := "example.com/reader"
	graph := NewGraph()
	graph.add(changingRecord("dependency", dependencyPath))
	graph.add(&packageRecord{identifier: "reader", path: readerPath,
		imports: map[string]string{dependencyPath: "dependency"},
		objects: []recordedObject{{path: dependencyPath, local: "Stable"}, {path: dependencyPath, local: "Changing"}},
		units: []recordedUnit{declaringUnit(readerPath, "reader.go", 10, "ReadsStable", recordedRead{target: 0}),
			declaringUnit(readerPath, "reader.go", 20, "ReadsChanging", recordedRead{target: 1})}})
	dependence := graph.Against(Difference{Packages: map[string]PackageDifference{"dependency": changingFiles}})
	expectDependent(test, dependence, "reader", "reader.go", map[int]bool{10: false, 20: true})
}

// A path the graph holds one package of is that package when the reader's import walk cannot reach it, cut short by
// a package the graph does not hold, whose imports no record tells: reader imports middle, which the graph does not
// hold, and which imports dependency. Judged with no DependentObject, a read bound to any package but dependency, none
// of which declares Changing, is dropped, so the reader of Changing must be dependent.
func TestAPathPastAPackageTheGraphDoesNotHoldIsItsOnePackage(test *testing.T) {
	test.Parallel()

	dependencyPath := "example.com/dependency"
	readerPath := "example.com/reader"
	graph := NewGraph()
	graph.add(changingRecord("dependency", dependencyPath))
	graph.add(&packageRecord{identifier: "reader", path: readerPath,
		imports: map[string]string{"example.com/middle": "middle"},
		objects: []recordedObject{{path: dependencyPath, local: "Changing"}},
		units:   []recordedUnit{declaringUnit(readerPath, "reader.go", 10, "ReadsChanging", recordedRead{target: 0})}})
	dependence := graph.Against(Difference{Packages: map[string]PackageDifference{"dependency": changingFiles}})
	expectDependent(test, dependence, "reader", "reader.go", map[int]bool{10: true})
}

// A package and its test variant share a path, and a test binary's closure holds one package of each path, so a read
// of the path is of the package the reader's import closure holds (packageOf), found by walking the records' import
// maps. The external test dependency_test imports first and second, each of which imports shared, which imports the
// test variant of dependency, so the walk must go past the first import it meets, which imports no dependency itself;
// consumer, outside the test binary, imports middle, which imports dependency itself. Only the variant differs, every
// unit of it seeded, as a test file of it the build does not compile holding a //line directive marks it. The graph
// holds the path twice, so the path alone names no package, and judged with no DependentObject, as a host that
// restored nothing judges, a read the walk misses is answered independent: the test's reader of Changing must be
// dependent, and the consumer's not.
func TestATestBinarysReaderReadsTheTestVariantItsClosureHolds(test *testing.T) {
	test.Parallel()

	dependencyPath := "example.com/dependency"
	variant := "dependency [dependency.test]"
	reading := func(identifier string, path string, imports map[string]string) *packageRecord {
		return &packageRecord{identifier: identifier, path: path, imports: imports,
			objects: []recordedObject{{path: dependencyPath, local: "Changing"}},
			units:   []recordedUnit{declaringUnit(path, "reader.go", 10, "ReadsChanging", recordedRead{target: 0})}}
	}
	importing := func(identifier string, path string, imports map[string]string) *packageRecord {
		return &packageRecord{identifier: identifier, path: path, imports: imports}
	}
	graph := NewGraph()
	graph.add(changingRecord("dependency", dependencyPath))
	graph.add(changingRecord(variant, dependencyPath))
	graph.add(importing("middle", "example.com/middle", map[string]string{dependencyPath: "dependency"}))
	graph.add(reading("consumer", "example.com/consumer", map[string]string{"example.com/middle": "middle"}))
	graph.add(importing("shared [dependency.test]", "example.com/shared", map[string]string{dependencyPath: variant}))
	for _, identifier := range []string{"first", "second"} {
		graph.add(importing(identifier+" [dependency.test]", "example.com/"+identifier,
			map[string]string{"example.com/shared": "shared [dependency.test]"}))
	}
	graph.add(reading("dependency_test [dependency.test]", "example.com/dependency_test", map[string]string{
		"example.com/first": "first [dependency.test]", "example.com/second": "second [dependency.test]"}))
	dependence := graph.Against(Difference{Packages: map[string]PackageDifference{
		variant: {EveryUnitDependent: true}}})
	expectDependent(test, dependence, "dependency_test [dependency.test]", "reader.go", map[int]bool{10: true})
	expectDependent(test, dependence, "consumer", "reader.go", map[int]bool{10: false})
}

// UnitDeclaringName finds no unit for a path the graph holds no package of, nor for one it holds several of, which
// no path alone can choose between, and finds the unit of a path it holds once: dependency and its test variant share
// a path, dependency is the first package by ID, and reader's path is its own.
func TestNoUnitDeclaresANameOfAPathTheGraphHoldsNoneOrSeveralOf(test *testing.T) {
	test.Parallel()

	dependencyPath := "example.com/dependency"
	graph := NewGraph()
	graph.add(changingRecord("dependency", dependencyPath))
	graph.add(changingRecord("dependency [dependency.test]", dependencyPath))
	graph.add(&packageRecord{identifier: "reader", path: "example.com/reader", imports: map[string]string{},
		units: []recordedUnit{declaringUnit("example.com/reader", "reader.go", 10, "Reader")}})
	dependence := graph.Against(Difference{})
	for _, path := range []string{"example.com/absent", dependencyPath} {
		if index, isDeclared := dependence.UnitDeclaringName(path, "Stable"); isDeclared {
			test.Errorf("%s Stable: want no declaring unit, got unit %d", path, index)
		}
	}
	if index, isDeclared := dependence.UnitDeclaringName("example.com/reader", "Reader"); !isDeclared || index != 4 {
		test.Errorf("example.com/reader Reader: want unit 4, got unit %d (%t)", index, isDeclared)
	}
}

// A read of a member of a held package that no unit declares, a field of an anonymous struct, is read through its
// type, each bit it reaches its own (resolveRead): holder's Layout has its underlying bit set, by a kind edge to the
// layout of Inner, which a host-only file declares, and its name bit unset. Reader reads Field through Layout's
// underlying bit, dependent, and through its name bit, not; and reads Member, which a unit declares, by its own bit
// alone, unset, though its record holds a reach through Layout's layout. Judged with no DependentObject, as a host
// that restored nothing judges, a held object read as another graph's is answered independent. Wrapper's kind edge to
// Field, which no unit declares, is dropped, as the counter dropped every edge to an object without a node, and asks
// nothing of another graph.
func TestAnUndeclaredMemberOfAHeldPackageReadsThroughEachBitOfItsType(test *testing.T) {
	test.Parallel()

	holderPath := "example.com/holder"
	readerPath := "example.com/reader"
	graph := NewGraph()
	graph.add(&packageRecord{identifier: "holder", path: holderPath, imports: map[string]string{},
		objects: []recordedObject{{path: holderPath, local: "Inner"}},
		units: []recordedUnit{declaringUnit(holderPath, "inner.go", 10, "Inner"),
			declaringUnit(holderPath, "holder.go", 10, "Layout"), declaringUnit(holderPath, "holder.go", 20, "Member")},
		edges: []recordedEdge{{from: "Layout", fromUnderlying: true, to: 0, toUnderlying: true}}})
	layoutBit := []recordedBit{{object: 1, underlying: true}}
	graph.add(&packageRecord{identifier: "reader", path: readerPath, imports: map[string]string{holderPath: "holder"},
		objects: []recordedObject{{path: holderPath, local: "Field"}, {path: holderPath, local: "Layout"},
			{path: holderPath, local: "Member"}},
		units: []recordedUnit{
			declaringUnit(readerPath, "reader.go", 10, "ThroughLayout", recordedRead{target: 0, through: layoutBit}),
			declaringUnit(readerPath, "reader.go", 20, "ThroughName",
				recordedRead{target: 0, through: []recordedBit{{object: 1}}}),
			declaringUnit(readerPath, "reader.go", 30, "ReadsMember", recordedRead{target: 2, through: layoutBit}),
			declaringUnit(readerPath, "reader.go", 40, "Wrapper"),
		},
		edges: []recordedEdge{{from: "Wrapper", fromUnderlying: true, to: 0}}})
	if external := len(graph.resolve().externalObjects); external != 0 {
		test.Errorf("want no external object, every object being holder's, got %d", external)
	}
	dependence := graph.Against(Difference{Packages: map[string]PackageDifference{
		"holder": {HostOnlyFiles: []string{"inner.go"}, SharedFiles: []string{"holder.go"}}}})
	expectDependent(test, dependence, "reader", "reader.go", map[int]bool{10: true, 20: false, 30: false, 40: false})
}

// An edge from a declaration to an object of a package the graph does not hold seeds the bit it leaves from when the
// bit it reaches is set in the build: Size's name bit hangs on the layout of elsewhere's Layout, which the build
// changes, and Count's on the layout of Stable, whose name alone the build changes. The reader of Size is dependent,
// and the reader of Count is not.
func TestAnExternalEdgeSeedsTheBitItLeavesWhenTheBitItReachesIsSet(test *testing.T) {
	test.Parallel()

	path := "example.com/sizes"
	graph := NewGraph()
	graph.add(&packageRecord{identifier: "sizes", path: path, imports: map[string]string{},
		objects: []recordedObject{{path: "example.com/elsewhere", local: "Layout"},
			{path: "example.com/elsewhere", local: "Stable"}, {path: path, local: "Size"},
			{path: path, local: "Count"}},
		units: []recordedUnit{declaringUnit(path, "sizes.go", 10, "Size"), declaringUnit(path, "sizes.go", 20, "Count"),
			declaringUnit(path, "sizes.go", 30, "ReadsSize", recordedRead{target: 2}),
			declaringUnit(path, "sizes.go", 40, "ReadsCount", recordedRead{target: 3})},
		edges: []recordedEdge{{from: "Size", to: 0, toUnderlying: true}, {from: "Count", to: 1, toUnderlying: true}}})
	dependence := graph.Against(Difference{
		DependentObject: func(object types.Object) (bool, bool) { return false, false },
		DependentName: func(path string, local string) (bool, bool, bool) {
			return local == "Stable", local == "Layout", true
		},
	})
	expectDependent(test, dependence, "sizes", "sizes.go", map[int]bool{30: true, 40: false})
}

// A read that reaches an object of a package the graph does not hold through its type asks that object's own bit,
// the one it reaches: ThroughLayout reads an object of its own package no unit declares through the layout of
// elsewhere's Layout, which the build changes, and ThroughNamed through the layout of Named, whose name alone the build
// changes. ReadsMember reads Member, which the answering graph declares, unchanged, so it is judged by Member's own
// bits, whatever its record holds of Member's reach through Layout. Only ThroughLayout is dependent.
func TestAReadThroughAnotherGraphsObjectAsksTheBitItReaches(test *testing.T) {
	test.Parallel()

	path := "example.com/reaching"
	graph := NewGraph()
	graph.add(&packageRecord{identifier: "reaching", path: path, imports: map[string]string{},
		objects: []recordedObject{{path: "example.com/elsewhere", local: "Layout"},
			{path: "example.com/elsewhere", local: "Named"}, {path: "example.com/elsewhere", local: "Member"}},
		units: []recordedUnit{
			declaringUnit(path, "reaching.go", 10, "ThroughLayout",
				recordedRead{target: -1, through: []recordedBit{{object: 0, underlying: true}}}),
			declaringUnit(path, "reaching.go", 20, "ThroughNamed",
				recordedRead{target: -1, through: []recordedBit{{object: 1, underlying: true}}}),
			declaringUnit(path, "reaching.go", 30, "ReadsMember",
				recordedRead{target: 2, through: []recordedBit{{object: 0, underlying: true}}}),
		}})
	dependence := graph.Against(Difference{
		DependentObject: func(object types.Object) (bool, bool) { return false, false },
		DependentName: func(path string, local string) (bool, bool, bool) {
			return local == "Named", local == "Layout", true
		},
	})
	expectDependent(test, dependence, "reaching", "reaching.go", map[int]bool{10: true, 20: false, 30: false})
}

// A build-only file's method on a type the host defines moves that type's method set and no other: Receiver is a
// host method's receiver, so it is defined, no alias, and seedAliasTargets follows nothing through it, though its
// name bit hangs on Embedded's, as a type's method set hangs on what it embeds. The reader of Receiver is dependent,
// and the reader of Embedded is not.
func TestABuildOnlyMethodOnAHostReceiverSeedsNothingItEmbeds(test *testing.T) {
	test.Parallel()

	path := "example.com/methods"
	method := declaringUnit(path, "methods.go", 30, "Receiver.Method")
	method.receiver = "Receiver"
	graph := NewGraph()
	graph.add(&packageRecord{identifier: "methods", path: path, imports: map[string]string{},
		typeNames: []string{"Embedded", "Receiver"},
		objects:   []recordedObject{{path: path, local: "Embedded"}, {path: path, local: "Receiver"}},
		units: []recordedUnit{declaringUnit(path, "methods.go", 10, "Embedded"),
			declaringUnit(path, "methods.go", 20, "Receiver"), method,
			declaringUnit(path, "methods.go", 40, "ReadsEmbedded", recordedRead{target: 0}),
			declaringUnit(path, "methods.go", 50, "ReadsReceiver", recordedRead{target: 1})},
		edges: []recordedEdge{{from: "Receiver", to: 0}}})
	buildOnly, parseError := parser.ParseFile(token.NewFileSet(), "extra_linux.go",
		"package methods\n\nfunc (Receiver) Extra() {}\n", 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	dependence := graph.Against(Difference{Packages: map[string]PackageDifference{
		"methods": {BuildOnlyFiles: []*ast.File{buildOnly}}}})
	expectDependent(test, dependence, "methods", "methods.go", map[int]bool{40: false, 50: true})
}

// Units come in package ID order, and each unit's calls are the units declaring its callees, never itself: alpha's one
// unit, the graph's unit 0, calls itself and beta's one unit, unit 1, the first unit of the next package.
func TestUnitsComeInPackageOrderAndACallReachesTheNextPackagesFirstUnit(test *testing.T) {
	test.Parallel()

	alphaPath := "example.com/alpha"
	betaPath := "example.com/beta"
	caller := declaringUnit(alphaPath, "alpha.go", 10, "Alpha")
	caller.callees = []int32{0, 1}
	graph := NewGraph()
	graph.add(&packageRecord{identifier: "beta", path: betaPath, imports: map[string]string{},
		units: []recordedUnit{declaringUnit(betaPath, "beta.go", 10, "Beta")}})
	graph.add(&packageRecord{identifier: "alpha", path: alphaPath, imports: map[string]string{betaPath: "beta"},
		objects: []recordedObject{{path: alphaPath, local: "Alpha"}, {path: betaPath, local: "Beta"}},
		units:   []recordedUnit{caller}})
	units := graph.Against(Difference{}).Units()
	if len(units) != 2 || units[0].PackageId != "alpha" || units[1].PackageId != "beta" {
		test.Fatalf("want alpha's unit and then beta's, got %+v", units)
	}
	if !slices.Equal(units[0].Calls, []int{1}) {
		test.Errorf("alpha's unit calls %v, want [1]", units[0].Calls)
	}
}

// chainLoadUnder is the build-facts-chain fixture of cohere-go's testdata, its own module, loaded whole under build
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
