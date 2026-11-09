// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation_test

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math/big"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

func TestConstString(t *testing.T) {
	const source = `
	package P

	type Named string

	func fn() (int, bool, string)
	func gen[T int]() {}
	`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}

	var conf types.Config
	pkg, err := conf.Check("P", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		expr     string // type expression
		constant any    // constant value
		want     string // expected String() value
	}{
		{"int", int64(0), "0:int"},
		{"int64", int64(0), "0:int64"},
		{"float32", int64(0), "0:float32"},
		{"float32", big.NewFloat(1.5), "1.5:float32"},
		{"bool", false, "false:bool"},
		{"string", "", `"":string`},
		{"Named", "", `"":P.Named`},
		{"struct{x string}", nil, "{}:struct{x string}"},
		{"[]int", nil, "nil:[]int"},
		{"[3]int", nil, "{}:[3]int"},
		{"*int", nil, "nil:*int"},
		{"interface{}", nil, "nil:interface{}"},
		{"interface{string}", nil, `"":interface{string}`},
		{"interface{int|int64}", nil, "0:interface{int|int64}"},
		{"interface{bool}", nil, "false:interface{bool}"},
		{"interface{bool|int}", nil, "invalid:interface{bool|int}"},
		{"interface{int|string}", nil, "invalid:interface{int|string}"},
		{"interface{bool|string}", nil, "invalid:interface{bool|string}"},
		{"interface{struct{x string}}", nil, "invalid:interface{struct{x string}}"},
		{"interface{int|int64}", int64(1), "1:interface{int|int64}"},
		{"interface{~bool}", true, "true:interface{~bool}"},
		{"interface{Named}", "lorem ipsum", `"lorem ipsum":interface{P.Named}`},
		{"func() (int, bool, string)", nil, "nil:func() (int, bool, string)"},
	} {
		// Eval() expr for its type.
		tv, evalError := types.Eval(fset, pkg, 0, test.expr)
		if evalError != nil {
			t.Fatalf("Eval(%s) failed: %v", test.expr, evalError)
		}
		var value constant.Value
		if test.constant != nil {
			value = constant.Make(test.constant)
		}
		c := intermediate_representation.NewConstant(value, tv.Type, nil)
		got := strings.ReplaceAll(c.String(), " | ", "|") // Accept both interface{a | b} and interface{a|b}.
		if got != test.want {
			t.Errorf(
				"intermediate_representation.NewConstant(%v, %s).String() = %q, want %q",
				value,
				tv.Type,
				got,
				test.want,
			)
		}
	}

	// Test tuples
	fn := pkg.Scope().Lookup("fn")
	tup := fn.Type().(*types.Signature).Results()
	if got, want := intermediate_representation.NewConstant(
		nil,
		tup,
		nil,
	).String(), `(0, false, ""):(int, bool, string)`; got != want {
		t.Errorf("intermediate_representation.NewConstant(%v, %s).String() = %v, want %v", nil, tup, got, want)
	}

	// Test type-param
	gen := pkg.Scope().Lookup("gen")
	tp := gen.Type().(*types.Signature).TypeParams().At(0)
	if got, want := intermediate_representation.NewConstant(nil, tp, nil).String(), "0:T"; got != want {
		t.Errorf("intermediate_representation.NewConstant(%v, %s).String() = %v, want %v", nil, tup, got, want)
	}
}
