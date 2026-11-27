package high_level_intermediate_representation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
)

// Lowering never fails, held over code written here.
//
// TestLowerRealCodebase lowered every function of Structure, a private repository, and checked the invariants on
// each; TestLowerRealCodebaseIsDeterministic lowered one of its files twice. A test may read only cohere's own
// repository, so both went with the corpus. What they held comes back in two parts: each construct real code once
// broke lowering on, as its own fixture asserting the right result where the history shows a wrong one, and a module
// written to mix those constructs with the rest of what application code holds, lowered whole and twice.

// mixedModulePath is the module lowered whole, a file written for this test rather than taken from any codebase.
const mixedModulePath = "testdata/lowering/Dashboard.tsx"

// readMixedModule reads the module lowered whole.
func readMixedModule(test *testing.T) string {
	test.Helper()
	contents, readError := os.ReadFile(mixedModulePath)
	if readError != nil {
		test.Fatalf("reading %s: %v", mixedModulePath, readError)
	}
	return string(contents)
}

// lowerEveryFunctionIn lowers each outermost function-like node of a TSX source, as the linter walks a file, and
// checks the invariants on each and on every function nested inside it.
//
// A panic is recovered and reported as a failure naming the source, since a panic in lowering is exactly what this
// guards against: the linter recovers per file, so in production it is a file nothing checked rather than a crash.
func lowerEveryFunctionIn(test *testing.T, sourceText string) []*Function {
	test.Helper()
	source := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/test.tsx",
		PathKey:  "/test.tsx",
	}, sourceText, core.ScriptKindTSX)

	var lowered []*Function
	forEachFunctionLike(source.AsNode(), func(node *ast.Node) {
		function := lowerRecovering(test, node)
		if function == nil {
			test.Errorf("lowering the function at %d returned nothing", node.Pos())
			return
		}
		lowered = append(lowered, function)
	})
	if len(lowered) == 0 {
		test.Fatal("the source holds no function, so there was nothing to lower")
	}

	var check func(function *Function)
	check = func(function *Function) {
		checkInvariants(test, function)
		for _, nested := range function.Functions {
			check(nested)
		}
	}
	for _, function := range lowered {
		check(function)
	}
	return lowered
}

// lowerRecovering lowers one node, turning a panic into a failure of the test that asked.
func lowerRecovering(test *testing.T, node *ast.Node) (function *Function) {
	test.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			test.Fatalf("lowering the function at %d panicked: %v", node.Pos(), recovered)
		}
	}()
	return Lower(node, nil)
}

// instructionValuesOf calls visit with every instruction value of the functions and the functions nested in them.
func instructionValuesOf(functions []*Function, visit func(function *Function, value InstructionValue)) {
	for _, function := range functions {
		for _, instruction := range function.Instructions {
			visit(function, instruction.Value)
		}
		instructionValuesOf(function.Functions, visit)
	}
}

// requireArrayHoleKept asserts the one array pattern or array literal in functions keeps its hole in the middle, with
// the bindings on either side of it named first and third.
func requireArrayHoleKept(test *testing.T, functions []*Function) {
	test.Helper()
	found := false
	instructionValuesOf(functions, func(function *Function, value InstructionValue) {
		destructure, isDestructure := value.(*Destructure)
		if !isDestructure {
			return
		}
		pattern, isArray := destructure.LValue.(*ArrayPattern)
		if !isArray {
			return
		}
		found = true
		if len(pattern.Elements) != 3 {
			test.Fatalf("the array pattern has %d positions, want 3; a dropped hole shifts every later binding left",
				len(pattern.Elements))
		}
		if pattern.Elements[1].Value != nil {
			test.Errorf("the middle position is bound, want a hole")
		}
		for index, want := range map[int]string{0: "first", 2: "third"} {
			place, isPlace := pattern.Elements[index].Value.(*PlacePattern)
			if !isPlace {
				test.Errorf("position %d binds %T, want the name %s", index, pattern.Elements[index].Value, want)
				continue
			}
			if got := function.Identifiers[place.Place.Identifier].Name; got != want {
				test.Errorf("position %d binds %q, want %q", index, got, want)
			}
		}
	})
	if !found {
		test.Fatal("no array pattern was lowered")
	}
}

// requireComputedObjectMember asserts the one object literal in functions holds a member whose key is computed: an
// ObjectMethod with no text key, entered in the object under a computed key rather than a text one.
func requireComputedObjectMember(test *testing.T, functions []*Function) {
	test.Helper()
	methods, computed := 0, 0
	instructionValuesOf(functions, func(_ *Function, value InstructionValue) {
		switch typed := value.(type) {
		case *ObjectMethod:
			methods++
			if typed.Key != "" {
				test.Errorf("the method's key reads %q, want none, since a computed name has no text", typed.Key)
			}
		case *ObjectExpression:
			for _, property := range typed.Properties {
				if property.ComputedKey != nil && property.Key == "" {
					computed++
				}
			}
		}
	})
	if methods != 1 || computed != 1 {
		test.Errorf("lowered %d object methods and %d computed keys, want 1 of each", methods, computed)
	}
}

// Each construct real code once broke lowering on, from the history of this package. Each lowers without panicking,
// to functions whose invariants hold, and where the defect was a wrong result rather than a crash, to the right one.
func TestLoweringTheConstructsThatOnceFailed(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string

		// fixedBy is the commit that fixed the construct, and how it failed before.
		fixedBy string

		sourceText string
		check      func(test *testing.T, functions []*Function)
	}{
		{
			name:       "a hole in an array binding pattern",
			fixedBy:    "13c196d44, the first corpus run: the hole was dropped and every later binding shifted left",
			sourceText: "function pick(list: number[]) { const [first, , third] = list; return first + third; }",
			check:      requireArrayHoleKept,
		},
		{
			// The same commit's comment names the assignment form as the other spelling, an OmittedExpression,
			// handled apart from the binding form.
			name:    "a hole in an array assignment pattern",
			fixedBy: "13c196d44, beside the binding form",
			sourceText: "function pick(list: number[]) { let first = 0; let third = 0; [first, , third] = list; " +
				"return first + third; }",
			check: requireArrayHoleKept,
		},
		{
			name:    "JSX text that is only whitespace across lines",
			fixedBy: "13c196d44, the first corpus run: Node.Text panicked on a JsxText node",
			sourceText: "function Card() {\n" +
				"    return (\n" +
				"        <div>\n" +
				"            <span />\n" +
				"            <b />\n" +
				"        </div>\n" +
				"    );\n" +
				"}\n",
			check: func(test *testing.T, functions []*Function) {
				// Whitespace holding a newline is not rendered, so only the two elements are children.
				requireJsxChildren(test, functions, "div", 2)
			},
		},
		{
			name:       "JSX text that is only whitespace on one line",
			fixedBy:    "13c196d44, which read whitespace by the parser's JSX rule rather than by trimming",
			sourceText: "function Pair(properties: { left: string; right: string }) { return <p>{properties.left} {properties.right}</p>; }",
			check: func(test *testing.T, functions []*Function) {
				// Whitespace on one line is rendered, so the space between the two values is a child of its own.
				requireJsxChildren(test, functions, "p", 3)
			},
		},
		{
			name:       "a class method with a computed name",
			fixedBy:    "5bc47c6bf, found running the binary over a repository: functionName read the name as text and panicked",
			sourceText: "class Secret { [Symbol.for('inspect')]() { return 'redacted'; } }",
			check: func(test *testing.T, functions []*Function) {
				if len(functions) != 1 || functions[0].Name != "" {
					test.Errorf("want one method lowered with no name, since a computed key is neither a " +
						"component nor a hook")
				}
			},
		},
		{
			name:       "an async generator object method with a computed name",
			fixedBy:    "e8df7694b, found on TanStack Query's tests: the object literal read the method's name as text and panicked",
			sourceText: "function useStream(value: number) { return { async *[Symbol.asyncIterator]() { yield value; } }; }",
			check:      requireComputedObjectMember,
		},
		{
			name:       "an object getter with a computed name",
			fixedBy:    "e8df7694b, which fixed a method's and an accessor's name together",
			sourceText: "function useLabel(key: symbol) { return { get [key]() { return 'label'; } }; }",
			check:      requireComputedObjectMember,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			// Deferred so it is said after a fatal failure too, which ends the test before any line below it.
			defer func() {
				if test.Failed() {
					test.Logf("fixed by %s", testCase.fixedBy)
				}
			}()
			testCase.check(test, lowerEveryFunctionIn(test, testCase.sourceText))
		})
	}
}

// requireJsxChildren asserts the one JSX element with the given host tag has the given number of children.
func requireJsxChildren(test *testing.T, functions []*Function, tag string, want int) {
	test.Helper()
	found := false
	instructionValuesOf(functions, func(_ *Function, value InstructionValue) {
		jsx, isJsx := value.(*JsxExpression)
		if !isJsx || jsx.Tag.Name != tag {
			return
		}
		found = true
		if len(jsx.Children) != want {
			test.Errorf("<%s> has %d children, want %d", tag, len(jsx.Children), want)
		}
	})
	if !found {
		test.Fatalf("no <%s> element was lowered", tag)
	}
}

// A module that mixes the constructs application code holds lowers whole: every function, nested ones included, with
// the invariants holding, and nothing left unsupported but what lower.go lists as unsupported.
//
// The counts are exact because the module is fixed. A walk that stopped finding functions, or a lowering that began
// giving up on a construct, moves one of them, where a floor would let either pass. The two `yield` expressions, in
// the class's generator and in the hook's async iterator, are the only unsupported values, and lower.go names `yield`
// among the constructs that reach UnsupportedNode rather than a faithful lowering.
func TestLoweringEveryFunctionOfAMixedModule(test *testing.T) {
	test.Parallel()
	functions := lowerEveryFunctionIn(test, readMixedModule(test))

	outermost, nested, blocks := len(functions), 0, 0
	unsupported := map[string]int{}
	var count func(function *Function)
	count = func(function *Function) {
		blocks += len(function.Blocks)
		for _, instruction := range function.Instructions {
			if node, isUnsupported := instruction.Value.(*UnsupportedNode); isUnsupported {
				unsupported[node.Reason]++
			}
		}
		for _, inner := range function.Functions {
			nested++
			count(inner)
		}
	}
	for _, function := range functions {
		count(function)
	}

	if outermost != 11 || nested != 15 || blocks != 124 {
		test.Errorf("lowered %d outermost functions, %d nested and %d blocks, want 11, 15 and 124", outermost, nested,
			blocks)
	}
	if len(unsupported) != 1 || unsupported["KindYieldExpression"] != 2 {
		test.Errorf("constructs left unsupported: %v, want the two yield expressions alone", unsupported)
	}
}

// Lowering the same module twice gives the same answer.
//
// Nondeterminism here would be invisible in every other test and fatal to a golden-file suite downstream. The usual
// source is map iteration order leaking into block or identifier numbering, which this package avoids by allocating
// ids from counters rather than from map walks. Each pass parses afresh, so no tree is shared between the two.
func TestLoweringAMixedModuleIsDeterministic(test *testing.T) {
	test.Parallel()
	sourceText := readMixedModule(test)

	printAll := func() string {
		var printed []byte
		for _, function := range lowerEveryFunctionIn(test, sourceText) {
			printed = append(printed, Print(function)...)
		}
		return string(printed)
	}
	first, second := printAll(), printAll()
	if first == "" {
		test.Fatal("the module lowered to nothing, so there is nothing to compare")
	}
	if first != second {
		test.Errorf("lowering %s twice printed different output; something depends on map order",
			filepath.Base(mixedModulePath))
	}
}
