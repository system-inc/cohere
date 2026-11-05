package markertype_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/system-inc/cohere/internal/goengine/rules/markertype"
)

// IsMarkerOnly holds a type with only empty methods taking nothing and answering nothing to be a marker type, and any
// method with a parameter, a statement, a result or no body to be real, so a marker type that also has a real method
// is no marker type.
func TestIsMarkerOnlyTellsMarkersFromRealMethods(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		// methods is the source of A's method declarations, parsed in a file that declares A.
		methods  string
		expected bool
	}{
		{"one marker method", "func (A) kind() {}", true},
		{"a pointer receiver's marker method", "func (*A) kind() {}", true},
		{"several marker methods", "func (A) kind() {}\nfunc (A) node() {}", true},
		{"an exported marker method", "func (A) Kind() {}", true},
		{"a comment inside the body", "func (A) kind() {\n\t// A is a kind of node.\n}", true},
		{"a block comment inside the body", "func (A) kind() { /* A is a kind. */ }", true},
		{"an empty result list", "func (A) kind() () {}", true},
		// A method that takes something is an operation, and a null object performs it as nothing, so an empty body
		// does not make it a marker, whatever its parameters are called.
		{"a parameter and an empty body", "func (A) Trace(event string) {}", false},
		{"a blank parameter and an empty body", "func (A) kind(_ int) {}", false},
		{"an unnamed parameter and an empty body", "func (A) kind(int) {}", false},
		{"a marker beside a no-op taking a parameter", "func (A) kind() {}\nfunc (A) Trace(event string) {}", false},
		{"a statement in the body", "func (A) kind() { println() }", false},
		{"a bare return in the body", "func (A) kind() { return }", false},
		{"a result", "func (A) kind() int { return 0 }", false},
		{"a named result with a bare return", "func (A) kind() (depth int) { return }", false},
		// Go asks a function with a result to end in a return, so only the result tells this one from a marker. It
		// parses but does not compile, which is code a syntax-only caller can still be handed.
		{"a result over an empty body", "func (A) kind() int {}", false},
		{"no body, written in assembly", "func (A) kind()", false},
		{"a marker beside a real method", "func (A) kind() {}\nfunc (A) Name() string { return \"a\" }", false},
		{"a real method beside a marker", "func (A) Name() string { return \"a\" }\nfunc (A) kind() {}", false},
		{"a function that is no method", "func kind() {}", false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			if found := markertype.IsMarkerOnly(parseMethods(subtest, testCase.methods)); found != testCase.expected {
				subtest.Errorf("IsMarkerOnly of %q is %v, expected %v", testCase.methods, found, testCase.expected)
			}
		})
	}
}

// A type with no methods is not what IsMarkerOnly decides, and it says so by answering false.
func TestIsMarkerOnlyOfNoMethodsIsFalse(test *testing.T) {
	test.Parallel()
	if markertype.IsMarkerOnly(nil) {
		test.Error("IsMarkerOnly of no methods is true, expected false")
	}
}

// parseMethods parses source after a declaration of A and returns its function declarations in order.
func parseMethods(test *testing.T, source string) []*ast.FuncDecl {
	test.Helper()
	fileSource := "package fixture\n\ntype A struct{}\n\n" + source + "\n"
	file, parseError := parser.ParseFile(token.NewFileSet(), "fixture.go", fileSource, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing %q: %v", source, parseError)
	}
	methods := []*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
			methods = append(methods, function)
		}
	}
	if len(methods) == 0 {
		test.Fatalf("finding a function declaration in %q", source)
	}
	return methods
}
