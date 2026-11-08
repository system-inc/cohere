package staticcheck_code_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"honnef.co/go/tools/pattern"

	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// syntaxPair is two spellings of syntax, the one bound first and the one recalled against it, and what both sides
// must find them, so a pair that stops meaning what it says fails rather than agreeing by accident.
type syntaxPair struct {
	bound    string
	recalled string
	same     bool
}

// expressionPairs are expressions compared as `(BinaryExpr x _ x)` compares its operands: alike and not, parentheses
// at every depth, a spread call, spellings of one object, every kind of literal and type, and the untagged field that
// is never the same as itself.
var expressionPairs = []syntaxPair{
	{"a", "a", true}, {"a", "b", false}, {"a", "(a)", true}, {"((a))", "a", true},
	{"f((a), b)", "f(a, ((b)))", true}, {"f(g((x)))", "(f)(g(x))", true},
	{"f(x...)", "f(x)", true}, {"f(x)", "f(x, y)", false},
	{"pkg.x", "x", false}, {"x.y", "x.y", true}, {"x.y", "x.(y)", false}, {"x", "y.x", false},
	{"1", "1", true}, {"1", "0x1", false}, {"'a'", "'a'", true}, {"`s`", "\"s\"", false}, {"1.5", "1.5", true},
	{"a + b", "a + b", true}, {"a + b", "a - b", false}, {"-a", "+a", false}, {"<-ch", "<-(ch)", true},
	{"chan int", "chan int", true}, {"chan int", "chan<- int", false}, {"map[string]int", "map[string]int", true},
	{"[]int{1, 2}", "[]int{1, (2)}", true}, {"T{a: 1}", "T{a: 1}", true}, {"T{a: 1}", "T{b: 1}", false},
	{"x[1:2]", "x[1:2]", true}, {"x[1:2]", "x[1:2:3]", false}, {"x[1:]", "x[1:2]", false}, {"x[:]", "x[:]", true},
	{"x[i]", "x[(i)]", true}, {"f[int](x)", "f[int](x)", true}, {"f[int, string]", "f[int, string]", true},
	{"x.(T)", "x.(T)", true}, {"&a", "&(a)", true}, {"*p", "*(p)", true}, {"[3]int", "[3]int", true},
	{"[...]int{1}", "[...]int{1}", true},
	{"struct{}", "struct{}", true}, {"struct{ x int }", "struct{ x int }", false},
	{"struct{ x int `tag` }", "struct{ x int `tag` }", true}, {"struct{ x int `tag` }", "struct{ x int }", false},
	{"interface{}", "interface{}", true}, {"interface{ m() }", "interface{ m() }", false},
	{"func()", "func()", true}, {"func(int)", "func(int)", false}, {"func() int", "func() int", false},
	{"func() { a() }", "func() { (a)() }", true}, {"func() { a() }", "func() { b() }", false},
	{"func() {}", "func() { a() }", false},
}

// statementPairs are statements compared as `(IfStmt _ _ x x)` compares a body of the bound statement alone with an
// else of the recalled one: alike and not, statements the pattern language looks through, a block beside the
// statement it holds, declarations and every kind of statement a body holds. A recalled expression statement,
// declaration or labeled statement is shed to what it wraps before it meets the body's list, and what it wraps is
// no statement, so it is never the same as a body holding its very spelling, where a block holding it is.
var statementPairs = []syntaxPair{
	{"a()", "a()", false}, {"a()", "b()", false}, {"a()", "{ a() }", true}, {"a()", "{ a(); b() }", false},
	{"L: a()", "a()", false}, {"a()", "L: (a)()", false}, {"var x int", "var x int", false},
	{"var x int", "var y int", false}, {"x := 1", "x := 1", true}, {"x := 1", "x = 1", false},
	{"x++", "x++", true}, {"x++", "x--", false}, {"for {}", "for {}", true},
	{"for i := range x {}", "for i := range x {}", true}, {"for i := range x {}", "for i := range y {}", false},
	{"go f()", "defer f()", false}, {"return 1, 2", "return 1, (2)", true}, {"return", "return 1", false},
	{"if c { a() }", "if c { a() }", true}, {"if c { a() } else { b() }", "if c { a() }", false},
	{"switch x { case 1: a() }", "switch x { case 1: a() }", true}, {"select {}", "select {}", true},
	{"ch <- 1", "ch <- (1)", true}, {"{}", "{}", false}, {"type T int", "type T int", false},
	{"for {}", "{ for {} }", true}, {"break", "continue", false}, {"goto L", "goto L", true},
}

// SameSyntax answers as staticcheck's pattern language does when a binding that holds syntax is recalled, asked of
// the matcher itself: each expression pair is the two operands of an equality matched against `(BinaryExpr x _ x)`,
// and each statement pair the body and else of an if statement matched against `(IfStmt _ _ x x)`, and the port's
// answer for what the query recalled must be the matcher's. Each pair's expected answer is required too, and the
// test fails unless both answers were met for each kind of pair.
func TestSameSyntaxMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	expressionQuery := pattern.MustParse(`(BinaryExpr x _ x)`)
	statementQuery := pattern.MustParse(`(IfStmt _ _ x x)`)
	met := map[string]int{}
	for _, pair := range expressionPairs {
		comparison := &ast.BinaryExpr{X: parsedExpression(test, pair.bound), Op: token.EQL,
			Y: parsedExpression(test, pair.recalled)}
		_, upstream := pattern.Match(expressionQuery, comparison)
		port := staticcheck_code.SameSyntax(comparison.X, comparison.Y)
		met[requireSameSyntaxAnswer(test, "expressions", pair, upstream, port)]++
	}
	for _, pair := range statementPairs {
		conditional := &ast.IfStmt{Cond: ast.NewIdent("c"),
			Body: &ast.BlockStmt{List: []ast.Stmt{parsedStatement(test, pair.bound)}},
			Else: parsedStatement(test, pair.recalled)}
		_, upstream := pattern.Match(statementQuery, conditional)
		port := staticcheck_code.SameSyntax(conditional.Body, conditional.Else)
		met[requireSameSyntaxAnswer(test, "statements", pair, upstream, port)]++
	}
	test.Logf("%d expression pairs and %d statement pairs asked of each side", len(expressionPairs),
		len(statementPairs))
	requireMet(test, met, []string{"expressions, true", "expressions, false", "statements, true", "statements, false"})
}

// requireSameSyntaxAnswer fails where the port answered pair other than the matcher, or the matcher other than the
// pair expects, and is the label of the answer met.
func requireSameSyntaxAnswer(test *testing.T, kind string, pair syntaxPair, upstream bool, port bool) string {
	test.Helper()
	if upstream != port {
		test.Errorf("%s %q and %q: staticcheck's matcher answered %t, the port %t", kind, pair.bound, pair.recalled,
			upstream, port)
	}
	if upstream != pair.same {
		test.Errorf("%s %q and %q: staticcheck's matcher answered %t, where the pair expects %t", kind, pair.bound,
			pair.recalled, upstream, pair.same)
	}
	if upstream {
		return kind + ", true"
	}
	return kind + ", false"
}

// parsedExpression is source parsed as an expression.
func parsedExpression(test *testing.T, source string) ast.Expr {
	test.Helper()
	expression, parseError := parser.ParseExpr(source)
	if parseError != nil {
		test.Fatalf("parsing %q: %v", source, parseError)
	}
	return expression
}

// parsedStatement is source parsed as the one statement of a function's body.
func parsedStatement(test *testing.T, source string) ast.Stmt {
	test.Helper()
	file, parseError := parser.ParseFile(token.NewFileSet(), "statement.go",
		"package p\nfunc f() {\n"+source+"\n}\n", 0)
	if parseError != nil {
		test.Fatalf("parsing %q: %v", source, parseError)
	}
	function, isFunction := file.Decls[0].(*ast.FuncDecl)
	if !isFunction || len(function.Body.List) != 1 {
		test.Fatalf("parsing %q: no function of one statement", source)
	}
	return function.Body.List[0]
}
