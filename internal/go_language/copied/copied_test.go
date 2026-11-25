package copied

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// checkerHeader is the header of the copy of x/tools' checker, the shape every well-formed case below varies.
const checkerHeader = "// cohere:copied golang.org/x/tools@v0.50.0 go/analysis/checker/checker.go\n" +
	"// cohere:until x/tools resolves the TODO in Analyze\n" +
	"// and lets a dependency's syntax go.\n"

// TestReadAcceptsAWellFormedHeader reads a header with an until that runs on to a second line, stopped by a blank
// comment line, and a header that sits below other comments before the package clause.
func TestReadAcceptsAWellFormedHeader(test *testing.T) {
	test.Parallel()
	expected := Header{
		Module:  "golang.org/x/tools",
		Version: "v0.50.0",
		Path:    "go/analysis/checker/checker.go",
		Until:   "x/tools resolves the TODO in Analyze and lets a dependency's syntax go.",
	}
	for _, source := range []string{
		checkerHeader + "//\n// The rest is prose of ours.\n\npackage checker\n",
		"//go:build go1.24\n\n" + checkerHeader + "\n// Copyright 2024 The Go Authors.\n\npackage checker\n",
	} {
		header, declared, readError := Read(parseSource(test, source))
		if readError != nil || !declared {
			test.Fatalf("reading %q: declared %t, error %v, want a header", source, declared, readError)
		}
		if header != expected {
			test.Errorf("reading %q: got %+v, want %+v", source, header, expected)
		}
		if !IsCopied(parseSource(test, source)) {
			test.Errorf("IsCopied is false for %q", source)
		}
	}
}

// TestReadAcceptsACopyOfTheStandardLibrary reads a copy of the toolchain's own source, whose module is std and whose
// version is the Go release it is of.
func TestReadAcceptsACopyOfTheStandardLibrary(test *testing.T) {
	test.Parallel()
	source := "// cohere:copied std@go1.27.0 go/build/read.go\n// cohere:until go/build shares its reads\n\n" +
		"package build\n"
	header, declared, readError := Read(parseSource(test, source))
	if readError != nil || !declared {
		test.Fatalf("reading %q: declared %t, error %v, want a header", source, declared, readError)
	}
	expected := Header{Module: StandardLibrary, Version: "go1.27.0", Path: "go/build/read.go",
		Until: "go/build shares its reads"}
	if header != expected {
		test.Errorf("reading %q: got %+v, want %+v", source, header, expected)
	}
}

// TestReadDeclaresNothingForAFileOfOurs keeps a file that only mentions the directives, or names one after its
// package clause, ours, so the house rules still judge it.
func TestReadDeclaresNothingForAFileOfOurs(test *testing.T) {
	test.Parallel()
	for _, source := range []string{
		"package ours\n",
		"// Package ours writes its header as\n//\n//\t// cohere:copied example.com/upstream@v1.0.0 a.go\n" +
			"package ours\n",
		"// cohere: a marked change, which is no header.\npackage ours\n",
		"package ours\n\n" + checkerHeader,
	} {
		header, declared, readError := Read(parseSource(test, source))
		if declared || readError != nil || header != (Header{}) {
			test.Errorf("reading %q: got %+v, declared %t, error %v, want nothing declared", source, header,
				declared, readError)
		}
		if IsCopied(parseSource(test, source)) {
			test.Errorf("IsCopied is true for %q", source)
		}
	}
}

// TestReadRefusesAMalformedHeader is every way a header can break the grammar. Each is declared and an error, never
// a silent pass: IsCopied is false, so the house rules report on the file again.
func TestReadRefusesAMalformedHeader(test *testing.T) {
	test.Parallel()
	const until = "// cohere:until upstream ships the fix\n"
	for _, malformed := range []struct {
		name   string
		header string
		saying string
	}{
		{"no until", "// cohere:copied golang.org/x/tools@v0.50.0 a.go\n", "never says when it goes away"},
		{"an empty until", "// cohere:copied golang.org/x/tools@v0.50.0 a.go\n// cohere:until\n", "says nothing"},
		{"an until below a blank line", "// cohere:copied golang.org/x/tools@v0.50.0 a.go\n\n" + until,
			"never says when it goes away"},
		{"an until below prose", "// cohere:copied golang.org/x/tools@v0.50.0 a.go\n// prose\n" + until,
			"is where the cohere:until line goes"},
		{"an until alone", until, "has no cohere:copied line right above it"},
		{"no version", "// cohere:copied golang.org/x/tools a.go\n" + until, "names no version"},
		{"an empty version", "// cohere:copied golang.org/x/tools@ a.go\n" + until, "names no version"},
		{"a version that is not semantic", "// cohere:copied golang.org/x/tools@latest a.go\n" + until,
			"not a semantic version"},
		{"a version not canonical", "// cohere:copied golang.org/x/tools@v0.50 a.go\n" + until,
			"which the module cache spells v0.50.0"},
		{"no path", "// cohere:copied golang.org/x/tools@v0.50.0\n" + until, "has 1 fields"},
		{"a path outside the module", "// cohere:copied golang.org/x/tools@v0.50.0 ../a.go\n" + until,
			"no clean path"},
		{"a path that is not Go", "// cohere:copied golang.org/x/tools@v0.50.0 README.md\n" + until,
			"no clean path"},
		{"a misspelled directive", "//cohere:copied golang.org/x/tools@v0.50.0 a.go\n" + until, "is not spelled"},
		{"a misspelled until", "// cohere:copied golang.org/x/tools@v0.50.0 a.go\n// cohere:untilnow\n",
			"is not spelled"},
		{"a standard library copy of a language version", "// cohere:copied std@go1.27 a.go\n" + until,
			"which is no Go release"},
		{"a standard library copy of a semantic version", "// cohere:copied std@v1.27.0 a.go\n" + until,
			"which is no Go release"},
		{"two copies", "// cohere:copied golang.org/x/tools@v0.50.0 a.go\n" + until +
			"// cohere:copied golang.org/x/tools@v0.50.0 b.go\n" + until, "a second cohere:copied line"},
	} {
		test.Run(malformed.name, func(test *testing.T) {
			test.Parallel()
			source := malformed.header + "\npackage upstream\n"
			_, declared, readError := Read(parseSource(test, source))
			if !declared || readError == nil || !strings.Contains(readError.Error(), malformed.saying) {
				test.Errorf("reading %q: declared %t, error %v, want an error saying %q", source, declared,
					readError, malformed.saying)
			}
			if IsCopied(parseSource(test, source)) {
				test.Errorf("IsCopied is true for %q", source)
			}
		})
	}
}

func parseSource(test *testing.T, source string) *ast.File {
	test.Helper()
	file, parseError := parser.ParseFile(token.NewFileSet(), "source.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing %q: %v", source, parseError)
	}
	return file
}
