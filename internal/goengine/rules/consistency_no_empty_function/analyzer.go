// Package consistency_no_empty_function is the Go engine's rule that an empty function body says why it is empty.
//
// An empty body reads the same whether it is a deliberate no-op, a method an interface asks for that this type has
// nothing to do in, a callback that ignores what it is handed, or work someone meant to write and forgot. Only its
// author knows which, so the body says so in a comment inside its braces, where the next reader looks first. An empty
// test is the sharpest case: it passes every run and can never fail.
package consistency_no_empty_function

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-empty-function"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_empty_function",
	Doc: "Flags a function, method or function literal whose body holds no statement and no comment with words in " +
		"it between its braces, tests included, since an empty test can never fail. A declaration with no body, " +
		"implemented in assembly or linked by name, has nothing to judge, and a generated file is left alone.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads only the syntax tree and its comments.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var emptyFunctionMessage = policy.MessageOf(Name, "emptyFunction")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// A generated file is its generator's to write, and the generator's next run undoes a comment added by hand.
		// This is the one place the rule skips a file, so a driver filter can take it over by deleting these lines.
		if ast.IsGenerated(file) {
			continue
		}
		judgeFile(pass, file)
	}
	return nil, nil
}

// judgeFile reports every empty function body in file that holds no comment saying why.
func judgeFile(pass *analysis.Pass, file *ast.File) {
	// Each comment that has words in it, by where it opens. The parser gathers file.Comments in source order, so the
	// slice is sorted and a body's comments are found by a binary search for its opening brace.
	commentOpenings := []token.Pos{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if hasWords(comment) {
				commentOpenings = append(commentOpenings, comment.Slash)
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		var body *ast.BlockStmt
		switch function := node.(type) {
		case *ast.FuncDecl:
			body = function.Body
		case *ast.FuncLit:
			body = function.Body
		default:
			return true
		}
		// A declaration without a body is implemented elsewhere, in assembly or through a linkname directive.
		if body == nil || !holdsNoStatement(body) {
			return true
		}
		// The first comment opening after the brace is inside the body when it opens before the closing brace.
		index, _ := slices.BinarySearch(commentOpenings, body.Lbrace+1)
		if index < len(commentOpenings) && commentOpenings[index] < body.Rbrace {
			return true
		}
		pass.Report(analysis.Diagnostic{
			Pos:      body.Lbrace,
			End:      body.Rbrace + 1,
			Category: emptyFunctionMessage.Id,
			Message:  emptyFunctionMessage.Render(map[string]string{}),
		})
		return true
	})
}

// holdsNoStatement is whether body has nothing in it to run. A semicolon written alone parses as an empty statement,
// which runs nothing and which gofmt drops, so `{ ; }` is as empty as `{}`.
func holdsNoStatement(body *ast.BlockStmt) bool {
	for _, statement := range body.List {
		if _, isEmpty := statement.(*ast.EmptyStmt); !isEmpty {
			return false
		}
	}
	return true
}

// hasWords is whether comment says anything once its slashes, stars and spaces are set aside, so `//` or `/**/`
// alone inside a body tells the next reader nothing and excuses nothing.
func hasWords(comment *ast.Comment) bool {
	return strings.TrimFunc(comment.Text, func(character rune) bool {
		return character == '/' || character == '*' || unicode.IsSpace(character)
	}) != ""
}
