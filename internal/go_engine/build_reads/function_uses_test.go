package build_reads

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"
)

// The function uses fixture's packages: second, whose types first embeds, instantiates and selects through, dot, which
// first imports by a dot, and first, whose declarations the test reads.
var functionUsesSources = []struct {
	path   string
	file   string
	source string
}{
	{"second", "second.go", `package second

type Inner struct{ Count int }

func (Inner) Describe() string { return "inner" }

type Base struct {
	Inner
	Label string
}

type Box[Item any] struct{ Item Item }

func (box Box[Item]) Get() Item { return box.Item }

func Identity[Value any](value Value) Value { return value }

const Limit = 3

type Level int

func Helper() int { return Limit }
`},
	{"dot", "dot.go", `package dot

func Greet() string { return "hello" }
`},
	{"first", "first.go", `package first

import (
	. "dot"
	"second"
)

type Local struct {
	second.Base
	own
}

type own struct{ Depth int }

func (own) Level() int { return 1 }

type shower interface{ show() string }

type Shown struct{ Name string }

func (shown Shown) show() string { return shown.Name }

var Started = second.Helper()

func Embedded(local Local) int {
	return local.Count + local.Depth
}

func Methods(local Local) (func() string, func(second.Inner) string) {
	return local.Describe, second.Inner.Describe
}

func Satisfy(shown Shown) shower {
	var moved shower = shown
	return moved
}

func Generic(box second.Box[Shown]) int {
	return len(second.Identity(box).Get().Name)
}

func Dotted() string { return Greet() }

func Counted(items []string) int {
	total := 0
	add := func(item string) { total += len(item) }
	for _, item := range items {
		add(item)
	}
	return total + second.Limit
}

func init() { _ = Started }

const (
	Low second.Level = iota + second.Limit
	High
)
`},
}

// checkedFixturePackage is one of the function uses fixture's packages, checked: its types and its source.
type checkedFixturePackage struct {
	own    *types.Package
	source checkedSource
}

// checkedFunctionUsesFixture is the function uses fixture's packages checked, in the order functionUsesSources lists
// them, and the file set that places them.
func checkedFunctionUsesFixture(test *testing.T) (*token.FileSet, []checkedFixturePackage) {
	test.Helper()

	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	configuration := types.Config{Importer: importerOf(func(path string) (*types.Package, error) {
		if imported, found := checked[path]; found {
			return imported, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	packagesChecked := make([]checkedFixturePackage, 0, len(functionUsesSources))
	for _, source := range functionUsesSources {
		file, parseError := parser.ParseFile(fileSet, source.file, source.source, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Uses:       map[*ast.Ident]types.Object{},
			Defs:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Implicits:  map[ast.Node]types.Object{},
			Instances:  map[*ast.Ident]types.Instance{},
		}
		own, checkError := configuration.Check(source.path, fileSet, []*ast.File{file}, info)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", source.path, checkError)
		}
		checked[source.path] = own
		packagesChecked = append(packagesChecked, checkedFixturePackage{own: own,
			source: checkedSource{files: []*ast.File{file}, info: info}})
	}
	return fileSet, packagesChecked
}

// functionUsesOfFixture are the fixture's first package's declarations' uses, each declaration's identity, then each
// part's uses, signature and body apart, written out and sorted (see writtenPartUses), and how many of the identities
// written were unknown.
func functionUsesOfFixture(test *testing.T) (map[string][]string, int) {
	test.Helper()

	fileSet, packagesChecked := checkedFunctionUsesFixture(test)
	first := packagesChecked[len(packagesChecked)-1]
	reads := &Reads{}
	reads.RecordTypes(first.own, first.source.info, first.source.files)
	uses, recorded := reads.FunctionUses()
	if !recorded {
		test.Fatal("want the first package's function uses recorded")
	}
	renderer := NewIdentityRenderer(fileSet, nil)
	written := map[string][]string{}
	for declared := range uses.Declarations() {
		var names []string
		for _, identity := range renderer.Declaration(declared.Declaration) {
			names = append(names, identity.String())
		}
		name := string(declared.Declaration.Kind) + " " + strings.Join(names, ", ")
		written[name+" signature"] = writtenPartUses(renderer, uses, declared.Signature)
		if declared.HasBody {
			written[name+" body"] = writtenPartUses(renderer, uses, declared.Body)
		}
	}
	return written, renderer.Unknown
}

// writtenPartUses are uses, of record, written out, sorted: a selection as its receiver's identity, the name selected
// at its path, and the selected object's identity, and any other use as its object's identity, each with its kinds.
func writtenPartUses(renderer *IdentityRenderer, record *FunctionUses, uses []PartUse) []string {
	written := make([]string, 0, len(uses))
	for _, use := range uses {
		used := record.Used(use)
		object := renderer.Object(used.Object).String()
		if used.Index == "" {
			written = append(written, object+" "+use.Kinds().String())
			continue
		}
		receiver := "none"
		if used.Receiver != nil {
			receiver = renderer.Object(used.Receiver).String()
		}
		written = append(written, receiver+" ."+used.Object.Name()+"@"+used.Index+" "+object+" "+
			use.Kinds().String())
	}
	slices.Sort(written)
	return written
}

// Each top-level declaration records every object each of its parts uses, its signature and its body apart, with
// every kind of use: a selection through another package's embedded field and through the package's own, a generic
// type and function instantiated, a dot import's name, a method value and a method expression, a value moved into an
// interface it satisfies through an unexported method, a var initialized by a call, the universe's len, and a closure,
// whose locals, like every local and parameter of the declaration, are its own and left out. A const spec with no type
// nor values uses what the spec it repeats does, and an init function has no objectpath, so its file and ordinal key
// it. Every identity is known.
func TestEachDeclarationRecordsWhatEachPartUses(test *testing.T) {
	test.Parallel()

	written, unknown := functionUsesOfFixture(test)
	if unknown != 0 {
		test.Errorf("want every identity known, got %d unknown", unknown)
	}
	want := map[string][]string{
		"Function first.Counted (first.go) body": {
			"import second (first.go) Named",
			"second.Limit (second.go) Named|Constant",
			"universe.len Named",
			"universe.string Named",
		},
		"Function first.Counted (first.go) signature": {
			"universe.int Named",
			"universe.string Named",
		},
		"Function first.Dotted (first.go) body": {
			"dot.Greet (dot.go) Named",
		},
		"Function first.Dotted (first.go) signature": {
			"universe.string Named",
		},
		"Function first.Embedded (first.go) body": {
			"first.Local (first.go) .Base@0, first.Local.UF0 (first.go) Selected",
			"first.Local (first.go) .Count@0,0,0, second.Inner.UF0 (second.go) Selected",
			"first.Local (first.go) .Depth@1,0, first.own.UF0 (first.go) Selected",
			"first.Local (first.go) .Inner@0,0, second.Base.UF0 (second.go) Selected",
			"first.Local (first.go) .own@1, first.Local.UF1 (first.go) Selected",
			"first.Local (first.go) ValueType",
			"first.own.UF0 (first.go) Named",
			"second.Inner.UF0 (second.go) Named",
		},
		"Function first.Embedded (first.go) signature": {
			"first.Local (first.go) Named|ValueType",
			"universe.int Named",
		},
		"Function first.Generic (first.go) body": {
			"first.Shown (first.go) .Name@0, first.Shown.UF0 (first.go) Selected",
			"first.Shown (first.go) ValueType|TypeArgument",
			"first.Shown.UF0 (first.go) Named",
			"import second (first.go) Named",
			"second.Box (second.go) .Get@0, second.Box.M0 (second.go) Selected",
			"second.Box (second.go) ValueType|TypeArgument",
			"second.Box.M0 (second.go) Named",
			"second.Identity (second.go) Named",
			"universe.len Named",
		},
		"Function first.Generic (first.go) signature": {
			"first.Shown (first.go) Named|ValueType|TypeArgument",
			"import second (first.go) Named",
			"second.Box (second.go) Named|ValueType",
			"universe.int Named",
		},
		"Function first.Methods (first.go) body": {
			"first.Local (first.go) .Base@0, first.Local.UF0 (first.go) Selected",
			"first.Local (first.go) .Describe@0,0,0, second.Inner.M0 (second.go) Selected",
			"first.Local (first.go) .Inner@0,0, second.Base.UF0 (second.go) Selected",
			"first.Local (first.go) ValueType",
			"import second (first.go) Named",
			"second.Inner (second.go) .Describe@0, second.Inner.M0 (second.go) Selected",
			"second.Inner (second.go) Named|ValueType",
			"second.Inner.M0 (second.go) Named",
		},
		"Function first.Methods (first.go) signature": {
			"first.Local (first.go) Named|ValueType",
			"import second (first.go) Named",
			"second.Inner (second.go) Named|ValueType",
			"universe.string Named",
		},
		"Function first.Satisfy (first.go) body": {
			"first.Shown (first.go) ValueType",
			"first.shower (first.go) Named|ValueType",
		},
		"Function first.Satisfy (first.go) signature": {
			"first.Shown (first.go) Named|ValueType",
			"first.shower (first.go) Named|ValueType",
		},
		"Function first.Shown.M0 (first.go) body": {
			"first.Shown (first.go) .Name@0, first.Shown.UF0 (first.go) Selected",
			"first.Shown (first.go) ValueType",
			"first.Shown.UF0 (first.go) Named",
		},
		"Function first.Shown.M0 (first.go) signature": {
			"first.Shown (first.go) Named|ValueType",
			"universe.string Named",
		},
		"Function first.own.M0 (first.go) body": nil,
		"Function first.own.M0 (first.go) signature": {
			"first.own (first.go) Named|ValueType",
			"universe.int Named",
		},
		"Function first.go#0 init body": {
			"first.Started (first.go) Named",
		},
		"Function first.go#0 init signature": nil,
		"Type first.Local (first.go) signature": {
			"first.own (first.go) Named|ValueType",
			"import second (first.go) Named",
			"second.Base (second.go) Named|ValueType",
		},
		"Type first.Shown (first.go) signature": {
			"universe.string Named",
		},
		"Type first.own (first.go) signature": {
			"universe.int Named",
		},
		"Type first.shower (first.go) signature": {
			"universe.string Named",
		},
		"Value first.Started (first.go) body": {
			"import second (first.go) Named",
			"second.Helper (second.go) Named",
		},
		"Value first.Started (first.go) signature": nil,
		"Value first.Low (first.go) signature": {
			"import second (first.go) Named",
			"second.Level (second.go) Named|ValueType",
		},
		"Value first.Low (first.go) body": {
			"import second (first.go) Named",
			"second.Level (second.go) ValueType",
			"second.Limit (second.go) Named|Constant",
			"universe.iota Named|Constant",
		},
		"Value first.High (first.go) signature": {
			"import second (first.go) Named",
			"second.Level (second.go) Named|ValueType",
		},
		"Value first.High (first.go) body": {
			"import second (first.go) Named",
			"second.Level (second.go) ValueType",
			"second.Limit (second.go) Named|Constant",
			"universe.iota Named|Constant",
		},
	}
	names := slices.Sorted(maps.Keys(written))
	for name := range want {
		if _, found := written[name]; !found {
			names = append(names, name)
		}
	}
	for _, name := range names {
		_, wanted := want[name]
		got, found := written[name]
		if wanted != found || !slices.Equal(got, want[name]) {
			test.Errorf("%s (wanted %t, found %t): want\n%s\ngot\n%s", name, wanted, found,
				strings.Join(want[name], "\n"), strings.Join(got, "\n"))
		}
	}
}

// Without the syntax or a map a use is read from, a use could go unseen, so the function uses are unrecorded, which
// is unknown, never empty, while the type reads beside them are recorded.
func TestFunctionUsesWithoutTypeInformationAreUnrecorded(test *testing.T) {
	test.Parallel()

	reads := &Reads{}
	reads.RecordTypes(types.NewPackage("example.com/own", "own"), &types.Info{}, nil)
	if uses, recorded := reads.FunctionUses(); recorded || uses != nil {
		test.Fatalf("want the function uses unrecorded, got %t, %v", recorded, uses)
	}
	if !reads.Recorded() {
		test.Fatal("want the type reads recorded")
	}
}
