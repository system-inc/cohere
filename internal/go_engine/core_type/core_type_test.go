package core_type_test

// Of answers a type that is no type parameter, interface or union from its underlying type, skipping the term set
// (#dmjqz1f). It must answer as staticcheck's typeutil.CoreType does for every type, those it skips and those it
// leaves to typeutil.CoreType.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
)

// coreTypeFixture declares a type of each kind the fast path answers and each it must leave to the term set: an
// interface with a core type, one without, an empty one, a union, type parameters with and without a core type,
// channels whose directions do and don't agree, an alias of a type parameter, and named types over all of them.
const coreTypeFixture = `package sample

type Integer interface{ ~int }
type Number interface{ ~int | ~float64 }
type Bytes interface{ ~[]byte }
type Receivers interface{ chan int | <-chan int }
type Mixed interface{ chan int | chan<- int | <-chan int }
type Any interface{}
type Stringer interface{ String() string }
type Named struct{ Field int }
type Pointer *Named
type List []Named
type Function func(int) error

func Generic[I Integer, N Number, B Bytes, R Receivers, M Mixed, A any, S Stringer](
	integer I, number N, bytes B, receive R, mixed M, anything A, stringer S) {
	type Local = I
	var local Local
	_, _, _, _, _, _, _, _ = integer, number, bytes, receive, mixed, anything, stringer, local
}

func Plain(named Named, pointer Pointer, list List, function Function, channel <-chan int, anything Any) {
	_, _, _, _, _, _ = named, pointer, list, function, channel, anything
	_ = map[string][2]Named{}
	_ = struct{ Inline int }{}
}
`

// TestOfAnswersAsTypeutilDoes holds Of to typeutil.CoreType, which computes the term set Of skips, at every type the
// fixture's check records and at the invalid type, and requires each side of the fast path to be met: a type it
// answers, an interface or type parameter whose core type isn't its underlying, and one with none. Without those, a
// fast path that answered an interface from its underlying type would pass here unseen.
func TestOfAnswersAsTypeutilDoes(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "sample.go", coreTypeFixture, 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}}
	if _, checkError := (&types.Config{}).Check("sample", fileSet, []*ast.File{file}, information); checkError != nil {
		test.Fatal(checkError)
	}
	candidates := []types.Type{types.Typ[types.Invalid]}
	for _, typeAndValue := range information.Types {
		candidates = append(candidates, typeAndValue.Type)
	}
	for _, object := range information.Defs {
		if object != nil {
			candidates = append(candidates, object.Type())
		}
	}
	// A type parameter with no constraint yet, as a type under construction has, which the term set refuses.
	candidates = append(candidates, types.NewTypeParam(types.NewTypeName(token.NoPos, nil, "Unbound", nil), nil))

	met := map[string]int{}
	for _, candidate := range candidates {
		want := typeutil.CoreType(candidate)
		got := core_type.Of(candidate)
		if got != want {
			test.Errorf("Of(%s): got %v, typeutil.CoreType's %v", candidate, got, want)
		}
		_, isTypeParameter := candidate.(*types.TypeParam)
		switch {
		case want == nil:
			met["no core type"]++
		case isTypeParameter || types.IsInterface(candidate):
			if want != candidate.Underlying() {
				met["a core type that isn't the underlying"]++
			}
		default:
			met["the underlying"]++
		}
	}
	for _, label := range []string{"no core type", "a core type that isn't the underlying", "the underlying"} {
		if met[label] == 0 {
			test.Errorf("no type whose core type is %s, so the comparison above can't tell a fast path that "+
				"answers it wrongly", label)
		}
	}
}
