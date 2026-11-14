package typescript

import (
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// The class the walk hands the rule: a computed member, which the core is handed the list without, and a real
// duplicate, so the core reports while its listener runs, the moment a checker on another goroutine could read the
// class too. Its `*[Symbol.iterator]()` is the member loom's cursor lost (#72kn826).
const computedMemberClass = "class Cursor {\n" +
	"  *[Symbol.iterator](): IterableIterator<number> {}\n" +
	"  read() {}\n" +
	"  read() {}\n" +
	"}\n"

// parsedClass parses computedMemberClass and returns its class node and the member list the parser gave it.
func parsedClass(test *testing.T) (*ast.Node, *ast.NodeList) {
	test.Helper()
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: tspath.RootedFilePath("/repository/source/Cursor.ts"),
		PathKey:  tspath.PathKey("/repository/source/Cursor.ts"),
	}, computedMemberClass, core.ScriptKindTS)
	if sourceFile == nil || len(sourceFile.Statements.Nodes) != 1 ||
		sourceFile.Statements.Nodes[0].Kind != ast.KindClassDeclaration {
		test.Fatal("the fixture did not parse to one class")
	}
	class := sourceFile.Statements.Nodes[0]
	return class, class.AsClassDeclaration().Members
}

// The syntax tree is shared with every checker, so the rule reads the class and never writes it: while its listener
// runs, the class's member list is still the one the parser gave it, computed members and all. The rule once swapped
// the list for the duration of the call, and a checker resolving the class on another goroutine then cached loom's
// cursor without its iterator for the whole run (#72kn826).
func TestNoDupeClassMembersLeavesTheClassItReadsUntouched(test *testing.T) {
	test.Parallel()
	class, parsed := parsedClass(test)
	if len(parsed.Nodes) != 3 {
		test.Fatalf("the class has %d members, want 3, so this proves nothing", len(parsed.Nodes))
	}
	reports := 0
	context := rule.Context{
		SourceFile: ast.GetSourceFileOfNode(class),
		Report: func(rule.Diagnostic) {
			reports++
			if seen := class.AsClassDeclaration().Members; seen != parsed || len(seen.Nodes) != 3 {
				test.Errorf("while the rule ran, the class's members were a list of %d, not the parser's own",
					len(seen.Nodes))
			}
		},
	}
	listeners := NoDupeClassMembers.Run(context, rule.Given[rule.NoOptions]{})
	listeners[ast.KindClassDeclaration](class)
	if reports != 1 {
		test.Fatalf("the rule reported %d times, want the one duplicate, so the check above ran against nothing",
			reports)
	}
	if class.AsClassDeclaration().Members != parsed {
		test.Error("after the rule ran, the class's members were not the parser's own list")
	}
}

// The race the land gate runs under -race: the rule's listener on one goroutine and a checker's read of the same
// class on another, as a type check beside the walk does it (Node.Members, from getMembersOfDeclaration). A rule
// that writes the class is a data race here, red under -race on its first run (#72kn826).
func TestNoDupeClassMembersAndACheckerReadTheClassAtOnce(test *testing.T) {
	test.Parallel()
	class, _ := parsedClass(test)
	context := rule.Context{
		SourceFile: ast.GetSourceFileOfNode(class),
		Report: func(rule.Diagnostic) {
			// The race is in the reading, not the reports, so this test keeps none; the test above counts them.
		},
	}
	listener := NoDupeClassMembers.Run(context, rule.Given[rule.NoOptions]{})[ast.KindClassDeclaration]

	const rounds = 200
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		for range rounds {
			listener(class)
		}
	}()
	members := 0
	go func() {
		defer waitGroup.Done()
		for range rounds {
			members = len(class.Members())
		}
	}()
	waitGroup.Wait()
	if members != 3 {
		test.Fatalf("the reader saw %d members at the end, want 3", members)
	}
}
