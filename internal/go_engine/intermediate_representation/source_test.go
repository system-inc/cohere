// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation_test

// go/ir's source_test.go but for TestObjValueLookup and the ValueForExpr tests, which read the expression
// map that comes with lifting (#ee33njz).

import (
	"go/version"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

func TestEnclosingFunction(test *testing.T) {
	type testcase struct {
		desc   string
		input  string // the input file
		substr string // first occurrence of this string denotes interval
		fn     string // name of expected containing function
	}
	tests := []testcase{
		// We use distinctive numbers as syntactic landmarks.
		{
			"Ordinary function", `
		  package main
		  func f() { println(1003) }`,
			"100", "main.f",
		},
		{
			"Methods", `
		  package main
          type T int
		  func (t T) f() { println(200) }`,
			"200", "(main.T).f",
		},
		{
			"Function literal", `
		  package main
		  func f() { println(func() { print(300) }) }`,
			"300", "main.f$1",
		},
		{
			"Doubly nested", `
		  package main
		  func f() { println(func() { print(func() { print(350) })})}`,
			"350", "main.f$1$1",
		},
		{
			"Implicit init for package-level var initializer", `
		  package main; var a = 400`,
			"400", "main.init",
		},
		{"No code for constants", "package main; const a = 500", "500", "(none)"},
		{" Explicit init", "package main; func init() { println(600) }", "600", "main.init#1"},
		{
			"Multiple explicit init functions", `
		  package main
		  func init() { println("foo") }
		  func init() { println(800) }`,
			"800", "main.init#2",
		},
		{
			"init containing FuncLit", `
		  package main
		  func init() { println(func(){print(900)}) }`,
			"900", "main.init#1$1",
		},
		{
			"generic", `
		    package main
			type S[T any] struct{}
			func (*S[T]) Foo() { println(1000) }
			type P[T any] struct{ *S[T] }`,
			"1000", "(*main.S[T]).Foo",
		},
	}
	if version.Compare(runtime.Version(), "go1.27") >= 0 {
		tests = append(tests, testcase{
			"generic method", `
package main
	type S struct{}
	func (S) Foo[T any]() { println(1100) }
	type P[T any] struct{ S }`,
			"1100", "(main.S).Foo",
		})
	}
	for _, testCase := range tests {
		test.Run(testCase.desc, func(test *testing.T) {
			goEnginePackage, file := buildPackage(test, testCase.input, nil)
			fset := goEnginePackage.Program.FileSet

			// Find [start,end) positions of the first occurrence of substr in file.
			index := strings.Index(testCase.input, testCase.substr)
			if index < 0 {
				test.Fatalf("%q is not a substring of input", testCase.substr)
			}
			filePos := fset.File(file.Package)
			start, end := filePos.Pos(index), filePos.Pos(index+len(testCase.substr))

			path, exact := astutil.PathEnclosingInterval(file, start, end)
			if !exact {
				test.Fatalf("PathEnclosingInterval(%q) not exact", testCase.substr)
			}

			name := "(none)"
			function := intermediate_representation.EnclosingFunction(goEnginePackage, path)
			if function != nil {
				name = function.String()
			}

			if name != testCase.fn {
				test.Errorf("EnclosingFunction(%q in %q) got %s, want %s",
					testCase.substr, testCase.input, name, testCase.fn)
			}

			// While we're here: test HasEnclosingFunction.
			if has := intermediate_representation.HasEnclosingFunction(
				goEnginePackage,
				path,
			); has != (function != nil) {
				test.Errorf("HasEnclosingFunction(%q in %q) got %v, want %v",
					testCase.substr, testCase.input, has, function != nil)
			}
		})
	}
}
