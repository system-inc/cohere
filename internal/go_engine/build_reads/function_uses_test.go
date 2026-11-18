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

// fixtureFile is one file of a fixture's package of path.
type fixtureFile struct {
	path   string
	file   string
	source string
}

// The function uses fixture's packages: second, whose types first embeds, instantiates and selects through, dot, which
// first imports by a dot, and first, whose declarations the test reads.
var functionUsesSources = []fixtureFile{
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

// checkedFixturePackage is one of a fixture's packages, checked: its types and its source.
type checkedFixturePackage struct {
	own    *types.Package
	source checkedSource
}

// checkedFixture is the packages of sources checked, each of the files sources lists in a row under one path, in the
// order sources lists them, their comments kept as a build's are, and the file set that places them.
func checkedFixture(test *testing.T, sources []fixtureFile) (*token.FileSet, []checkedFixturePackage) {
	test.Helper()

	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	configuration := types.Config{Importer: importerOf(func(path string) (*types.Package, error) {
		if imported, found := checked[path]; found {
			return imported, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	var packagesChecked []checkedFixturePackage
	for start := 0; start < len(sources); {
		end := start + 1
		for end < len(sources) && sources[end].path == sources[start].path {
			end++
		}
		var files []*ast.File
		for _, source := range sources[start:end] {
			file, parseError := parser.ParseFile(fileSet, source.file, source.source,
				parser.SkipObjectResolution|parser.ParseComments)
			if parseError != nil {
				test.Fatal(parseError)
			}
			files = append(files, file)
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Uses:       map[*ast.Ident]types.Object{},
			Defs:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Implicits:  map[ast.Node]types.Object{},
			Instances:  map[*ast.Ident]types.Instance{},
		}
		own, checkError := configuration.Check(sources[start].path, fileSet, files, info)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", sources[start].path, checkError)
		}
		checked[sources[start].path] = own
		packagesChecked = append(packagesChecked, checkedFixturePackage{own: own,
			source: checkedSource{files: files, info: info}})
		start = end
	}
	return fileSet, packagesChecked
}

// functionUsesOf are the declarations' uses of the last package of sources, each declaration's identity, then each
// part's uses, signature and body apart, written out and sorted (see writtenPartUses), `part unknown` among them for a
// part the record marks unknown, and how many of the identities written were unknown.
func functionUsesOf(test *testing.T, sources []fixtureFile) (map[string][]string, int) {
	test.Helper()

	fileSet, packagesChecked := checkedFixture(test, sources)
	last := packagesChecked[len(packagesChecked)-1]
	reads := &Reads{}
	reads.RecordTypes(last.own, last.source.info, last.source.files)
	uses, recorded := reads.FunctionUses()
	if !recorded {
		test.Fatal("want the last package's function uses recorded")
	}
	renderer := NewIdentityRenderer(fileSet, nil)
	written := map[string][]string{}
	for declared := range uses.Declarations() {
		var names []string
		for _, identity := range renderer.Declaration(declared.Declaration) {
			names = append(names, identity.String())
		}
		name := string(declared.Declaration.Kind) + " " + strings.Join(names, ", ")
		written[name+" signature"] = writtenPartUses(renderer, uses, declared.Signature, declared.SignatureUnknown)
		if declared.HasBody {
			written[name+" body"] = writtenPartUses(renderer, uses, declared.Body, declared.BodyUnknown)
		}
	}
	return written, renderer.Unknown
}

// checkWrittenUses fails test for each part written and want differ on, either missing one or the two holding other
// uses.
func checkWrittenUses(test *testing.T, written map[string][]string, want map[string][]string) {
	test.Helper()

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

// writtenPartUses are uses, of record, written out, sorted: a selection as its receiver's identity, the name selected
// at its path, and the selected object's identity, and any other use as its object's identity, each with its kinds,
// and `part unknown` when unknown marks the part so.
func writtenPartUses(renderer *IdentityRenderer, record *FunctionUses, uses []PartUse, unknown bool) []string {
	written := make([]string, 0, len(uses))
	if unknown {
		written = append(written, "part unknown")
	}
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

	written, unknown := functionUsesOf(test, functionUsesSources)
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
			"(second.Box).Get (second.go) Named",
			"first.Shown (first.go) .Name@0, first.Shown.UF0 (first.go) Selected",
			"first.Shown (first.go) ValueType|TypeArgument",
			"first.Shown.UF0 (first.go) Named",
			"import second (first.go) Named",
			"second.Box (second.go) .Get@0, (second.Box).Get (second.go) Selected",
			"second.Box (second.go) ValueType|TypeArgument",
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
			"(second.Inner).Describe (second.go) Named",
			"first.Local (first.go) .Base@0, first.Local.UF0 (first.go) Selected",
			"first.Local (first.go) .Describe@0,0,0, (second.Inner).Describe (second.go) Selected",
			"first.Local (first.go) .Inner@0,0, second.Base.UF0 (second.go) Selected",
			"first.Local (first.go) ValueType",
			"import second (first.go) Named",
			"second.Inner (second.go) .Describe@0, (second.Inner).Describe (second.go) Selected",
			"second.Inner (second.go) Named|ValueType",
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
		"Function (first.Shown).show (first.go) body": {
			"first.Shown (first.go) .Name@0, first.Shown.UF0 (first.go) Selected",
			"first.Shown (first.go) ValueType",
			"first.Shown.UF0 (first.go) Named",
		},
		"Function (first.Shown).show (first.go) signature": {
			"first.Shown (first.go) Named|ValueType",
			"universe.string Named",
		},
		"Function (first.own).Level (first.go) body": nil,
		"Function (first.own).Level (first.go) signature": {
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
	checkWrittenUses(test, written, want)
}

// The structure fixture's packages: other, whose Values returns an array of its T and whose Pairs returns T inside two
// instances, and first, which never names T.
var structureSources = []fixtureFile{
	{"other", "other.go", `package other

type T struct{ A int }

func Values() [1]T { return [1]T{} }

type Pair[First, Second any] struct {
	First  First
	Second Second
}

type Box[Item any] struct{ Item Item }

func Pairs() Pair[int, Box[T]] { return Pair[int, Box[T]]{} }
`},
	{"first", "first.go", `package first

import "other"

func Compare() bool { return other.Values() == other.Values() }

func Infer() { generic(other.Values()) }

func Nested() { generic(other.Pairs()) }

func generic[S comparable](s S) {}
`},
}

// A value's type and a type argument record every named type their structure reaches, though no identifier names it:
// comparing `[1]other.T` reads T's comparability, inferring it as a type argument checks T against comparable, and
// T held two instances deep in another type argument is reached as surely.
func TestAValueTypeRecordsEveryNamedTypeItsStructureReaches(test *testing.T) {
	test.Parallel()

	written, _ := functionUsesOf(test, structureSources)
	want := map[string][]string{
		"Function first.Compare (first.go) body": {
			"import other (first.go) Named",
			"other.T (other.go) ValueType",
			"other.Values (other.go) Named",
		},
		"Function first.Compare (first.go) signature": {
			"universe.bool Named",
		},
		"Function first.Infer (first.go) body": {
			"first.generic (first.go) Named",
			"import other (first.go) Named",
			"other.T (other.go) ValueType|TypeArgument",
			"other.Values (other.go) Named",
		},
		"Function first.Infer (first.go) signature": nil,
		"Function first.Nested (first.go) body": {
			"first.generic (first.go) Named",
			"import other (first.go) Named",
			"other.Box (other.go) ValueType|TypeArgument",
			"other.Pair (other.go) ValueType|TypeArgument",
			"other.Pairs (other.go) Named",
			"other.T (other.go) ValueType|TypeArgument",
		},
		"Function first.Nested (first.go) signature": nil,
		"Function first.generic (first.go) body":     nil,
		"Function first.generic (first.go) signature": {
			"universe.comparable Named|ValueType",
		},
	}
	checkWrittenUses(test, written, want)
}

// The unknown fixture's package: cfg, an unexported var of an unnamed struct type, whose field A no objectpath
// reaches, read by Read, and Settled, which reads cfg whole and nothing unknown.
var unknownSources = []fixtureFile{
	{"first", "first.go", `package first

var cfg struct{ A int }

func Read() int { return cfg.A }

func Settled() any { return cfg }
`},
}

// A part holding a use whose identity renders unknown is marked unknown itself, so a reader never compares its uses,
// while every other part of the package, its signature included, is known.
func TestAPartHoldingAnUnknownUseIsUnknown(test *testing.T) {
	test.Parallel()

	written, unknown := functionUsesOf(test, unknownSources)
	if unknown == 0 {
		test.Error("want an identity unknown, got none")
	}
	want := map[string][]string{
		"Function first.Read (first.go) body": {
			"first.cfg (first.go) Named",
			"none .A@0, unknown Selected",
			"part unknown",
			"unknown Named",
		},
		"Function first.Read (first.go) signature": {
			"universe.int Named",
		},
		"Function first.Settled (first.go) body": {
			"first.cfg (first.go) Named",
		},
		"Function first.Settled (first.go) signature": {
			"universe.any Named|ValueType",
		},
		"Value first.cfg (first.go) signature": {
			"universe.int Named",
		},
	}
	checkWrittenUses(test, written, want)
}

// Each declaration's offset is where the dependence graph places its unit (#w72zxtq): a function at its `func`, past
// its doc comment, and a type or value spec at its first name, a const spec repeating the one before at its own name
// rather than that one's, and an init function and a blank value at their own place too, so the census can ask the
// graph about each.
func TestEachDeclarationsOffsetIsWhereItStarts(test *testing.T) {
	test.Parallel()

	sources := []fixtureFile{{"first", "first.go", `package first

// Documented has a doc comment its offset passes.
func Documented() int { return 1 }

type (
	Kind  int
	Other struct{ Count int }
)

const (
	Low Kind = iota
	High
)

var _, Blank = 1, 2

func init() {}
`}}
	_, packagesChecked := checkedFixture(test, sources)
	last := packagesChecked[len(packagesChecked)-1]
	reads := &Reads{}
	reads.RecordTypes(last.own, last.source.info, last.source.files)
	uses, recorded := reads.FunctionUses()
	if !recorded {
		test.Fatal("want the package's function uses recorded")
	}
	var got []string
	for declared := range uses.Declarations() {
		started := strings.Fields(sources[0].source[declared.Declaration.Offset:])[0]
		got = append(got, string(declared.Declaration.Kind)+" "+declared.Declaration.Name+": "+started)
	}
	want := []string{
		"Function Documented: func",
		"Type Kind: Kind",
		"Type Other: Other",
		"Value : Low",
		"Value : High",
		"Value : _,",
		"Function init: func",
	}
	if !slices.Equal(got, want) {
		test.Errorf("each declaration starts at\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The selection fixture's packages: other, whose T first's Holder embeds in a struct of no name, and first, whose
// Promoted selects T's field through Holder and whose method Again selects itself.
var selectionSources = []fixtureFile{
	{"other", "other.go", `package other

type T struct{ A int }
`},
	{"first", "first.go", `package first

import "other"

var Holder struct{ other.T }

func Promoted() int { return Holder.A }

type Own struct{}

func (own Own) Again() { own.Again() }
`},
}

// A selection that starts from no named type is recorded with no receiver, each embedded field on its way too, and a
// selection of the declaration's own method from a type of its package is recorded: only one whose selected object
// and receiver are both the declaration's own is left out.
func TestASelectionIsLeftOutOnlyWhenItAndItsReceiverAreOwn(test *testing.T) {
	test.Parallel()

	written, _ := functionUsesOf(test, selectionSources)
	want := map[string][]string{
		"Function (first.Own).Again (first.go) body": {
			"first.Own (first.go) .Again@0, (first.Own).Again (first.go) Selected",
			"first.Own (first.go) ValueType",
		},
		"Function (first.Own).Again (first.go) signature": {
			"first.Own (first.go) Named|ValueType",
		},
		"Function first.Promoted (first.go) body": {
			"first.Holder (first.go) Named",
			"none .A@0,0, other.T.UF0 (other.go) Selected",
			"none .T@0, first.Holder.F0 (first.go) Selected",
			"other.T (other.go) ValueType",
			"other.T.UF0 (other.go) Named",
		},
		"Function first.Promoted (first.go) signature": {
			"universe.int Named",
		},
		"Type first.Own (first.go) signature": nil,
		"Value first.Holder (first.go) signature": {
			"import other (first.go) Named",
			"other.T (other.go) Named|ValueType",
		},
	}
	checkWrittenUses(test, written, want)
}

// The branches fixture's packages: other, whose vars each hold T only through one kind of unnamed type, whose Union
// holds it only as a union term, and whose Ignored holds it only as an alias's type argument, and first, which reads
// each of them and never names T.
var branchesSources = []fixtureFile{
	{"other", "other.go", `package other

type T struct{ A int }

type I[X any] interface{ M() X }

type Ignoring[X any] = int

type Union = interface{ T | int }

var (
	MapKey       map[T]int
	MapElement   map[int]T
	Result       func() T
	Parameter    func(T)
	Channel      chan T
	Slice        []T
	PointerArray *[1]T
	Field        struct{ F T }
	Method       interface{ M() T }
	Embedded     interface{ I[T] }
	Ignored      Ignoring[T]
)
`},
	{"first", "first.go", `package first

import "other"

func MapKey() { _ = other.MapKey }

func MapElement() { _ = other.MapElement }

func Result() { _ = other.Result }

func Parameter() { _ = other.Parameter }

func Channel() { _ = other.Channel }

func Slice() { _ = other.Slice }

func PointerArray() { _ = other.PointerArray }

func Field() { _ = other.Field }

func Method() { _ = other.Method }

func Embedded() { _ = other.Embedded }

func Constrained[S other.Union](s S) {}

func Ignored() { _ = other.Ignored }
`},
}

// Each branch of the named-type walk reaches T on its own: a map's key and its element, a signature's results and
// its parameters, a channel's, a slice's, a pointer's and an array's element, a struct's field, an interface's method
// and its embedded instance, a union's term in a constraint, and an alias's type argument its right side ignores.
// Each part holds a type that reaches T through its one branch alone, so a walk that skips the branch leaves T out.
func TestEachBranchOfTheNamedTypeWalkReachesItsNamedType(test *testing.T) {
	test.Parallel()

	written, _ := functionUsesOf(test, branchesSources)
	for _, branch := range []struct {
		part string
		want string
	}{
		{"Function first.MapKey (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.MapElement (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Result (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Parameter (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Channel (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Slice (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.PointerArray (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Field (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Method (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Embedded (first.go) body", "other.T (other.go) ValueType"},
		{"Function first.Constrained (first.go) signature", "other.T (other.go) ValueType"},
		{"Function first.Ignored (first.go) body", "other.T (other.go) ValueType"},
	} {
		if !slices.Contains(written[branch.part], branch.want) {
			test.Errorf("%s: want %s, got\n%s", branch.part, branch.want, strings.Join(written[branch.part], "\n"))
		}
	}
}

// A package that names a symbol by go:linkname records no function uses, which is unknown, never empty, since its
// bodies depend on a symbol no use names, while the type reads beside them are recorded.
func TestALinknamedPackageRecordsNoFunctionUses(test *testing.T) {
	test.Parallel()

	// The type checker reads no go:linkname, so the fixture needs no unsafe import, which no fixture importer serves.
	_, packagesChecked := checkedFixture(test, []fixtureFile{{"first", "first.go", `package first

//go:linkname now time.now
func now() (int64, int32, int64)

func Read() int64 {
	seconds, _, _ := now()
	return seconds
}
`}})
	reads := &Reads{}
	reads.RecordTypes(packagesChecked[0].own, packagesChecked[0].source.info, packagesChecked[0].source.files)
	if uses, recorded := reads.FunctionUses(); recorded || uses != nil {
		test.Fatalf("want the function uses unrecorded, got %t, %v", recorded, uses)
	}
	if !reads.Recorded() {
		test.Fatal("want the type reads recorded")
	}
}

// A selection whose way runs through a type that is no struct, which the type checker never produces, marks its part
// unknown rather than record the embedded fields it could follow and leave the rest out, while the declaration's other
// part stays known.
func TestASelectionThroughNoStructMarksItsPartUnknown(test *testing.T) {
	test.Parallel()

	_, packagesChecked := checkedFixture(test, unknownSources)
	first := packagesChecked[0]
	recorder := newFunctionUsesRecorder(first.own, first.source.info, first.source.files)
	recorder.startFile(first.source.files[0])
	var read *ast.FuncDecl
	for _, declaration := range first.source.files[0].Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Name.Name == "Read" {
			read = function
		}
	}
	if read == nil {
		test.Fatal("want the fixture's Read")
	}
	recorder.beginFunction(read)
	recorder.reach(read.Body)
	recorder.selectionAt(types.Typ[types.Int], []int{0, 0}, first.own.Scope().Lookup("cfg"))
	recorder.end()
	uses := recorder.finish()
	if uses == nil {
		test.Fatal("want the record recorded")
	}
	declarations := slices.Collect(uses.Declarations())
	if len(declarations) != 1 {
		test.Fatalf("want Read alone recorded, got %d declarations", len(declarations))
	}
	if !declarations[0].BodyUnknown || declarations[0].SignatureUnknown {
		test.Errorf("want the body alone unknown, got the body %t and the signature %t", declarations[0].BodyUnknown,
			declarations[0].SignatureUnknown)
	}
}

// A package that uses more things than a PartUse can index overflows and records no function uses at all, never a
// part of them, which is unknown, while the type reads beside them are recorded. The limit is lowered below the
// fixture's table, and every parallel test waits until this one has ended and put it back.
//
// Not parallel: it lowers partUseEntryLimit, which every recorder of every test reads.
func TestAPackageThatOverflowsRecordsNoFunctionUses(test *testing.T) {
	limit := partUseEntryLimit
	partUseEntryLimit = 2
	test.Cleanup(func() { partUseEntryLimit = limit })

	_, packagesChecked := checkedFixture(test, functionUsesSources)
	first := packagesChecked[len(packagesChecked)-1]
	reads := &Reads{}
	reads.RecordTypes(first.own, first.source.info, first.source.files)
	if uses, recorded := reads.FunctionUses(); recorded || uses != nil {
		test.Fatalf("want the function uses unrecorded, got %t, %v", recorded, uses)
	}
	if !reads.Recorded() {
		test.Fatal("want the type reads recorded")
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
