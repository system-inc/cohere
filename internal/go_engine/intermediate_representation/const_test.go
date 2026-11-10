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

func TestConstString(test *testing.T) {
	test.Parallel()
	const source = `
	package P

	type Named string

	func fn() (int, bool, string)
	func gen[T int]() {}
	`
	fileSet := token.NewFileSet()
	file, parseFileError := parser.ParseFile(fileSet, "p.go", source, 0)
	if parseFileError != nil {
		test.Fatal(parseFileError)
	}

	var conf types.Config
	goPackage, checkError := conf.Check("P", fileSet, []*ast.File{file}, nil)
	if checkError != nil {
		test.Fatal(checkError)
	}

	for _, testCase := range []struct {
		expression string // type expression
		constant   any    // constant value
		want       string // expected String() value
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
		tv, evalError := types.Eval(fileSet, goPackage, 0, testCase.expression)
		if evalError != nil {
			test.Fatalf("Eval(%s) failed: %v", testCase.expression, evalError)
		}
		var value constant.Value
		if testCase.constant != nil {
			value = constant.Make(testCase.constant)
		}
		irConstant := intermediate_representation.NewConstant(value, tv.Type, nil)
		got := strings.ReplaceAll(irConstant.String(), " | ", "|") // Accept both interface{a | b} and interface{a|b}.
		if got != testCase.want {
			test.Errorf(
				"intermediate_representation.NewConstant(%v, %s).String() = %q, want %q",
				value,
				tv.Type,
				got,
				testCase.want,
			)
		}
	}

	// Test tuples
	function := goPackage.Scope().Lookup("fn")
	functionSignature, isSignature := function.Type().(*types.Signature)
	if !isSignature {
		test.Fatalf("fn has type %s, not a signature", function.Type())
	}
	tup := functionSignature.Results()
	if got, want := intermediate_representation.NewConstant(
		nil,
		tup,
		nil,
	).String(), `(0, false, ""):(int, bool, string)`; got != want {
		test.Errorf("intermediate_representation.NewConstant(%v, %s).String() = %v, want %v", nil, tup, got, want)
	}

	// Test type-param
	gen := goPackage.Scope().Lookup("gen")
	genSignature, isSignature := gen.Type().(*types.Signature)
	if !isSignature {
		test.Fatalf("gen has type %s, not a signature", gen.Type())
	}
	tp := genSignature.TypeParams().At(0)
	if got, want := intermediate_representation.NewConstant(nil, tp, nil).String(), "0:T"; got != want {
		test.Errorf("intermediate_representation.NewConstant(%v, %s).String() = %v, want %v", nil, tup, got, want)
	}
}
