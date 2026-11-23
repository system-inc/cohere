package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestImplicitConstraintInterfacesKeyApart checks two functions whose constraints become implicit interfaces, and
// requires each interface to be keyable and to key apart from the other, so the proof compares them instead of
// leaving them out.
func TestImplicitConstraintInterfacesKeyApart(t *testing.T) {
	t.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "constraints.go", `package constraints

func First[T ~int | ~string](value T) T { return value }

func Second[T ~float64 | ~uint8](value T) T { return value }
`, 0)
	if parseError != nil {
		t.Fatal(parseError)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if _, checkError := (&types.Config{}).Check("constraints", fileSet, []*ast.File{file}, info); checkError != nil {
		t.Fatal(checkError)
	}
	keys := map[expressionKey]string{}
	implicit := 0
	for expression, typeAndValue := range info.Types {
		if implicitConstraint(expression) == nil {
			continue
		}
		implicit++
		if !keyable(expression) {
			t.Errorf("%s isn't keyable", typeAndValue.Type)
		}
		key := keyOf(fileSet, expression)
		if earlier, held := keys[key]; held {
			t.Errorf("%s and %s share the key %v", earlier, typeAndValue.Type, key)
		}
		keys[key] = typeAndValue.Type.String()
	}
	if implicit != 2 {
		t.Fatalf("found %d implicit interfaces, want 2", implicit)
	}
}
