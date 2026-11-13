package build_dependence

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// judgedFixture is a fixture's host and build loads and the build's dependence, judged on the graph of every package
// the host loaded.
type judgedFixture struct {
	host       loadedBuild
	build      loadedBuild
	difference Difference
	dependence *Dependence
}

// judge loads fixture under the host and build and judges build on the host's graph.
func judge(test *testing.T, fixture string, build fixtureBuild) judgedFixture {
	test.Helper()
	host, built := loadFixture(test, fixture, hostBuild), loadFixture(test, fixture, build)
	difference := differenceOf(host, built)
	return judgedFixture{host: host, build: built, difference: difference,
		dependence: graphOf(host, nil).Against(difference)}
}

// verdicts are every host unit's verdict, by its name (unitName), each name once across the fixture's packages.
func verdicts(test *testing.T, host loadedBuild, dependence *Dependence) map[string]bool {
	test.Helper()
	judged := map[string]bool{}
	for _, loaded := range host {
		for _, file := range loaded.Syntax {
			tokenFile := loaded.Fset.File(file.FileStart)
			for _, unit := range unitDeclarations(file) {
				name := unitName(unit)
				if _, isNamed := judged[name]; isNamed {
					test.Fatalf("two units are named %q; a fixture names each unit once", name)
				}
				judged[name] = dependence.UnitDependent(loaded.ID, tokenFile.Position(unit.Pos()).Filename,
					tokenFile.Offset(unit.Pos()))
			}
		}
	}
	return judged
}

// expectVerdicts fails for every unit whose verdict is not want's, and for every unit want names that is not there.
func expectVerdicts(test *testing.T, judged map[string]bool, want map[string]bool) {
	test.Helper()
	for _, name := range slices.Sorted(maps.Keys(want)) {
		dependent, isJudged := judged[name]
		switch {
		case !isJudged:
			test.Errorf("%s: no such unit in the host", name)
		case dependent != want[name]:
			test.Errorf("%s: want dependent %t, got %t", name, want[name], dependent)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(judged)) {
		if _, isWanted := want[name]; !isWanted {
			test.Errorf("%s: judged dependent %t, and the test names no verdict for it", name, judged[name])
		}
	}
}

// On linux, the layout fixture's Size is 2 where the host's is 4. Every unit that reads Size by value, or touches the
// underlying of a type whose layout hangs on it, is dependent; a unit that hands such a type on by pointer, uses it by
// name, or merely calls a function whose body reads Size is not. The dependency's Width differs the same way.
func TestLayoutVerdictsOnLinux(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	expectVerdicts(test, verdicts(test, judged.host, judged.dependence), map[string]bool{
		"value Size":             true,
		"func platformName":      true,
		"value Mode":             false,
		"func Length":            true,
		"value Doubled":          true,
		"value Table":            true,
		"func Name":              true,
		"func Describe":          false,
		"type Holder":            true,
		"func CountOf":           true,
		"func Forward":           false,
		"func NewHolder":         true,
		"method Holder.Capacity": true,
		"func PassesHolder":      false,
		"type Shell":             false,
		"func ShellCount":        true,
		"type Inner":             true,
		"type Opaque":            true,
		"func Keep":              false,
		"type Box":               false,
		"func Boxes":             true,
		"func ReadsSize":         true,
		"func CallsReadsSize":    false,
		"func Untouched":         false,
		"func CellOf":            true,
		"func Cells":             true,
		"func Labelled":          false,
		"func CallsWide":         false,
		"func Mapped":            false,
		"value Width":            true,
		"type Cell":              true,
		"value Label":            false,
		"func Wide":              true,
	})
}

// Under the tag build, only Mode's file differs, so only Mode and what reads it are dependent.
func TestLayoutVerdictsUnderATag(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", taggedBuild)
	want := map[string]bool{}
	for name := range verdicts(test, judged.host, judged.dependence) {
		want[name] = false
	}
	want["value Mode"] = true
	want["func Describe"] = true
	expectVerdicts(test, verdicts(test, judged.host, judged.dependence), want)
}

// On linux, Plain gains Describe in a file only the build compiles. Plain's method set moves, so every use of Plain
// counts, its conversion to an interface among them, and so does every use of Wrapper, which embeds Plain and promotes
// Describe, even through a pointer. Asking whether a value implements Describer reads nothing of Plain.
func TestMethodsVerdictsOnLinux(test *testing.T) {
	test.Parallel()

	judged := judge(test, "methods", linuxBuild)
	expectVerdicts(test, verdicts(test, judged.host, judged.dependence), map[string]bool{
		"type Plain":       true,
		"type Describer":   false,
		"func Boxed":       true,
		"func IsDescriber": false,
		"type Wrapper":     true,
		"func Wrapped":     true,
		"type Count":       false,
		"func Counted":     false,
	})
}

// Under the tag build, Count gains Tagged in the tag's file, so Count and what uses it are dependent, and Plain is
// as the host has it.
func TestMethodsVerdictsUnderATag(test *testing.T) {
	test.Parallel()

	judged := judge(test, "methods", taggedBuild)
	expectVerdicts(test, verdicts(test, judged.host, judged.dependence), map[string]bool{
		"type Plain":       false,
		"type Describer":   false,
		"func Boxed":       false,
		"func IsDescriber": false,
		"type Wrapper":     false,
		"func Wrapped":     false,
		"type Count":       true,
		"func Counted":     true,
	})
}

// The unit-level check: for every fixture and build, every unit in a file both builds compile that the judgment calls
// independent records the same types, constants, uses, definitions and selections in the host and in the build, each
// fully loaded. The one difference allowed is the exception class unexportedUntouched names, and each fixture's
// exceptions are pinned exactly, so one that grows fails: on linux, Forward and Shell name Holder, whose unexported
// slots array hangs on Size, and types.ObjectString writes Holder's whole struct, though neither selects slots. Beside
// them a strict case stays pinned: PassesHolder holds a Holder by pointer too, from a constructor in another file,
// never names it, only passes it on and calls a method on it, and records exactly alike, not even the exception. On
// linux the layout fixture's dependent units really differ, which shows the comparison can see a difference.
func TestEveryIndependentUnitRecordsTheSameInBothBuilds(test *testing.T) {
	test.Parallel()

	for _, fixture := range []string{"layout", "methods"} {
		for _, build := range []fixtureBuild{linuxBuild, taggedBuild} {
			test.Run(fixture+" "+build.name, func(test *testing.T) {
				test.Parallel()

				judged := judge(test, fixture, build)
				checked := checkUnits(judged.host, judged.build, judged.dependence)
				test.Logf("compared %d independent units; %d dependent units in shared files record differently",
					checked.compared, checked.dependentDiffer)
				for _, miss := range checked.misses {
					test.Errorf("independent and records differently: %s", miss)
				}
				onLinux := fixture == "layout" && build.name == linuxBuild.name
				var wantUntouched []string
				if onLinux {
					wantUntouched = []string{"func Forward", "type Shell"}
				}
				if !slices.Equal(checked.unexportedUntouched, wantUntouched) {
					test.Errorf("want exactly %d units differing only in unexported fields they never select, %q, "+
						"got %d, %q", len(wantUntouched), wantUntouched, len(checked.unexportedUntouched),
						checked.unexportedUntouched)
				}
				if fixture == "layout" && !slices.Contains(checked.identical, "func PassesHolder") {
					test.Errorf("want PassesHolder compared and recording exactly alike, got the identical units %q",
						checked.identical)
				}
				if checked.compared == 0 {
					test.Error("compared no independent unit")
				}
				if onLinux && checked.dependentDiffer == 0 {
					test.Error("no dependent unit records differently, so the comparison sees nothing")
				}
			})
		}
	}
}

// The layout fixture's counts on linux: its 29 workspace units and the dependency's 4, those dependent, each package
// touched, and the units of the files only linux compiles, counted apart.
func TestLayoutCountsOnLinux(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	counts := judged.dependence.Counts()
	nodes := map[bool]int{}
	for _, loaded := range judged.host {
		for _, file := range loaded.Syntax {
			for _, unit := range unitDeclarations(file) {
				nodes[fixtureWorkspace(loaded)] += countNodes(unit)
			}
		}
	}
	for _, scope := range []struct {
		name   string
		counts ScopeCounts
		want   ScopeCounts
	}{
		{"workspace", counts.Workspace, ScopeCounts{Units: 29, DependentUnits: 17, Nodes: nodes[true], Packages: 1,
			DependentPackages: 1, DependentPackageNodes: nodes[true], BuildOnlyUnits: 2}},
		{"dependencies", counts.Dependencies, ScopeCounts{Units: 4, DependentUnits: 3, Nodes: nodes[false],
			Packages: 1, DependentPackages: 1, DependentPackageNodes: nodes[false], BuildOnlyUnits: 1}},
	} {
		got := scope.counts
		// Dependent and build-only nodes are sums the units' own sizes decide, so they are checked for range here.
		if got.DependentNodes <= 0 || got.DependentNodes >= got.Nodes || got.BuildOnlyNodes <= 0 {
			test.Errorf("%s: want dependent nodes within the scope's and build-only nodes counted, got %+v",
				scope.name, got)
		}
		got.DependentNodes, got.BuildOnlyNodes = 0, 0
		if got != scope.want {
			test.Errorf("%s: want %+v, got %+v", scope.name, scope.want, got)
		}
	}
}

// A graph that does not hold a dependency, as the engine's does not hold one checked without syntax, judges the
// packages it holds as the whole graph does once the whole graph's dependent objects answer for the dependency's, and
// without them calls the units that read the dependency's dependent objects independent.
func TestDependentObjectsAnswerForAPackageTheGraphDoesNotHold(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	isDependency := func(loaded *packages.Package) bool { return !fixtureWorkspace(loaded) }
	partial := graphOf(judged.host, isDependency)
	whole := judged.dependence.DependentObjects()
	answered := judged.difference
	answered.DependentObject = func(object types.Object) (bool, bool) {
		bits := whole[object]
		return bits.Name, bits.Underlying
	}
	withAnswers := verdicts(test, judged.host, partial.Against(answered))
	withoutAnswers := verdicts(test, judged.host, partial.Against(judged.difference))
	wholeVerdicts := verdicts(test, judged.host, judged.dependence)
	for _, loaded := range judged.host {
		if isDependency(loaded) {
			continue
		}
		for _, file := range loaded.Syntax {
			for _, unit := range unitDeclarations(file) {
				name := unitName(unit)
				if withAnswers[name] != wholeVerdicts[name] {
					test.Errorf("%s: want dependent %t as the whole graph judges it, got %t", name,
						wholeVerdicts[name], withAnswers[name])
				}
			}
		}
	}
	for _, name := range []string{"func CellOf", "func Cells"} {
		if withoutAnswers[name] {
			test.Errorf("%s: want it independent without the dependency's answers, got dependent", name)
		}
	}
}

// restoredFromExportData is host's workspace packages checked again against the dependency's types read from export
// data, as the engine's host reads a package it restored from the binary's table: each object the workspace reads of
// the dependency is another than the one the dependency's own load declares, and a graph of these packages holds
// nothing of the dependency.
func restoredFromExportData(test *testing.T, host loadedBuild) loadedBuild {
	test.Helper()
	fileSet := token.NewFileSet()
	imported := map[string]*types.Package{}
	for _, loaded := range host {
		if fixtureWorkspace(loaded) {
			continue
		}
		var exportData bytes.Buffer
		if writeError := gcexportdata.Write(&exportData, loaded.Fset, loaded.Types); writeError != nil {
			test.Fatal(writeError)
		}
		if _, readError := gcexportdata.Read(&exportData, fileSet, imported, loaded.PkgPath); readError != nil {
			test.Fatal(readError)
		}
	}
	restored := loadedBuild{}
	for identifier, loaded := range host {
		if !fixtureWorkspace(loaded) {
			continue
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
		configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
			if read, isRead := imported[path]; isRead {
				return read, nil
			}
			return nil, fmt.Errorf("the fixture's workspace imports %s, which no export data holds", path)
		})}
		checked, checkError := configuration.Check(loaded.PkgPath, loaded.Fset, loaded.Syntax, information)
		if checkError != nil {
			test.Fatal(checkError)
		}
		restored[identifier] = &packages.Package{ID: loaded.ID, PkgPath: loaded.PkgPath, GoFiles: loaded.GoFiles,
			Fset: loaded.Fset, Syntax: loaded.Syntax, Types: checked, TypesInfo: information}
	}
	return restored
}

// importerFunction is a function as a types.Importer.
type importerFunction func(path string) (*types.Package, error)

// Import is the function's answer for path.
func (importer importerFunction) Import(path string) (*types.Package, error) {
	return importer(path)
}

// The engine's host restores the standard library from export data, so its graph holds none of it, and a graph of its
// own, loaded from source, judges it (#ss4t5g6). Here the workspace is checked against the dependency read from export
// data, a graph of the dependency alone is judged on linux, and its judgment answers for each object the workspace
// reads through that object's counterpart, found by its path: every workspace unit's verdict is then the whole
// graph's, CellOf and Cells dependent through Width and Cell's underlying. With no graph to answer, every object read
// fails wide, counted, and Labelled, independent in truth, is called dependent with them.
func TestCounterpartsAnswerForARestoredDependency(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	restored := restoredFromExportData(test, judged.host)
	isWorkspace := func(loaded *packages.Package) bool { return fixtureWorkspace(loaded) }
	dependencyGraph := graphOf(judged.host, isWorkspace)
	dependencyJudged := dependencyGraph.Against(judged.difference)
	restoredGraph := graphOf(restored, nil)
	for _, loaded := range restored {
		for _, imported := range loaded.Types.Imports() {
			if judged.host[imported.Path()] != nil && judged.host[imported.Path()].Types == imported {
				test.Fatalf("%s reads %s as its own load declares it, want it read from export data", loaded.ID,
					imported.Path())
			}
		}
	}

	counterparts := NewCounterparts(dependencyGraph, dependencyJudged)
	answered := judged.difference
	answered.DependentObject = counterparts.DependentObject
	withCounterparts := verdicts(test, restored, restoredGraph.Against(answered))
	wholeVerdicts := verdicts(test, judged.host, judged.dependence)
	for name, dependent := range withCounterparts {
		if dependent != wholeVerdicts[name] {
			test.Errorf("%s: want dependent %t as the whole graph judges it, got %t", name, wholeVerdicts[name],
				dependent)
		}
	}
	for _, name := range []string{"func CellOf", "func Cells"} {
		if !withCounterparts[name] {
			test.Errorf("%s: want it dependent through its counterpart's bits, got independent", name)
		}
	}
	if counts := counterparts.Counts(); counts.Answered == 0 || counts.WithoutCounterpart != 0 ||
		counts.WithoutPackage != 0 {
		test.Errorf("want every object answered by its counterpart, got %+v", counts)
	}

	failingWide := NewCounterparts(nil, nil)
	unanswered := judged.difference
	unanswered.DependentObject = failingWide.DependentObject
	withoutGraph := verdicts(test, restored, restoredGraph.Against(unanswered))
	for _, name := range []string{"func CellOf", "func Cells", "func Labelled"} {
		if !withoutGraph[name] {
			test.Errorf("%s: want it dependent, failing wide with no graph to answer, got independent", name)
		}
	}
	if counts := failingWide.Counts(); counts.WithoutPackage == 0 || counts.Answered != 0 {
		test.Errorf("want every object counted as of a package no graph holds, got %+v", counts)
	}
}

// A unit's selected fields are every field on each of its selections' paths: ShellCount selects Count promoted through
// Shell's embedded Holder, so both count as selected, and a difference in either is never exempt.
func TestPromotedSelectionsSelectEveryFieldOnTheirPath(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	var selected map[string]bool
	for _, loaded := range host {
		for _, file := range loaded.Syntax {
			for _, unit := range unitDeclarations(file) {
				if unitName(unit) == "func ShellCount" {
					selected = selectedFields(fingerprint(loaded.TypesInfo, loaded.Fset, unit))
				}
			}
		}
	}
	for _, name := range []string{"Holder", "Count"} {
		if !selected[name] {
			test.Errorf("want %s selected on ShellCount's path, got %v", name, selected)
		}
	}
	if selected["slots"] {
		test.Errorf("want slots unselected, got %v", selected)
	}
}

// A type name whose struct differs between the builds is exempt only when every differing field is unexported and
// unselected: an exported field differing, or an unexported one the unit selects, is a miss.
func TestOnlyUnselectedUnexportedFieldsAreExempt(test *testing.T) {
	test.Parallel()

	own := types.NewPackage("example.com/own", "own")
	named := func(slotsLength int64, countType types.Type) fingerprintEntry {
		fields := []*types.Var{
			types.NewField(token.NoPos, own, "slots", types.NewArray(types.Typ[types.Int], slotsLength), false),
			types.NewField(token.NoPos, own, "Count", countType, false),
		}
		typeName := types.NewTypeName(token.NoPos, own, "Holder", nil)
		types.NewNamed(typeName, types.NewStruct(fields, nil), nil)
		return fingerprintEntry{text: types.ObjectString(typeName, qualifierByPackagePath), object: typeName}
	}
	hosted := named(4, types.Typ[types.Int])
	for _, exemption := range []struct {
		name     string
		built    fingerprintEntry
		selected map[string]bool
		exempt   bool
	}{
		{"an unselected unexported field", named(2, types.Typ[types.Int]), map[string]bool{}, true},
		{"a selected unexported field", named(2, types.Typ[types.Int]), map[string]bool{"slots": true}, false},
		{"an exported field", named(4, types.Typ[types.Int64]), map[string]bool{}, false},
		{"no difference in any field", named(4, types.Typ[types.Int]), map[string]bool{}, false},
	} {
		if exempt := unexportedUntouched(hosted, exemption.built, exemption.selected); exempt != exemption.exempt {
			test.Errorf("%s: want exempt %t, got %t", exemption.name, exemption.exempt, exempt)
		}
	}
}

// A unit under a //line directive is named by the file a reader sees, as a cgo package's units are named by the .go
// file the cgo tool translated: the layout fixture's Mapped sits in mapped.go under `//line mapped.source:1`, and the
// judgment knows it in mapped.source, at its offset in mapped.go, and by mapped.go's name knows no unit there, which
// it never calls independent.
func TestAUnitIsNamedByTheFileAReaderSees(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	for _, loaded := range judged.host {
		for _, file := range loaded.Syntax {
			tokenFile := loaded.Fset.File(file.FileStart)
			for _, unit := range unitDeclarations(file) {
				if unitName(unit) != "func Mapped" {
					continue
				}
				seen := tokenFile.Position(unit.Pos()).Filename
				if filepath.Base(seen) != "mapped.source" || filepath.Base(tokenFile.Name()) != "mapped.go" {
					test.Fatalf("want Mapped in mapped.go seen in mapped.source, got %s seen in %s", tokenFile.Name(),
						seen)
				}
				offset := tokenFile.Offset(unit.Pos())
				if judged.dependence.UnitDependent(loaded.ID, seen, offset) {
					test.Errorf("want Mapped independent where a reader sees it, %s at %d", seen, offset)
				}
				if !judged.dependence.UnitDependent(loaded.ID, tokenFile.Name(), offset) {
					test.Errorf("want no unit known in %s at %d, which is never read as independent",
						tokenFile.Name(), offset)
				}
				return
			}
		}
	}
	test.Fatal("the layout fixture has no unit Mapped")
}

// Each unit carries the units it calls statically: CallsReadsSize calls ReadsSize, PassesHolder calls Forward,
// NewHolder and the method Capacity, and Untouched calls nothing. An object's position finds its declaring unit, as
// another build's fact on it is matched (#0ghk7vj): Length's name sits in Length's unit, and no unit holds the package
// clause.
func TestUnitsCarryTheirStaticCallsAndHoldTheirObjects(test *testing.T) {
	test.Parallel()

	judged := judge(test, "layout", linuxBuild)
	units := judged.dependence.Units()
	named := map[string]int{}
	for _, loaded := range judged.host {
		for _, file := range loaded.Syntax {
			tokenFile := loaded.Fset.File(file.FileStart)
			for _, unit := range unitDeclarations(file) {
				seen, offset := tokenFile.Position(unit.Pos()).Filename, tokenFile.Offset(unit.Pos())
				index, isUnit := judged.dependence.UnitContaining(loaded.ID, seen, offset)
				if !isUnit || units[index].File != seen || units[index].Start != offset {
					test.Fatalf("%s: want its own unit at %s:%d, got %d (%t)", unitName(unit), seen, offset, index,
						isUnit)
				}
				named[unitName(unit)] = index
			}
			if _, isUnit := judged.dependence.UnitContaining(loaded.ID, tokenFile.Name(),
				tokenFile.Offset(file.Package)); isUnit {
				test.Errorf("a unit holds the package clause of %s", tokenFile.Name())
			}
		}
	}
	callsOf := func(name string) []string {
		var called []string
		for _, callee := range units[named[name]].Calls {
			for calleeName, index := range named {
				if index == callee {
					called = append(called, calleeName)
				}
			}
		}
		slices.Sort(called)
		return called
	}
	for name, want := range map[string][]string{
		"func CallsReadsSize": {"func ReadsSize"},
		"func PassesHolder":   {"func Forward", "func NewHolder", "method Holder.Capacity"},
		"func Untouched":      nil,
	} {
		if got := callsOf(name); !slices.Equal(got, want) {
			test.Errorf("%s: want the calls %q, got %q", name, want, got)
		}
	}
}
