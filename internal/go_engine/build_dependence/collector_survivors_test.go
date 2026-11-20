package build_dependence

import (
	"maps"
	"slices"
	"testing"
)

// besideAHostOnlyWidth is the package survivors of shared, a file both builds compile, beside host.go, a file only the
// host compiles that declares Width, judged with every unit of host.go a seed, so Width's bits are set and whatever
// reads Width is dependent through its kind edges alone.
func besideAHostOnlyWidth(test *testing.T, shared string) (loadedBuild, *Dependence) {
	test.Helper()
	loaded := parsedPackage(test, "survivors", [][2]string{
		{"shared.go", "package survivors\n\n" + shared},
		{"host.go", "package survivors\n\nconst Width = 4\n"},
	})
	graph := NewGraph()
	graph.AddPackage(loaded, true, nil)
	return loadedBuild{loaded.ID: loaded}, graph.Against(Difference{Packages: map[string]PackageDifference{
		loaded.ID: {SharedFiles: []string{"shared.go"}, HostOnlyFiles: []string{"host.go"}}}})
}

// expectNameBits fails for every local of the survivors package whose bits are not want's.
func expectNameBits(test *testing.T, dependence *Dependence, want map[string]Bits) {
	test.Helper()
	for _, local := range slices.Sorted(maps.Keys(want)) {
		got, isDeclared := dependence.NameBits("example.com/survivors", local)
		if !isDeclared || got != want[local] {
			test.Errorf("%s: want its bits %+v, got %+v, declared %t", local, want[local], got, isDeclared)
		}
	}
}

// A named type's name bit follows each method declared on it (collectTypeKinds' edge to every method). A method
// declared in a shared file whose signature reads Width moves its receiver's method set, which the receiver's name bit
// carries, though the receiver's own declaration reads nothing: Changing's name bit is set, and Boxed, which converts a
// Changing to an interface, is dependent. Steady, whose method reads a fixed length, keeps both bits unset, and
// BoxesSteady is not. Drop the edge to a declared method and Changing's name bit is unset and Boxed independent.
func TestAReceiversNameBitFollowsTheSignatureOfAMethodDeclaredOnIt(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Changing struct{}

func (Changing) Apply(items [Width]int) {}

func Boxed() any { return Changing{} }

type Steady struct{}

func (Steady) Apply(items [1]int) {}

func BoxesSteady() any { return Steady{} }
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Changing": {Name: true},
		"Steady":   {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Changing":         true,
		"method Changing.Apply": true,
		"func Boxed":            true,
		"type Steady":           false,
		"method Steady.Apply":   false,
		"func BoxesSteady":      false,
		"value Width":           true,
	})
}

// A named interface's name bit follows its own underlying bit (collectTypeKinds' self edge). An interface's methods
// are its underlying, so a method signature that reads Width sets Applier's underlying bit, and its name bit must
// follow: Pointer, whose result points at an Applier and reads it by name alone, is dependent, and so is IsApplier,
// which asserts to it. Steadier, whose method reads a fixed length, keeps both bits unset, and neither SteadyPointer
// nor IsSteadier is dependent. Make the self edge read the name bit and Applier's name bit is unset and Pointer
// independent, since no other edge of an interface's name bit reads its methods.
func TestAnInterfacesNameBitFollowsItsMethodSignatures(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Applier interface{ Apply(items [Width]int) }

func IsApplier(value any) bool {
	_, isApplier := value.(Applier)
	return isApplier
}

func Pointer() *Applier { return nil }

type Steadier interface{ Apply(items [1]int) }

func IsSteadier(value any) bool {
	_, isSteadier := value.(Steadier)
	return isSteadier
}

func SteadyPointer() *Steadier { return nil }
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Applier":  {Name: true, Underlying: true},
		"Steadier": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Applier":       true,
		"func IsApplier":     true,
		"func Pointer":       true,
		"type Steadier":      false,
		"func IsSteadier":    false,
		"func SteadyPointer": false,
		"value Width":        true,
	})
}

// An alias's name bit reads the uses of its right-hand side's syntax (collectTypeKinds). An alias of a spelled array
// reaches Width only through its syntax, the type checker's [4]byte having lost the name, so Row's name bit, which a
// reader by name reads, is set beside its underlying bit, and Rows, whose result is a pointer to a Row, is dependent.
// Fixed, an alias of a fixed length, keeps both bits unset, and Fixeds is not. Leave the right-hand side's uses out of
// the name bit and Row's name bit is unset and Rows independent.
func TestAnAliasOfASpelledTypeHasANameBitThatReadsItsSyntax(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Row = [Width]byte

func Rows() *Row { return nil }

type Fixed = [1]byte

func Fixeds() *Fixed { return nil }
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Row":   {Name: true, Underlying: true},
		"Fixed": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Row":    true,
		"func Rows":   true,
		"type Fixed":  false,
		"func Fixeds": false,
		"value Width": true,
	})
}

// A const spec with neither type nor values repeats the last one's expression, so it reads what that expression reads
// (collectFile, collectValue): Second, after `First = Width`, has its name bit set and is dependent, though its own
// span names nothing. FixedAgain, after `Fixed = 1`, keeps both bits unset. Keep no values for the spec to repeat, or
// read a repeat's own empty values, and Second's name bit is unset and Second independent.
func TestARepeatedConstSpecReadsWhatTheExpressionItRepeatsReads(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `const (
	First = Width
	Second
)

const (
	Fixed = 1
	FixedAgain
)
`)
	expectNameBits(test, dependence, map[string]Bits{
		"First":      {Name: true},
		"Second":     {Name: true},
		"Fixed":      {},
		"FixedAgain": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"value First":      true,
		"value Second":     true,
		"value Fixed":      false,
		"value FixedAgain": false,
		"value Width":      true,
	})
}

// A constant's value reads a type it names in full (addUse): `unsafe.Sizeof(Layout{})` is Layout's size, which moves
// with Layout's underlying alone, so Size has its name bit set and is dependent while Layout's name bit stays unset.
// SteadySize, the size of a fixed layout, keeps both bits unset. Read the named type by its name alone and Size's name
// bit is unset and Size independent.
func TestAConstantReadsTheLayoutOfATypeItsValueNames(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `import "unsafe"

type Layout struct{ items [Width]byte }

const Size = unsafe.Sizeof(Layout{})

type Steady struct{ items [1]byte }

const SteadySize = unsafe.Sizeof(Steady{})
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Layout":     {Underlying: true},
		"Size":       {Name: true},
		"Steady":     {},
		"SteadySize": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Layout":      true,
		"value Size":       true,
		"type Steady":      false,
		"value SteadySize": false,
		"value Width":      true,
	})
}

// A constant's value reads a var it names in full (addUse): `unsafe.Sizeof(Changing)` is the size of Changing's type,
// a Layout, which moves with Layout's underlying alone, so Size has its name bit set and is dependent while Changing's
// bits, read by its type's name, stay unset. FixedSize, the size of a fixed array, keeps both bits unset. Read the var
// by its own bit alone, or in full only where its value is not read in full, and Size's name bit is unset and Size
// independent.
func TestAConstantReadsTheLayoutOfAVariableItsValueNames(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `import "unsafe"

type Layout [Width]byte

var Changing Layout

const Size = unsafe.Sizeof(Changing)

var Steady [1]byte

const FixedSize = unsafe.Sizeof(Steady)
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Layout":    {Underlying: true},
		"Changing":  {},
		"Size":      {Name: true},
		"Steady":    {},
		"FixedSize": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Layout":     true,
		"value Changing":  false,
		"value Size":      true,
		"value Steady":    false,
		"value FixedSize": false,
		"value Width":     true,
	})
}

// A generic type's or alias's name bit reads its type parameters' constraints (collectTypeKinds): a constraint
// spelled `~[Width]byte` reaches Width only through its syntax, the type checker's `~[4]byte` having lost the name, so
// Box and Rows have their name bits set; and a named Constraint, an interface over Width, is reached through its type,
// so Held and Bytes have theirs set too. Pointer, HeldPointer, RowsOf and BytesOf, which read them by name, are
// dependent. The same over a fixed length keeps every bit unset. Leave out a type's or an alias's walk of its type
// parameters' syntax, or of their constraints' types, and its name bit is unset and its reader independent. Bytes uses
// no type parameter on its right-hand side, since an alias whose right-hand side reads one, `= []T`, reaches its
// constraint through that walk as well, where interfaces read alike in either mode.
func TestAGenericsNameBitReadsItsConstraints(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Constraint interface{ ~[Width]byte }

type Box[T ~[Width]byte] struct{ value T }

func Pointer() *Box[[4]byte] { return nil }

type Held[T Constraint] struct{ value T }

func HeldPointer() *Held[[4]byte] { return nil }

type Rows[T ~[Width]byte] = []T

func RowsOf() Rows[[4]byte] { return nil }

type Bytes[T Constraint] = []byte

func BytesOf() Bytes[[4]byte] { return nil }

type Fixed interface{ ~[1]byte }

type SteadyBox[T ~[1]byte] struct{ value T }

type SteadyHeld[T Fixed] struct{ value T }

type SteadyRows[T ~[1]byte] = []T

type SteadyBytes[T Fixed] = []byte

func SteadyOf() (*SteadyBox[[1]byte], *SteadyHeld[[1]byte], SteadyRows[[1]byte], SteadyBytes[[1]byte]) {
	return nil, nil, nil, nil
}
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Box":         {Name: true},
		"Held":        {Name: true, Underlying: true},
		"Rows":        {Name: true, Underlying: true},
		"Bytes":       {Name: true},
		"SteadyBox":   {},
		"SteadyHeld":  {},
		"SteadyRows":  {},
		"SteadyBytes": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Constraint":  true,
		"type Box":         true,
		"func Pointer":     true,
		"type Held":        true,
		"func HeldPointer": true,
		"type Rows":        true,
		"func RowsOf":      true,
		"type Bytes":       true,
		"func BytesOf":     true,
		"type Fixed":       false,
		"type SteadyBox":   false,
		"type SteadyHeld":  false,
		"type SteadyRows":  false,
		"type SteadyBytes": false,
		"func SteadyOf":    false,
		"value Width":      true,
	})
}

// A type defined from another type's name reads it in full into its name bit (collectTypeKinds, #9paaen9): Derived is
// Base's shape, whose layout alone moves, so Derived's name bit is set beside its underlying bit, and Pointer, whose
// result points at a Derived and reads it by name alone, is dependent. Kept, defined from a fixed layout, keeps both
// bits unset, and KeptPointer is not. Read Base by its name alone and Derived's name bit is unset and Pointer
// independent.
func TestATypeDefinedFromANameReadsItInFullIntoItsNameBit(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Base struct{ items [Width]byte }

type Derived Base

func Pointer() *Derived { return nil }

type Steady struct{ items [1]byte }

type Kept Steady

func KeptPointer() *Kept { return nil }
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Base":    {Underlying: true},
		"Derived": {Name: true, Underlying: true},
		"Kept":    {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Base":        true,
		"type Derived":     true,
		"func Pointer":     true,
		"type Steady":      false,
		"type Kept":        false,
		"func KeptPointer": false,
		"value Width":      true,
	})
}

// An interface method's bit reads its results as a member's own type, as its line observes them
// (collectInterfaceMethods, #mtfdb1m): Give gives an unnamed struct holding a Pad, whose layout alone moves, in an
// unexported field the line writes by its properties, so Give's name bit is set, and Giver's bits through it, the
// interface's own walk reading Pad by its name alone. Gives, which calls Give, and IsGiver, which asserts to Giver, are
// dependent. Keeper, the same over a fixed layout, keeps both bits unset, and neither Keeps nor IsKeeper is dependent.
// Read a method's parameters in place of its results and Give's name bit is unset, and Giver's with it.
func TestAnInterfaceMethodReadsWhatItsResultsObserve(test *testing.T) {
	test.Parallel()

	host, dependence := besideAHostOnlyWidth(test, `type Pad struct{ _ [Width]byte }

type Giver interface{ Give() struct{ _ Pad } }

func Gives(giver Giver) any { return giver.Give() }

func IsGiver(value any) bool {
	_, isGiver := value.(Giver)
	return isGiver
}

type Fixed struct{ _ [1]byte }

type Keeper interface{ Keep() struct{ _ Fixed } }

func Keeps(keeper Keeper) any { return keeper.Keep() }

func IsKeeper(value any) bool {
	_, isKeeper := value.(Keeper)
	return isKeeper
}
`)
	expectNameBits(test, dependence, map[string]Bits{
		"Pad":    {Underlying: true},
		"Giver":  {Name: true, Underlying: true},
		"Fixed":  {},
		"Keeper": {},
	})
	expectVerdicts(test, verdicts(test, host, dependence), map[string]bool{
		"type Pad":      true,
		"type Giver":    true,
		"func Gives":    true,
		"func IsGiver":  true,
		"type Fixed":    false,
		"type Keeper":   false,
		"func Keeps":    false,
		"func IsKeeper": false,
		"value Width":   true,
	})
}
