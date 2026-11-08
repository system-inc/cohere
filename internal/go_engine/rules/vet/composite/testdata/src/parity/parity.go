// Package parity holds every branch of composites' judgment, each beside a clean case, and the shapes most likely to
// tell a port from the original: aliases, elided literals and pointers, generics and the order of a constraint's
// terms, dot imports, embedded fields, closures and the allow list. The parity test runs the original and the port
// over it and compares their findings and suggested fixes, with the allow list on and off.
package parity

import (
	"go/token"
	"image"
	"image/color"
	"testing"
	"unicode"

	"parity/library"
	. "parity/library"
)

// Local is a struct of this package, so its unkeyed literal is the writer's to keep in step.
type Local struct{ F int }

// LocalPair is a local struct of two fields.
type LocalPair struct {
	Name  string
	Count int
}

// Defined is a local type defined over an imported struct, so it is local.
type Defined library.Struct

// LocalAlias is a local name for an imported struct, judged as the import it names and printed as the alias.
type LocalAlias = library.Struct

// LocalPointAlias is a local name for an allow-listed type, which the allow list does not know by this name.
type LocalPointAlias = image.Point

// LocalConstraint is an interface of Local's type set alone.
type LocalConstraint interface{ Local }

// LocalGeneric is a local generic struct.
type LocalGeneric[Value any] struct {
	Value Value
	Count int
}

// Every value keyed, every value unkeyed, and an empty literal.
//
// A literal with too few or too many values, or one that would set an unexported or blank field of another package,
// does not type-check, so it cannot be a fixture here. Those are the branches that offer no fix, and x/tools' fixture
// a, which the parity test also runs, holds them: tooFewFieldsStructLiteral, tooManyFieldsStructLiteral and
// sync.Mutex{0, 0}.
var (
	keyed     = library.Struct{F: 1}
	unkeyed   = library.Struct{1}
	empty     = library.Struct{}
	emptyType = library.Empty{}
	pair      = library.Pair{"name", 1}
	keyedPair = library.Pair{Name: "name", Count: 1}
	// An embedded field is named for its type.
	embedding      = library.Embedding{library.Struct{1}, &library.Pair{"name", 1}, 3}
	keyedEmbedding = library.Embedding{Struct: library.Struct{F: 1}, Count: 3}
)

// Local structs, anonymous structs and literals of no struct are never judged.
var (
	local          = Local{1}
	localPointer   = &Local{1}
	localPair      = []LocalPair{{"name", 1}}
	localPointers  = []*LocalPair{{"name", 1}}
	anonymous      = struct{ A, B int }{1, 2}
	anonymousSlice = []struct{ A, B int }{{1, 2}}
	defined        = Defined{1}
	localGeneric   = LocalGeneric[int]{1, 2}
	numbers        = []int{1, 2}
	words          = map[string]int{"one": 1}
	array          = [2]int{1, 2}
	rows           = library.Rows{{1, 2}, {3, 4}}
)

// Elided element and key types: a struct, a pointer to one, in slices, arrays and maps, named and not.
var (
	slice              = []library.Struct{{1}, {F: 2}}
	pointerSlice       = []*library.Struct{{1}, &library.Struct{2}}
	fixedArray         = [2]library.Struct{{1}, {2}}
	ellipsis           = [...]library.Pair{{"name", 1}}
	keys               = map[library.Struct]library.Pair{{1}: {"name", 1}}
	pointerKeys        = map[*library.Struct]int{{1}: 1}
	named              = library.Structs{{1}, {F: 2}}
	namedPointer       = library.Pointers{{1}}
	namedMap           = library.Lookup{{1}: {2}}
	nested             = [][]library.Struct{{{1}}, {{F: 2}}}
	namedPointerElided = []library.StructPointer{{1}}
)

// Aliases print as the alias, here and in the other package, and are judged as the struct they name.
var (
	localAlias        = LocalAlias{1}
	importedAlias     = library.Alias{1}
	localAliasSlice   = []LocalAlias{{1}}
	localAliasPointer = []*LocalAlias{{1}}
)

// Generic imported structs, instantiated, print with their type arguments.
var (
	generic       = library.Generic[string]{"value", 1}
	genericNested = library.Generic[library.Struct]{library.Struct{1}, 1}
	genericSlice  = []library.Generic[int]{{1, 2}}
)

// A dot import's struct is as imported as any.
var (
	dotted      = Struct{1}
	dottedKeyed = Struct{F: 1}
	dottedPair  = []Pair{{"name", 1}}
)

// The allow list knows these by the name they print, so a pointer or an alias of one is not on it.
var (
	point          = image.Point{1, 2}
	rectangle      = image.Rectangle{image.Point{1, 2}, image.Point{3, 4}}
	rgba           = color.RGBA{1, 2, 3, 4}
	ranges         = []unicode.Range16{{0x10, 0x20, 1}}
	tests          = []testing.InternalTest{{"Test", nil}}
	benchmarks     = []testing.InternalBenchmark{{"Benchmark", nil}}
	pointerToPoint = []*image.Point{{1, 2}}
	localPoint     = LocalPointAlias{1, 2}
	importedPoint  = library.PointAlias{1, 2}
	caseRange      = unicode.CaseRange{1, 2, [3]rune{}}
	position       = token.Position{"file.go", 0, 1, 1}
)

// Literals inside functions, closures and methods are judged alike.
func inFunctions() {
	_ = library.Struct{1}
	closure := func() library.Pair {
		return library.Pair{"name", 1}
	}
	_ = closure
	go func() {
		_ = &library.Struct{1}
	}()
	defer func() {
		_ = []library.Struct{{1}}
	}()
}

// method holds a literal in a method.
func (local Local) method() library.Struct {
	return library.Struct{local.F}
}

// Type parameters are judged by the first term of their constraint's normalized term set.
func parameters[
	Imported library.Struct,
	ImportedConstraint library.Constraint,
	Tilde ~struct{ F int },
	TildeImported library.TildeConstraint,
	LocalFirst interface{ Local | library.Struct },
	ImportedFirst interface{ library.Struct | Local },
	// A union of a tilde term and a named type it holds does not type-check, overlapping terms, so the tilde
	// terms meet named ones by intersection below.
	Both interface{ library.Struct | library.Other },
	Intersected interface {
		library.Constraint
		~struct{ F int }
	},
	TildeIntersected interface {
		~struct{ F int }
		library.Struct | Local
	},
	TildeIntersectedLocalFirst interface {
		library.TildeConstraint
		Local | library.Struct
	},
	Interfaces interface {
		library.Constraint | LocalConstraint
	},
	InterfacesLocalFirst interface {
		LocalConstraint | library.Constraint
	},
	Pointer interface{ *library.Struct },
	LocalPointer interface{ *Local },
	Slice ~[]library.Struct,
	Map ~map[library.Struct]int,
	GenericImported library.Generic[int],
	Same LocalPair,
]() {
	_ = Imported{1}
	_ = Imported{F: 1}
	_ = ImportedConstraint{1}
	_ = Tilde{1}
	_ = TildeImported{1}
	_ = LocalFirst{1}
	_ = ImportedFirst{1}
	_ = Both{1}
	_ = Intersected{1}
	_ = TildeIntersected{1}
	_ = TildeIntersectedLocalFirst{1}
	_ = Interfaces{1}
	_ = InterfacesLocalFirst{1}
	_ = []Pointer{{1}}
	_ = []LocalPointer{{1}}
	_ = Slice{{1}, {F: 2}}
	_ = Map{{1}: 1}
	_ = GenericImported{1, 2}
	_ = Same{"name", 1}
	_ = []Imported{{1}}
}

// genericFunction holds literals of its own type parameter's instantiations of an imported generic struct.
func genericFunction[Value any](value Value) library.Generic[Value] {
	_ = []library.Generic[Value]{{value, 1}}
	return library.Generic[Value]{value, 1}
}
