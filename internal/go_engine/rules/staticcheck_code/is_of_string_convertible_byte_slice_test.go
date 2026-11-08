package staticcheck_code_test

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Both sides judge alike whether every expression of the byte slice fixtures converts to a string as a slice of
// bytes, in three settings of language version: a package loaded outside a module, where the type checker records no
// version even for its file tagged go1.18, a module at go 1.17 with a file tagged newer, and a module at go 1.22
// with a file tagged older. An
// expression of no type, such as a package's name, panics on both sides. The test fails unless a slice of a type
// defined on byte was met both where it converts and where it does not, a slice of bytes, a near miss and an
// expression of no type, so a fixture that stops holding one cannot pass by asking nothing of it.
func TestIsOfStringConvertibleByteSliceMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareisofstringconvertiblebyteslice", []fixtureLoad{
		pathFixture("byte_slices"), moduleFixture("byte_slices_older"), moduleFixture("byte_slices_newer"),
	}, compareIsOfStringConvertibleByteSlice)
	requireMet(test, met, []string{
		"a slice of bytes, true", "a slice of a type defined on byte, true", "a slice of a type defined on byte, false",
		"no slice of bytes, false", "an expression of no type, a panic",
		"of no language version, true", "of no language version, false",
		"of go1.17, true", "of go1.17, false", "of go1.21, true", "of go1.22, true",
	})
}

// compareIsOfStringConvertibleByteSlice asks both sides of every expression of the pass's package whether it is a
// slice of bytes that converts to a string, each question labeled by its element and by its language version.
func compareIsOfStringConvertibleByteSlice(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			position := pass.Fset.Position(expression.Pos())
			upstream := func() bool { return code.IsOfStringConvertibleByteSlice(pass, expression) }
			port := func() bool { return staticcheck_code.IsOfStringConvertibleByteSlice(context, expression) }
			element := byteSliceElementLabel(pass.TypesInfo.TypeOf(expression))
			byElement := ask("IsOfStringConvertibleByteSlice", position, "", upstream, port)
			byElement.label = element + ", " + byElement.upstream
			if element == "an expression of no type" {
				byElement.label = element + ", a panic"
			}
			questions = append(questions, byElement)
			if element != "a slice of a type defined on byte" && element != "a slice of bytes" {
				return true
			}
			languageVersion := staticcheck_report.LanguageVersion(context, expression)
			if languageVersion == "" {
				languageVersion = "no language version"
			}
			byVersion := ask("IsOfStringConvertibleByteSlice", position, "", upstream, port)
			byVersion.label = "of " + languageVersion + ", " + byVersion.upstream
			questions = append(questions, byVersion)
			return true
		})
	}
	return questions
}

// byteSliceElementLabel is what kind of slice of bytes typed is, if any: of byte itself, of a type defined on byte,
// or none, and whether it has a type at all.
func byteSliceElementLabel(typed types.Type) string {
	if typed == nil {
		return "an expression of no type"
	}
	slice, isSlice := typed.Underlying().(*types.Slice)
	if !isSlice {
		return "no slice of bytes"
	}
	element := types.Unalias(slice.Elem())
	switch {
	case types.Identical(element, types.Typ[types.Byte]):
		return "a slice of bytes"
	case types.Identical(element.Underlying(), types.Typ[types.Byte]):
		return "a slice of a type defined on byte"
	default:
		return "no slice of bytes"
	}
}
