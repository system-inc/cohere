package load

import (
	"cmp"
	"go/ast"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/test_functions"
)

// Kind is where a diagnostic came from: go list, the parser, or the type checker, which also reports what go
// test refuses in a test function's signature. Unknown is what go/packages reports without a kind.
type Kind string

const (
	KindList    Kind = "List"
	KindParse   Kind = "Parse"
	KindType    Kind = "Type"
	KindUnknown Kind = "Unknown"
)

// Diagnostic is one error the load met. A diagnostic whose error names no place has an empty File and a
// zero Line and Column, and is kept: an error is never dropped for lack of a position.
type Diagnostic struct {
	Kind Kind

	// File is absolute.
	File string

	// Line and Column are 1-based, Column counted in UTF-8 bytes as go/token counts it; either is 0 when the
	// error does not say it.
	Line   int
	Column int

	Message string
}

// diagnostics is every error in the graph the roots reach, and every test function go test would refuse in
// the test variants to analyze, each once, sorted. The same error is often reported by both p and
// "p [p.test]", and go list names the first wrongly signed test function on go test's main as well, with the
// position in its message, so a diagnostic is one per file, position and message whatever its kind; the
// signature check goes first so its kind is the one kept. testFunctions is each test variant to analyze's refused
// test functions, by root, judged as it was type-checked (testFunctionDiagnostics), since by the load's end an
// analysis beside it may have let the variant's syntax go.
func diagnostics(
	workspace string,
	roots []*packages.Package,
	testFunctions map[*packages.Package][]Diagnostic,
) []Diagnostic {
	var found []Diagnostic
	for _, root := range roots {
		found = append(found, testFunctions[root]...)
	}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		for _, packageError := range visited.Errors {
			found = append(found, diagnosticOf(workspace, packageError))
		}
	})

	type identity struct {
		file         string
		line, column int
		message      string
	}
	seen := map[identity]bool{}
	var unique []Diagnostic
	for _, diagnostic := range found {
		key := identity{diagnostic.File, diagnostic.Line, diagnostic.Column, diagnostic.Message}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, diagnostic)
		}
	}
	slices.SortStableFunc(unique, func(left, right Diagnostic) int {
		return cmp.Or(
			strings.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			strings.Compare(left.Message, right.Message),
		)
	})
	return unique
}

// testFunctionDiagnostics is every test function go test would refuse in a test variant's test files, read from its
// syntax.
func testFunctionDiagnostics(testVariant *packages.Package) []Diagnostic {
	var testFiles []*ast.File
	for _, file := range testVariant.Syntax {
		if strings.HasSuffix(testVariant.Fset.Position(file.FileStart).Filename, "_test.go") {
			testFiles = append(testFiles, file)
		}
	}
	var found []Diagnostic
	for _, problem := range test_functions.Problems(testVariant.Fset, testFiles) {
		found = append(found, Diagnostic{
			Kind:    KindType,
			File:    problem.Position.Filename,
			Line:    problem.Position.Line,
			Column:  problem.Position.Column,
			Message: problem.Message,
		})
	}
	return found
}

// diagnosticOf is one go/packages error as a diagnostic. Its Pos is "file:line:col", "file:line", "file",
// "-" or empty, and a go list position is relative to the directory go list ran in. go list names a wrongly
// signed test function on go test's main with no Pos and the position leading the message, so an error
// without a Pos whose message starts with an absolute position is read as that position and the rest.
func diagnosticOf(workspace string, packageError packages.Error) Diagnostic {
	diagnostic := Diagnostic{Kind: kindOf(packageError.Kind), Message: packageError.Msg}
	if packageError.Pos == "" || packageError.Pos == "-" {
		file, line, column, rest, found := leadingPosition(packageError.Msg)
		if found {
			diagnostic.File, diagnostic.Line, diagnostic.Column, diagnostic.Message = file, line, column, rest
		}
		return diagnostic
	}
	diagnostic.File, diagnostic.Line, diagnostic.Column = position(packageError.Pos)
	if !filepath.IsAbs(diagnostic.File) {
		diagnostic.File = filepath.Join(workspace, diagnostic.File)
	}
	return diagnostic
}

// leadingPosition reads the absolute "file:line:col: " or "file:line: " that starts message, and the rest
// after it. A file's name can hold ": " itself, so each one is tried in turn.
func leadingPosition(message string) (string, int, int, string, bool) {
	for offset := 0; ; {
		index := strings.Index(message[offset:], ": ")
		if index < 0 {
			return "", 0, 0, "", false
		}
		file, line, column := position(message[:offset+index])
		if filepath.IsAbs(file) && line > 0 {
			return file, line, column, message[offset+index+len(": "):], true
		}
		offset += index + 1
	}
}

// position splits "file:line:col", "file:line" or "file" into its parts, reading the numbers from the right
// so a colon in the file's own name, such as a Windows drive's, stays in the file.
func position(text string) (string, int, int) {
	file := text
	var numbers []int
	for len(numbers) < 2 {
		index := strings.LastIndexByte(file, ':')
		if index < 0 {
			break
		}
		number, numberError := strconv.Atoi(file[index+1:])
		if numberError != nil || number < 1 {
			break
		}
		numbers = append([]int{number}, numbers...)
		file = file[:index]
	}
	switch len(numbers) {
	case 2:
		return file, numbers[0], numbers[1]
	case 1:
		return file, numbers[0], 0
	default:
		return file, 0, 0
	}
}

// kindOf is go/packages' error kind as a Kind.
func kindOf(kind packages.ErrorKind) Kind {
	switch kind {
	case packages.ListError:
		return KindList
	case packages.ParseError:
		return KindParse
	case packages.TypeError:
		return KindType
	default:
		return KindUnknown
	}
}
