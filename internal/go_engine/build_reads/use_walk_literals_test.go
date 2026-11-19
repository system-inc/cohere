package build_reads

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"runtime"
	"strings"
	"testing"
)

// literalElements is how many elements the table literal of TestWalkingALiteralsElementsHoldsNoContextForEach holds:
// enough that a context held for each, about 80 bytes and a map entry, comes to megabytes.
const literalElements = 10_000

// literalWalkBudget is the most a walk of that literal may allocate, in bytes: the walk's own maps and one visitor,
// far below one context for each element.
const literalWalkBudget = 64 << 10

// A composite literal's elements are placed one at a time, each as the walk reaches it, so a walk holds no context for
// every element at once (#deb8gq9): walking a slice literal of literalElements values allocates under
// literalWalkBudget, the least of three walks, where placing every element first held a context for each, about
// 80 bytes and a map entry apiece, and grew the map through every size up to them.
//
// Not parallel: it reads the process's allocation count, which another test allocating meanwhile would add to.
func TestWalkingALiteralsElementsHoldsNoContextForEach(test *testing.T) {
	source := "package table\n\nvar Table = []int{" + strings.Repeat("1, ", literalElements) + "}\n"
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "table.go", source, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{},
		Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances: map[*ast.Ident]types.Instance{}}
	checked, checkError := (&types.Config{Importer: importer.Default()}).Check("table", fileSet, []*ast.File{file},
		info)
	if checkError != nil {
		test.Fatal(checkError)
	}
	least := uint64(1 << 62)
	for range 3 {
		collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
		var before runtime.MemStats
		var after runtime.MemStats
		runtime.ReadMemStats(&before)
		walkUses(&collected, checked, info, []*ast.File{file}, true)
		runtime.ReadMemStats(&after)
		least = min(least, after.TotalAlloc-before.TotalAlloc)
	}
	if least > literalWalkBudget {
		test.Errorf("walking a literal of %d elements allocated %d bytes, want at most %d: a context held for each",
			literalElements, least, literalWalkBudget)
	}
}

// placedDependency is the dependency of the placing fixtures: an interface, a struct that has its methods, a struct
// whose field holds the interface, and a struct a field of which another struct's literal copies.
const placedDependency = "package dependency\n\n" +
	"type Shape interface{ Area() int }\n\n" +
	"type Box struct{ Side int }\n\n" +
	"func (box Box) Area() int { return box.Side * box.Side }\n\n" +
	"type Holder struct{ Shape Shape }\n\n" +
	"type Inner struct{ Count int }\n\n" +
	"type Record struct{ Count int }\n\n" +
	"type Size int\n\nconst Count Size = 3\n\n" +
	"type Key struct{ Name string }\n\ntype Value struct{ Count int }\n\n" +
	"type Generic[Element any] struct{ Value Element }\n\ntype Argument struct{ Count int }\n"

// placedUsers are uses whose kind the walk reads from a destination it places (#deb8gq9), each named with the kind
// of the dependency's type it must record, which no other part of its source records:
//   - one call's results spread over a call's parameters, an assignment's targets, a declaration's names and a
//     function's results, each moving a Box to a Shape, the interface's method set (placeCall, placeValues);
//   - a struct literal's keyed and unkeyed field holding a Box as a Shape, the same (placeField);
//   - a slice literal's element and a map literal's key holding a Box as a Shape, the same (inspectElements);
//   - a struct literal with elements handed on by pointer, its type whole (inspectComposite);
//   - a field a struct literal copies from a pointer's struct, that struct whole (placeField's whole operands).
var placedUsers = map[string]struct {
	source string
	name   string
	kind   UseKind
}{
	"a call's results spread over parameters": {
		source: "func two() (dependency.Box, int) { return dependency.Box{}, 0 }\n\n" +
			"func take(shape dependency.Shape, count int) {}\n\nfunc use() { take(two()) }\n",
		name: "Shape", kind: UseMethodSet,
	},
	"a call's results spread over targets": {
		source: "func two() (dependency.Box, int) { return dependency.Box{}, 0 }\n\n" +
			"var shape dependency.Shape\n\nvar count int\n\nfunc use() { shape, count = two() }\n",
		name: "Shape", kind: UseMethodSet,
	},
	"a call's results spread over names": {
		source: "func two() (dependency.Box, dependency.Box) { return dependency.Box{}, dependency.Box{} }\n\n" +
			"var first, second dependency.Shape = two()\n",
		name: "Shape", kind: UseMethodSet,
	},
	"a call's results spread over results": {
		source: "func two() (dependency.Box, int) { return dependency.Box{}, 0 }\n\n" +
			"func use() (dependency.Shape, int) { return two() }\n",
		name: "Shape", kind: UseMethodSet,
	},
	"a keyed field": {
		source: "var holder = dependency.Holder{Shape: dependency.Box{}}\n",
		name:   "Shape", kind: UseMethodSet,
	},
	"an unkeyed field": {
		source: "var holder = dependency.Holder{dependency.Box{}}\n",
		name:   "Shape", kind: UseMethodSet,
	},
	"a slice literal's element": {
		source: "var shapes = []dependency.Shape{dependency.Box{}}\n",
		name:   "Shape", kind: UseMethodSet,
	},
	"a map literal's key": {
		source: "var counts = map[dependency.Shape]int{dependency.Box{}: 1}\n",
		name:   "Shape", kind: UseMethodSet,
	},
	"a struct literal with elements": {
		source: "func keep(*dependency.Holder) {}\n\nfunc use() { keep(&dependency.Holder{Shape: nil}) }\n",
		name:   "Holder", kind: UseWhole,
	},
	"a field copied from a pointer's struct": {
		source: "func copyOf(inner *dependency.Inner) dependency.Record {\n" +
			"\treturn dependency.Record{Count: inner.Count}\n}\n",
		name: "Inner", kind: UseWhole,
	},
}

// Each placed user records its dependency type's kind of use, which only a destination the walk placed gives it: an
// unplaced value is moved where the walk cannot see, which records no interface's method set, and a struct literal
// placed by no rule records neither its own type whole nor a selected field's operand.
func TestEachPlacedUseRecordsItsKind(test *testing.T) {
	test.Parallel()

	for user, placed := range placedUsers {
		test.Run(user, func(test *testing.T) {
			test.Parallel()

			collected := readsOver(test, placedDependency,
				"package user\n\nimport \""+unplacedDependency+"\"\n\n"+placed.source)
			for use := range collected.uses {
				if use.Kind == placed.kind && use.Object.Name() == placed.name &&
					use.Object.Pkg().Path() == unplacedDependency {
					return
				}
			}
			test.Errorf("%s: want dependency.%s used %v, got %v", user, placed.name, placed.kind, collected.uses)
		})
	}
}

// Selections differing only by receiver or only by name are two selections, each kept (selectionKey, #deb8gq9): a
// Record's Count and an Inner's, both the first field, and a Box's Side and its Area, both at index 0, four in all.
func TestSelectionsAreTheSameOnlyByReceiverNameAndPath(test *testing.T) {
	test.Parallel()

	collected := readsOver(test, placedDependency, "package user\n\nimport \""+unplacedDependency+"\"\n\n"+
		"func use(record dependency.Record, inner dependency.Inner, box dependency.Box) int {\n"+
		"\treturn record.Count + inner.Count + box.Side + box.Area()\n}\n")
	if len(collected.selections) != 4 {
		test.Errorf("collected %d selections, want 4: %v", len(collected.selections), collected.selections)
	}
}

// literalTypeUsers are composite literals whose own type expression holds a use below the type's name, each taken by
// address and compared with nil, so nothing moves the literal and only the walk of its type records the use (#deb8gq9):
// an anonymous struct's field type, declared, whole; an array length of a constant of another package's named type, a
// value of it; a map literal's key type, whole; and a generic type's argument, which its instance's own record reads
// whole.
var literalTypeUsers = map[string]struct {
	source string
	name   string
	kind   UseKind
}{
	"an anonymous struct's field type": {
		source: "func use() bool { return &struct{ Field dependency.Record }{} != nil }\n",
		name:   "Record", kind: UseWhole,
	},
	"an array's length": {
		source: "func use() bool { return &[dependency.Count]int{} != nil }\n",
		name:   "Size", kind: UseWhole,
	},
	"a map's key type": {
		source: "func use() bool { return &map[dependency.Key]dependency.Value{} != nil }\n",
		name:   "Key", kind: UseWhole,
	},
	"a generic type's argument": {
		source: "func use() bool { return &dependency.Generic[dependency.Argument]{} != nil }\n",
		name:   "Argument", kind: UseWhole,
	},
}

// A composite literal's own type expression is walked, so what it holds below the type's name records its kind of use:
// each literal type user records the dependency type's kind its type expression gives, which nothing else in its
// source records.
func TestALiteralsOwnTypeRecordsWhatItHolds(test *testing.T) {
	test.Parallel()

	for user, literal := range literalTypeUsers {
		test.Run(user, func(test *testing.T) {
			test.Parallel()

			collected := readsOver(test, placedDependency,
				"package user\n\nimport \""+unplacedDependency+"\"\n\n"+literal.source)
			for use := range collected.uses {
				if use.Kind == literal.kind && use.Object.Name() == literal.name &&
					use.Object.Pkg().Path() == unplacedDependency {
					return
				}
			}
			test.Errorf("%s: want dependency.%s used %v, got %v", user, literal.name, literal.kind, collected.uses)
		})
	}
}
