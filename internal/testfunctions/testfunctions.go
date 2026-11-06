// Package testfunctions holds go test's check of test function signatures, read from syntax alone, so both
// loaders, the tests' gotypes and the Go engine's, refuse the same functions go test refuses before it builds a
// test's main. go list reports only the first such function in a package; this names every one.
package testfunctions

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Problem is one test function go test refuses, where its declaration starts and what go test says of it.
type Problem struct {
	Position token.Position
	Message  string
}

// Problems is what go test refuses in test files before it builds a test's main: a Test, Benchmark or Fuzz
// function, or TestMain, whose signature is not the one its kind must have, as go's own check reads it
// (cmd/go/internal/load's checkTestFunc: one parameter, a pointer to a type named T, B, F or M, no results, no
// type parameters).
func Problems(fileSet *token.FileSet, files []*ast.File) []Problem {
	var found []Problem
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			name := function.Name.Name
			kind := ""
			switch {
			case name == "TestMain":
				if isTestFunction(function, "T") {
					continue
				}
				kind = "M"
			case isTestName(name, "Test"):
				kind = "T"
			case isTestName(name, "Benchmark"):
				kind = "B"
			case isTestName(name, "Fuzz"):
				kind = "F"
			default:
				continue
			}
			message := ""
			if function.Type.TypeParams != nil && function.Type.TypeParams.NumFields() > 0 {
				message = "test functions cannot have type parameters"
			} else if !isTestFunction(function, kind) {
				message = fmt.Sprintf("wrong signature for %s, must be: func %s(%s *testing.%s)",
					name, name, strings.ToLower(kind), kind)
			}
			if message != "" {
				found = append(found, Problem{Position: fileSet.Position(function.Pos()), Message: message})
			}
		}
	}
	return found
}

// isTestName is go test's isTest: name is prefix followed by nothing or by something that is not a lower-case
// letter, so TestMain and Test are tests and Testify is not.
func isTestName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(next)
}

// isTestFunction is go test's isTestFunc: one parameter, of type *T or *anything.T for kind T, and no results.
func isTestFunction(function *ast.FuncDecl, kind string) bool {
	if function.Type.Results != nil && len(function.Type.Results.List) > 0 || function.Type.Params.List == nil ||
		len(function.Type.Params.List) != 1 || len(function.Type.Params.List[0].Names) > 1 {
		return false
	}
	pointer, ok := function.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	if name, ok := pointer.X.(*ast.Ident); ok && name.Name == kind {
		return true
	}
	if selector, ok := pointer.X.(*ast.SelectorExpr); ok && selector.Sel.Name == kind {
		return true
	}
	return false
}
