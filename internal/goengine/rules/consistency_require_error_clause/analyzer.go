// Package consistency_require_error_clause is the Go engine's rule that an error's text is a lowercase clause that
// reads right after a caller wraps it.
//
// An error travels up through callers that each wrap it with what they were doing, so the line a reader finally sees
// is every layer's text joined by colons: `loading settings: opening config: permission denied`. Each layer's text is
// one clause of that sentence, so it opens lowercase unless it opens on a proper name, it ends without a mark that
// would land mid-line, and it says what was being done or what went wrong rather than that something failed, which
// every layer of an error already says.
package consistency_require_error_clause

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-error-clause"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_require_error_clause",
	Doc: "Flags the literal text given to errors.New or fmt.Errorf when it opens with a capital that is no proper " +
		"name, ends with a period, exclamation mark or colon, or opens with failed to, failure, error:, err:, unable " +
		"to or could not, since an error's text is a clause that reads right after a caller wraps it. A text that is " +
		"not a string literal is not judged, and neither is whether the clause is a gerund, which cannot be told " +
		"without a dictionary.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var capitalizedOpeningMessage = policy.MessageOf(Name, "capitalizedOpening")

var trailingPunctuationMessage = policy.MessageOf(Name, "trailingPunctuation")

var failurePrefixMessage = policy.MessageOf(Name, "failurePrefix")

// failurePrefixes are the openings that say only that something failed, matched without regard to case.
var failurePrefixes = []string{"failed to", "failure", "error:", "err:", "unable to", "could not"}

// trailingMarks are the marks an error's text may not end on, since each lands mid-line once a caller wraps it.
const trailingMarks = ".!:"

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall || len(call.Args) == 0 || !isErrorConstructor(pass, call) {
				return true
			}
			argument := ast.Unparen(call.Args[0])
			if !isLiteralText(argument) {
				return true
			}
			value := pass.TypesInfo.Types[argument].Value
			// A package that fails to type-check is never run, so a literal always has its value. One that somehow
			// does not is left alone, since a finding guessed without types is worse than none.
			if value == nil || value.Kind() != constant.String {
				return true
			}
			judge(pass, argument, constant.StringVal(value))
			return true
		})
	}
	return nil, nil
}

// isErrorConstructor is whether call calls the standard library's errors.New or fmt.Errorf, resolved by the callee's
// package and name so a function of the same name anywhere else, or a method, is never mistaken for either.
func isErrorConstructor(pass *analysis.Pass, call *ast.CallExpr) bool {
	function, isFunc := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunc || function.Pkg() == nil || function.Signature().Recv() != nil {
		return false
	}
	switch function.Pkg().Path() {
	case "errors":
		return function.Name() == "New"
	case "fmt":
		return function.Name() == "Errorf"
	}
	return false
}

// isLiteralText is whether expression is written out where the error is made: a string literal, or literals joined
// by +, as a long text is wrapped across lines. A named constant or anything built from a variable is not, so the
// finding always lands on the text it is about.
func isLiteralText(expression ast.Expr) bool {
	switch typed := ast.Unparen(expression).(type) {
	case *ast.BasicLit:
		return typed.Kind == token.STRING
	case *ast.BinaryExpr:
		return typed.Op == token.ADD && isLiteralText(typed.X) && isLiteralText(typed.Y)
	}
	return false
}

// judge reports each problem with text, the value of the literal argument, at that literal.
func judge(pass *analysis.Pass, argument ast.Expr, text string) {
	// A failure prefix is also the opening, so a capitalized one, `Failed to`, is one problem and not two: removing
	// the prefix removes the capital with it.
	if prefix, found := failurePrefixOf(text); found {
		report(pass, argument, failurePrefixMessage, map[string]string{"prefix": prefix})
	} else if word := openingWord(text); word != "" && !isProperName(pass, argument.Pos(), word) {
		report(pass, argument, capitalizedOpeningMessage, map[string]string{"word": word})
	}
	// Trailing space or a newline after the mark still leaves the mark at the end of the clause.
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	last, _ := utf8.DecodeLastRuneInString(trimmed)
	if trimmed != "" && strings.ContainsRune(trailingMarks, last) {
		report(pass, argument, trailingPunctuationMessage, map[string]string{"punctuation": string(last)})
	}
}

// failurePrefixOf is the failure prefix text opens with, spelled as text spells it. A prefix ending on a letter must
// end a word there, so `failures` or `could nothing` is not `failure` or `could not`.
func failurePrefixOf(text string) (string, bool) {
	for _, prefix := range failurePrefixes {
		if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
			continue
		}
		next, _ := utf8.DecodeRuneInString(text[len(prefix):])
		if strings.HasSuffix(prefix, ":") || !(unicode.IsLetter(next) || unicode.IsDigit(next)) {
			return text[:len(prefix)], true
		}
	}
	return "", false
}

// openingWord is the identifier-shaped word text opens with when it opens on a capital, or empty when it opens on
// anything else: a lowercase word, a verb such as %s, a digit or a mark.
func openingWord(text string) string {
	first, _ := utf8.DecodeRuneInString(text)
	if !unicode.IsUpper(first) {
		return ""
	}
	end := strings.IndexFunc(text, func(character rune) bool {
		return !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_')
	})
	if end == -1 {
		return text
	}
	return text[:end]
}

// isProperName is whether word, which opens on a capital, is a name that keeps its capital in any sentence, decided
// without a list: Go itself; a word with a capital after its first letter, which covers an initialism such as JSON
// and a name such as TypeScript; or an exported name declared where the error is made, such as a type the text
// describes.
func isProperName(pass *analysis.Pass, position token.Pos, word string) bool {
	if word == "Go" {
		return true
	}
	_, firstSize := utf8.DecodeRuneInString(word)
	if strings.IndexFunc(word[firstSize:], unicode.IsUpper) != -1 {
		return true
	}
	// The innermost scope at the literal sees the function's own names as well as the file's imports and the
	// package's declarations, and the position keeps out a local declared after the call.
	scope := pass.Pkg.Scope().Innermost(position)
	if scope == nil {
		scope = pass.Pkg.Scope()
	}
	_, object := scope.LookupParent(word, position)
	return object != nil && token.IsExported(word)
}

// report flags argument with message rendered from values.
func report(pass *analysis.Pass, argument ast.Expr, message policy.MessageHandle, values map[string]string) {
	pass.Report(analysis.Diagnostic{
		Pos:      argument.Pos(),
		End:      argument.End(),
		Category: message.Id,
		Message:  message.Render(values),
	})
}
