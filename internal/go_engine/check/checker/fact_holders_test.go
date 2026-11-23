package checker

import (
	"fmt"
	"go/constant"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// markable is every object declaringPackage declares that a fact can be on, in a fixed order: each package-level
// object, and every field of a struct type and every method of a named type among them.
func markable(declaringPackage *types.Package) []types.Object {
	var objects []types.Object
	for _, name := range declaringPackage.Scope().Names() {
		object := declaringPackage.Scope().Lookup(name)
		objects = append(objects, object)
		typeName, isTypeName := object.(*types.TypeName)
		if !isTypeName {
			continue
		}
		named, isNamed := typeName.Type().(*types.Named)
		if !isNamed {
			continue
		}
		if structure, isStruct := named.Underlying().(*types.Struct); isStruct {
			for index := range structure.NumFields() {
				objects = append(objects, structure.Field(index))
			}
		}
		for index := range named.NumMethods() {
			objects = append(objects, named.Method(index))
		}
	}
	return objects
}

// importClosure is every package importingPackage imports, directly or not, sorted by path.
func importClosure(importingPackage *types.Package) []*types.Package {
	seen := map[*types.Package]bool{}
	var closure []*types.Package
	var visit func(imports []*types.Package)
	visit = func(imports []*types.Package) {
		for _, imported := range imports {
			if !seen[imported] {
				seen[imported] = true
				closure = append(closure, imported)
				visit(imported.Imports())
			}
		}
	}
	visit(importingPackage.Imports())
	sort.Slice(closure, func(left, right int) bool { return closure[left].Path() < closure[right].Path() })
	return closure
}

// markingAnalyzer marks every object its package declares, then asks the fact of every object of every package
// below it, and reports what it found and what it missed, so a fact read from the wrong action, or not at all, is a
// finding that differs.
var markingAnalyzer = &analysis.Analyzer{
	Name:      "marking",
	Doc:       "marking marks every object a package declares and reports which marks of the packages below it it reads",
	FactTypes: []analysis.Fact{new(markFact)},
	Run: func(pass *analysis.Pass) (any, error) {
		for _, object := range markable(pass.Pkg) {
			pass.ExportObjectFact(object, &markFact{Of: types.ObjectString(object, nil)})
		}
		for _, below := range importClosure(pass.Pkg) {
			for _, object := range markable(below) {
				var found markFact
				if pass.ImportObjectFact(object, &found) {
					pass.Reportf(pass.Files[0].Package, "found %v", &found)
				} else {
					pass.Reportf(pass.Files[0].Package, "missing on %s", types.ObjectString(object, nil))
				}
			}
		}
		return nil, nil
	},
}

// factSummary is every action of graph, by package, with each finding and every fact it lists and exports, each as
// text, sorted, so two runs over the same packages compare line by line.
func factSummary(graph *Graph) []string {
	var lines []string
	for action := range graph.All() {
		prefix := action.Analyzer.Name + "@" + action.Package.ID
		if action.Err != nil {
			lines = append(lines, prefix+" error "+action.Err.Error())
		}
		for _, diagnostic := range action.Diagnostics {
			lines = append(lines, prefix+" "+diagnostic.Message)
		}
		for _, fact := range action.AllObjectFacts() {
			lines = append(lines, fmt.Sprintf("%s lists %s: %v", prefix, types.ObjectString(fact.Object, nil),
				fact.Fact))
		}
		exported, _ := action.ExportedFacts()
		for _, fact := range exported {
			lines = append(lines, fmt.Sprintf("%s exports %s: %v", prefix, types.ObjectString(fact.Object, nil),
				fact.Fact))
		}
	}
	slices.Sort(lines)
	return lines
}

// marksModule is four packages: bottom declares an object of every kind a fact can be on, middle imports it, top
// imports only middle, so whatever reaches top of bottom's crosses two edges, and beside imports top and bottom both,
// so it reaches bottom directly and through the chain.
var marksModule = map[string]string{
	"bottom/bottom.go": "package bottom\n\n" +
		"type Shape struct{ Side int }\n\nfunc (Shape) Area() int { return 0 }\n\n" +
		"const Sides = 4\n\nvar Count int\n\nvar hidden int\n\nfunc Draw() {}\n\nfunc erase() {}\n",
	"middle/middle.go": "package middle\n\nimport \"example.com/marks/bottom\"\n\n" +
		"type Frame struct{ Inner bottom.Shape }\n\nfunc Paint() { bottom.Draw() }\n",
	"top/top.go": "package top\n\nimport \"example.com/marks/middle\"\n\nfunc Hang() { middle.Paint() }\n",
	"beside/beside.go": "package beside\n\n" +
		"import (\n\t\"example.com/marks/bottom\"\n\t\"example.com/marks/top\"\n)\n\n" +
		"func Look() { top.Hang(); bottom.Draw() }\n",
}

// Every action reads, lists and exports the same facts as the copy gives it, which SanityCheck keeps as upstream's:
// a method's, a field's, a type's and a constant's from two edges away, read where they were exported, and a
// function's or a variable's only from the package that declared it.
func TestEveryActionReadsTheFactsTheCopyWouldHaveGivenIt(test *testing.T) {
	test.Parallel()

	copied := factSummary(analyzeWithin(test, []*analysis.Analyzer{markingAnalyzer},
		writeModule(test, "marks", marksModule, "./..."), &Options{SanityCheck: true}))
	held := factSummary(analyzeWithin(test, []*analysis.Analyzer{markingAnalyzer},
		writeModule(test, "marks", marksModule, "./..."), &Options{}))

	// Two runs that both read nothing would compare equal, so what top reads of bottom two edges down is held to
	// what exportedFrom passes, and to what it lists.
	for _, want := range []string{
		"marking@example.com/marks/top found mark of func (example.com/marks/bottom.Shape).Area() int",
		"marking@example.com/marks/top found mark of field Side int",
		"marking@example.com/marks/top found mark of type example.com/marks/bottom.Shape struct{Side int}",
		"marking@example.com/marks/top found mark of const example.com/marks/bottom.Sides untyped int",
		"marking@example.com/marks/top missing on func example.com/marks/bottom.Draw()",
		"marking@example.com/marks/top missing on var example.com/marks/bottom.Count int",
		"marking@example.com/marks/beside found mark of func example.com/marks/bottom.Draw()",
		"marking@example.com/marks/beside missing on func example.com/marks/bottom.erase()",
		"marking@example.com/marks/top lists func (example.com/marks/bottom.Shape).Area() int: " +
			"mark of func (example.com/marks/bottom.Shape).Area() int",
	} {
		if !slices.Contains(held, want) {
			test.Errorf("want %q, got\n%s", want, strings.Join(held, "\n"))
		}
	}
	if !slices.Equal(copied, held) {
		for _, line := range copied {
			if !slices.Contains(held, line) {
				test.Errorf("only the copy has: %s", line)
			}
		}
		for _, line := range held {
			if !slices.Contains(copied, line) {
				test.Errorf("only the holders have: %s", line)
			}
		}
		test.Errorf("want the same %d lines from both, got %d copied and %d held", len(copied), len(copied),
			len(held))
	}
}

// heldMark is what action reads of object, a mark's name, or "none" when it finds no mark, together with every
// mark it lists and exports, so one line says what each of the three gives.
func heldMark(action *Action, object types.Object) string {
	read := "none"
	var found markFact
	if action.ObjectFact(object, &found) {
		read = found.Of
	}
	var listed []string
	var exported []string
	for _, fact := range action.AllObjectFacts() {
		listed = append(listed, fmt.Sprint(fact.Fact))
	}
	exportedFacts, _ := action.ExportedFacts()
	for _, fact := range exportedFacts {
		exported = append(exported, fmt.Sprint(fact.Fact))
	}
	slices.Sort(listed)
	slices.Sort(exported)
	return fmt.Sprintf("reads %s, lists %v, exports %v", read, listed, exported)
}

// Where several actions share one types.Package, each holding its own fact on one field, an action reads the one the
// copy would have kept: its own over any inherited, and of two inherited the later dependency's, however far below
// it the holder sits; and an action on that types.Package exports the inherited fact as its own, as the copy's
// would. Under SanityCheck, the copy itself, every line is the same.
func TestOfSeveralHoldersTheFactTheCopyWouldHaveKeptWins(test *testing.T) {
	test.Parallel()

	shared := types.NewPackage("example.com/shared", "shared")
	field := types.NewField(0, shared, "Field", types.Typ[types.Int], false)
	marking := &analysis.Analyzer{Name: "marking", FactTypes: []analysis.Fact{new(markFact)}}
	for _, sanityCheck := range []bool{false, true} {
		options := &Options{SanityCheck: sanityCheck}
		// inherited is an action on actionPackage that has inherited from deps in order, as execOnce has it inherit.
		inherited := func(actionPackage *types.Package, deps ...*Action) *Action {
			actionOn := &packages.Package{ID: actionPackage.Path(), Types: actionPackage}
			action := &Action{Analyzer: marking, Package: actionOn, Deps: deps, opts: options,
				objectFacts: map[objectFactKey]analysis.Fact{}}
			for _, dep := range deps {
				inheritFacts(action, dep)
			}
			return action
		}
		early, late := inherited(shared), inherited(shared)
		early.RestoreObjectFact(field, &markFact{Of: "early"})
		late.RestoreObjectFact(field, &markFact{Of: "late"})
		// Through middle, late's fact reaches top first; early's, a direct dependency listed after middle, wins.
		middle := inherited(types.NewPackage("example.com/middle", "middle"), late)
		top := inherited(types.NewPackage("example.com/top", "top"), middle, early)
		// An heir on the shared package itself inherits early's fact on an object of its own package.
		heir := inherited(shared, late, early)
		for _, held := range []struct {
			name   string
			action *Action
			want   string
		}{
			{"middle", middle, "reads late, lists [mark of late], exports []"},
			{"top", top, "reads early, lists [mark of early], exports []"},
			{"the heir", heir, "reads early, lists [mark of early], exports [mark of early]"},
		} {
			if got := heldMark(held.action, field); got != held.want {
				test.Errorf("SanityCheck %v: want %s %s, got %s", sanityCheck, held.name, held.want, got)
			}
		}
		heir.RestoreObjectFact(field, &markFact{Of: "own"})
		if got, want := heldMark(heir, field), "reads own, lists [mark of own], exports [mark of own]"; got != want {
			test.Errorf("SanityCheck %v: want the heir's own fact over what it inherited, %s, got %s", sanityCheck,
				want, got)
		}
		// The heir held inherited facts on its own package before it restored its own, so only its going first among
		// those holders gives an action above it the heir's fact rather than early's.
		above := inherited(types.NewPackage("example.com/above", "above"), heir)
		if got, want := heldMark(above, field), "reads own, lists [mark of own], exports []"; got != want {
			test.Errorf("SanityCheck %v: want the action above the heir to read the heir's own fact, %s, got %s",
				sanityCheck, want, got)
		}
	}
}

// A fact that passes every edge is held where it was exported, never copied: two edges above bottom, top's own map
// holds no key of bottom's method, field, type or constant facts, which it reads all the same, while under SanityCheck
// the copy puts every one of them there. So a change that copies them again fails here, whatever it reads.
func TestAFactThatPassesEveryEdgeIsHeldNotCopied(test *testing.T) {
	test.Parallel()

	for _, sanityCheck := range []bool{false, true} {
		graph := analyzeWithin(test, []*analysis.Analyzer{markingAnalyzer},
			writeModule(test, "marks", marksModule, "./..."), &Options{SanityCheck: sanityCheck})
		copied := -1
		for action := range graph.All() {
			if action.Package.PkgPath != "example.com/marks/top" {
				continue
			}
			copied = 0
			for key := range action.objectFacts {
				if inheritedEverywhere(key.obj) && key.obj.Pkg() != action.Package.Types {
					copied++
				}
			}
		}
		// bottom's Shape, its Side and Area, and Sides; middle's Frame and its Inner.
		want := 0
		if sanityCheck {
			want = 6
		}
		if copied != want {
			test.Errorf("SanityCheck %v: want %d facts of other packages' methods, fields, types and constants in "+
				"top's own map, got %d", sanityCheck, want, copied)
		}
	}
}

// inheritedEverywhere names exactly the objects exportedFrom passes along an edge to a package that did not declare
// them, so what the checker leaves uncopied is what the copy would have passed everywhere.
func TestInheritedEverywhereIsWhatExportedFromPassesFromAnyPackage(test *testing.T) {
	test.Parallel()

	declaring := types.NewPackage("example.com/declaring", "declaring")
	elsewhere := types.NewPackage("example.com/elsewhere", "elsewhere")
	named := types.NewNamed(types.NewTypeName(0, declaring, "Named", nil), types.NewStruct(nil, nil), nil)
	method := types.NewFunc(0, declaring, "Method", types.NewSignatureType(types.NewVar(0, declaring, "receiver",
		named), nil, nil, nil, nil, false))
	for _, object := range []types.Object{
		named.Obj(),
		method,
		types.NewField(0, declaring, "Field", types.Typ[types.Int], false),
		types.NewConst(0, declaring, "Constant", types.Typ[types.Int], constant.MakeInt64(1)),
		types.NewFunc(0, declaring, "Function", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewFunc(0, declaring, "function", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewVar(0, declaring, "Variable", types.Typ[types.Int]),
		types.NewLabel(0, declaring, "label"),
		types.NewPkgName(0, declaring, "name", elsewhere),
	} {
		if everywhere, passed := inheritedEverywhere(object), exportedFrom(object, elsewhere); everywhere != passed {
			test.Errorf("%s: inheritedEverywhere %v, exportedFrom another package %v", object, everywhere, passed)
		}
	}
}
