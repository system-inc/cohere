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
