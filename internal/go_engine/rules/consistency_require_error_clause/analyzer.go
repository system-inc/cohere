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

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-error-clause"

// listener hears every call of the syntax dispatcher's walk.
var listener = rule.Listener{Nodes: []ast.Node{(*ast.CallExpr)(nil)}, Node: judgeCall}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_require_error_clause", "Flags the literal text given to "+
	"errors.New or fmt.Errorf when it opens on an ordinary English word from policy/ErrorOpeningWords.json written "+
	"with a sentence's capital, ends with a period, exclamation mark or colon, or opens with failed to, failure, "+
	"error:, err:, unable to or could not, since an error's text is a clause that reads right after a caller wraps "+
	"it. A capitalized word outside the vocabulary is never flagged, since a proper name cannot be told by its shape. "+
	"A text that is not a string literal is not judged, and neither is whether the clause is a gerund, which cannot "+
	"be told without a dictionary.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var capitalizedOpeningMessage = policy.MessageOf(Name, "capitalizedOpening")

var trailingPunctuationMessage = policy.MessageOf(Name, "trailingPunctuation")

var failurePrefixMessage = policy.MessageOf(Name, "failurePrefix")

// failurePrefixes are the openings that say only that something failed, matched without regard to case.
var failurePrefixes = []string{"failed to", "failure", "error:", "err:", "unable to", "could not"}

// trailingMarks are the marks an error's text may not end on, since each lands mid-line once a caller wraps it.
const trailingMarks = ".!:"

// judgeCall judges the literal text a call to errors.New or fmt.Errorf is given.
func judgeCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 || !isErrorConstructor(context, call) {
		return
	}
	argument := ast.Unparen(call.Args[0])
	if !isLiteralText(argument) {
		return
	}
	value := context.TypesInfo.Types[argument].Value
	// A package that fails to type-check is never run, so a literal always has its value. One that somehow does not
	// is left alone, since a finding guessed without types is worse than none.
	if value == nil || value.Kind() != constant.String {
		return
	}
	judge(context, argument, constant.StringVal(value))
}

// isErrorConstructor is whether call calls the standard library's errors.New or fmt.Errorf, resolved by the callee's
// package and name so a function of the same name anywhere else, or a method, is never mistaken for either.
func isErrorConstructor(context *rule.Context, call *ast.CallExpr) bool {
	function, isFunc := typeutil.Callee(context.TypesInfo, call).(*types.Func)
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
func judge(context *rule.Context, argument ast.Expr, text string) {
	// A failure prefix is also the opening, so a capitalized one, `Failed to`, is one problem and not two: removing
	// the prefix removes the capital with it.
	if prefix, found := failurePrefixOf(text); found {
		report(context, argument, failurePrefixMessage, map[string]string{"prefix": prefix})
	} else if word := sentenceCapitalOpening(text); word != "" {
		report(context, argument, capitalizedOpeningMessage, map[string]string{"word": word})
	}
	// Trailing space or a newline after the mark still leaves the mark at the end of the clause.
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	last, _ := utf8.DecodeLastRuneInString(trimmed)
	if trimmed != "" && strings.ContainsRune(trailingMarks, last) {
		report(context, argument, trailingPunctuationMessage, map[string]string{"punctuation": string(last)})
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

// sentenceCapitalOpening is the word text opens on when that word is an ordinary English word from the house
// vocabulary written with a sentence's capital, `The` or `Couldn't`, or empty otherwise. A word outside the
// vocabulary may be a proper name, a language, a tool or another package's type, and no shape tells those apart, so
// it is never flagged. The word runs over letters, digits, underscores and apostrophes, so `Couldn't` is one word and
// an identifier such as `No_proxy` is read whole and stays quiet.
func sentenceCapitalOpening(text string) string {
	first, firstSize := utf8.DecodeRuneInString(text)
	if !unicode.IsUpper(first) {
		return ""
	}
	end := strings.IndexFunc(text, func(character rune) bool {
		return !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '\'')
	})
	if end == -1 {
		end = len(text)
	}
	word := text[:end]
	// A capital after the first letter is no sentence capital: `IT`, `NO` and `ON` are initialisms or codes that
	// happen to spell a vocabulary word, and a sentence would keep them as written.
	if strings.IndexFunc(word[firstSize:], unicode.IsUpper) != -1 {
		return ""
	}
	if !policy.ErrorOpeningWords.Holds(strings.ToLower(word)) {
		return ""
	}
	return word
}

// report flags argument with message rendered from values.
func report(context *rule.Context, argument ast.Expr, message policy.MessageHandle, values map[string]string) {
	context.Report(analysis.Diagnostic{
		Pos:      argument.Pos(),
		End:      argument.End(),
		Category: message.Id,
		Message:  message.Render(values),
	})
}
