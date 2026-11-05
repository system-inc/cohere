package high_level_intermediate_representation

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/system-inc/cohere/static_single_assignment"
)

// lowerSource parses one function out of source and lowers it.
//
// The first function-like node wins, so a test writes the function under test first and any helper
// after it.
func lowerSource(test *testing.T, code string) *Function {
	test.Helper()
	source := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/test.tsx",
		PathKey:  "/test.tsx",
	}, code, core.ScriptKindTSX)

	var root *ast.Node
	source.AsNode().ForEachChild(func(node *ast.Node) bool {
		if root != nil {
			return true
		}
		if ast.IsFunctionLike(node) {
			root = node
			return true
		}
		// A function assigned to a variable is nested inside the statement.
		node.ForEachChild(func(child *ast.Node) bool {
			if root != nil {
				return true
			}
			child.ForEachChild(func(grandchild *ast.Node) bool {
				if root == nil && ast.IsFunctionLike(grandchild) {
					root = grandchild
					return true
				}
				return false
			})
			return false
		})
		return false
	})
	if root == nil {
		test.Fatal("test source has no function")
	}

	function := Lower(root, nil)
	if function == nil {
		test.Fatal("lowering produced nothing")
	}
	return function
}

// checkInvariants asserts the three properties Finalize establishes, which every pass assumes.
//
// Called from every lowering test rather than tested once, because the invariants are what break
// when a construct's lowering is wrong, and a construct-specific test that only reads the printed
// output will not notice a predecessor edge that is missing.
func checkInvariants(test *testing.T, function *Function) {
	test.Helper()

	if len(function.Blocks) == 0 {
		test.Fatal("a lowered function has no blocks")
	}
	if function.Blocks[0].Id != function.Entry {
		test.Errorf("Blocks[0] is bb%d but the entry is bb%d; reverse postorder must put the entry first",
			function.Blocks[0].Id, function.Entry)
	}

	// Every terminal is set: a block left open is a lowering path that forgot to terminate.
	for _, block := range function.Blocks {
		if block.Terminal == nil {
			test.Errorf("bb%d has no terminal", block.Id)
		}
	}

	// Every successor names a block that exists. A dangling id means a reserved block was never
	// filled, or was dropped as unreachable while something still pointed at it.
	for _, block := range function.Blocks {
		if block.Terminal == nil {
			continue
		}
		EachSuccessor(block.Terminal, func(id static_single_assignment.BlockId) {
			if _, ok := function.Block(id); !ok {
				test.Errorf("bb%d names successor bb%d, which does not exist", block.Id, id)
			}
		})
	}

	// Predecessors agree with successors in both directions. A one-way disagreement is exactly what
	// makes single-assignment construction mint a wrong phi.
	forward := map[static_single_assignment.BlockId]map[static_single_assignment.BlockId]bool{}
	for _, block := range function.Blocks {
		if block.Terminal == nil {
			continue
		}
		EachSuccessor(block.Terminal, func(id static_single_assignment.BlockId) {
			if forward[id] == nil {
				forward[id] = map[static_single_assignment.BlockId]bool{}
			}
			forward[id][block.Id] = true
		})
	}
	for _, block := range function.Blocks {
		recorded := map[static_single_assignment.BlockId]bool{}
		for _, predecessor := range block.Predecessors {
			recorded[predecessor] = true
			if _, ok := function.Block(predecessor); !ok {
				test.Errorf("bb%d lists predecessor bb%d, which does not exist", block.Id, predecessor)
			}
		}
		for predecessor := range forward[block.Id] {
			if !recorded[predecessor] {
				test.Errorf("bb%d has an edge from bb%d that Predecessors does not record", block.Id, predecessor)
			}
		}
		for predecessor := range recorded {
			if !forward[block.Id][predecessor] {
				test.Errorf("bb%d lists predecessor bb%d, which has no edge to it", block.Id, predecessor)
			}
		}
	}

	// Evaluation order is assigned and strictly increasing across the block order.
	previous := static_single_assignment.EvaluationOrder(0)
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			order := function.Instructions[instructionId].Order
			if order == 0 {
				test.Errorf("an instruction in bb%d has no evaluation order", block.Id)
			}
			if order <= previous {
				test.Errorf("evaluation order went backwards in bb%d: %d after %d", block.Id, order, previous)
			}
			previous = order
		}
		if block.Terminal != nil {
			order := TerminalOrder(block.Terminal)
			if order == 0 {
				test.Errorf("the terminal of bb%d has no evaluation order", block.Id)
			}
			previous = order
		}
	}

	// Every place names an identifier that exists.
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			EachInstructionPlace(instruction, func(place Place, _ PlaceRole) {
				if int(place.Identifier) >= len(function.Identifiers) {
					test.Errorf("a place in bb%d names identifier %d, out of %d",
						block.Id, place.Identifier, len(function.Identifiers))
				}
			})
		}
	}
}

// requireInstruction fails unless the printed output holds the given text.
func requireInstruction(test *testing.T, function *Function, want string) {
	test.Helper()
	printed := Print(function)
	if !strings.Contains(printed, want) {
		test.Errorf("expected to find %q in the lowered form:\n%s", want, printed)
	}
}

// TestLowerTryFinally is the first of the four shapes the brief named as where lowering designs
// break.
//
// The property asserted is the divergence this package chose: the finally body is lowered once.
// cohere's `controlflow` lays it out twice and both copies carry the same source positions; see the
// Try terminal's comment for why an IR cannot do that.
func TestLowerTryFinally(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(a: number) {
			let x = 0;
			try {
				x = compute(a);
				return x;
			} catch (error) {
				x = -1;
			} finally {
				cleanup(x);
			}
			return x;
		}
	`)
	checkInvariants(test, function)

	// The finally body appears once. Counting the call is the direct test of single layout: two
	// layouts would produce two `cleanup` loads.
	printed := Print(function)
	if count := strings.Count(printed, "LoadGlobal cleanup"); count != 1 {
		test.Errorf("the finally body was laid out %d times, want exactly 1:\n%s", count, printed)
	}

	// The handler has a real edge even though the try body's throwing is not proven.
	foundTry := false
	for _, block := range function.Blocks {
		if try, ok := block.Terminal.(*Try); ok {
			foundTry = true
			if !HasBlock(try.Handler) {
				test.Error("the try has no handler block")
			}
			handler, ok := function.Block(try.Handler)
			if !ok {
				test.Fatal("the handler block does not exist")
			}
			if len(handler.Predecessors) == 0 {
				test.Error("the handler has no predecessor, so nothing can reach the catch")
			}
			if try.HandlerBinding == nil {
				test.Error("the catch parameter was not bound")
			}
		}
	}
	if !foundTry {
		test.Error("no Try terminal was produced")
	}
}

// TestLowerTryFinallyWithoutCatch pins the bare try/finally case.
//
// `controlflow` gives a body that cannot throw no edge to its catch. This always gives one, because
// "cannot throw" is a judgement a later pass may revise and a graph whose shape depends on it must
// be rebuilt when it changes.
func TestLowerTryFinallyWithoutCatch(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f() {
			try {
				risky();
			} finally {
				cleanup();
			}
		}
	`)
	checkInvariants(test, function)

	printed := Print(function)
	if count := strings.Count(printed, "LoadGlobal cleanup"); count != 1 {
		test.Errorf("the finally body was laid out %d times, want exactly 1:\n%s", count, printed)
	}
	requireInstruction(test, function, "Try block=")
}

// TestLowerLoop covers the second named shape.
func TestLowerLoop(t *testing.T) {
	t.Parallel()

	function := lowerSource(t, `
		function f(items: number[]) {
			let total = 0;
			for (let index = 0; index < items.length; index++) {
				if (items[index] < 0) {
					continue;
				}
				total += items[index];
			}
			return total;
		}
	`)
	checkInvariants(t, function)
	requireInstruction(t, function, "For init=")

	// A loop must have a back edge: the block holding the test is reachable from inside the body.
	// Without one, every downstream analysis treats the loop as straight-line code.
	found := false
	for _, block := range function.Blocks {
		forStatement, ok := block.Terminal.(*For)
		if !ok {
			continue
		}
		found = true
		test, ok := function.Block(forStatement.Test)
		if !ok {
			t.Fatal("the for test block does not exist")
		}
		if len(test.Predecessors) < 2 {
			t.Errorf("the for test has %d predecessors, want at least 2 (the init and the back edge)",
				len(test.Predecessors))
		}
	}
	if !found {
		t.Error("no For terminal was produced")
	}
}

// TestLowerForOfProducesIteratorProtocol pins that iteration is instructions rather than terminal
// payload, which is what lets an effect pass see that iterating mutates the iterator.
func TestLowerForOfProducesIteratorProtocol(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(items: string[]) {
			for (const item of items) {
				use(item);
			}
		}
	`)
	checkInvariants(test, function)
	requireInstruction(test, function, "GetIterator")
	requireInstruction(test, function, "IteratorNext")
	requireInstruction(test, function, "ForOf init=")
}

func TestLowerWhileAndDoWhile(test *testing.T) {
	test.Parallel()

	whileFunction := lowerSource(test, `
		function f(n: number) {
			while (n > 0) {
				n = n - 1;
			}
			return n;
		}
	`)
	checkInvariants(test, whileFunction)
	requireInstruction(test, whileFunction, "While test=")

	doFunction := lowerSource(test, `
		function g(n: number) {
			do {
				n = n - 1;
			} while (n > 0);
			return n;
		}
	`)
	checkInvariants(test, doFunction)
	requireInstruction(test, doFunction, "DoWhile loop=")
}

// TestLowerSwitch covers the third named shape.
//
// The property that matters is fallthrough between cases: a case running off its end must reach the
// next case block, not the statement's fallthrough. Getting that backwards is the classic switch
// lowering bug and it produces a graph that looks right.
func TestLowerSwitch(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(kind: string) {
			let result = 0;
			switch (kind) {
				case "a":
					result = 1;
					break;
				case "b":
					result = 2;
				case "c":
					result = 3;
					break;
				default:
					result = -1;
			}
			return result;
		}
	`)
	checkInvariants(test, function)

	var switchTerminal *Switch
	for _, block := range function.Blocks {
		if candidate, ok := block.Terminal.(*Switch); ok {
			switchTerminal = candidate
		}
	}
	if switchTerminal == nil {
		test.Fatal("no Switch terminal was produced")
	}
	if len(switchTerminal.Cases) != 4 {
		test.Errorf("the switch has %d cases, want 4", len(switchTerminal.Cases))
	}

	defaults := 0
	for _, kase := range switchTerminal.Cases {
		if kase.Test == nil {
			defaults++
		}
	}
	if defaults != 1 {
		test.Errorf("the switch has %d default cases, want 1", defaults)
	}

	// The `case "b"` body falls into `case "c"`. Its block must lead to the next case block.
	caseB, ok := function.Block(switchTerminal.Cases[1].Block)
	if !ok {
		test.Fatal("the second case block does not exist")
	}
	caseCId := switchTerminal.Cases[2].Block
	reaches := false
	EachSuccessor(caseB.Terminal, func(id static_single_assignment.BlockId) {
		if id == caseCId {
			reaches = true
		}
	})
	if !reaches {
		test.Errorf("case \"b\" does not fall through to case \"c\"; fallthrough between cases is lost:\n%s",
			Print(function))
	}
}

// TestLowerSwitchWithoutDefaultCanSkipEveryCase pins the one place a fallthrough is a real edge.
//
// A switch with no default can fall past every case, so `EachSuccessor` must yield the fallthrough
// for it and must not for a switch that has one. Treating it uniformly either way is wrong.
func TestLowerSwitchWithoutDefaultCanSkipEveryCase(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(kind: string) {
			switch (kind) {
				case "a":
					use(1);
					break;
			}
			return 0;
		}
	`)
	checkInvariants(test, function)

	for _, block := range function.Blocks {
		switchTerminal, ok := block.Terminal.(*Switch)
		if !ok {
			continue
		}
		reachesFallthrough := false
		EachSuccessor(switchTerminal, func(id static_single_assignment.BlockId) {
			if id == switchTerminal.Fallthrough {
				reachesFallthrough = true
			}
		})
		if !reachesFallthrough {
			test.Error("a switch with no default must have an edge to its fallthrough: every case can be skipped")
		}
	}
}

// TestLowerDestructuring covers the fourth named shape.
//
// The property asserted is that the pattern stays structured rather than being expanded into
// property loads. See pattern.go: the flattening is recoverable from the structure and the
// structure is not recoverable from the flattening.
func TestLowerDestructuring(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(input: {a: number; b: {c: number}; rest: number}) {
			const {a, b: {c}, ...others} = input;
			const [first, , third = 3, ...tail] = getList();
			return a + c + first + third;
		}
	`)
	checkInvariants(test, function)

	printed := Print(function)
	if !strings.Contains(printed, "Destructure") {
		test.Errorf("no Destructure instruction was produced:\n%s", printed)
	}

	var objectPattern *ObjectPattern
	var arrayPattern *ArrayPattern
	for _, instruction := range function.Instructions {
		destructure, ok := instruction.Value.(*Destructure)
		if !ok {
			continue
		}
		switch pattern := destructure.LValue.(type) {
		case *ObjectPattern:
			objectPattern = pattern
		case *ArrayPattern:
			arrayPattern = pattern
		}
	}

	if objectPattern == nil {
		test.Fatalf("the object destructure did not produce an ObjectPattern:\n%s", printed)
	}
	if objectPattern.Rest == nil {
		test.Error("the object rest element was not bound")
	}
	if len(objectPattern.Properties) != 2 {
		test.Errorf("the object pattern has %d properties, want 2", len(objectPattern.Properties))
	}
	// The nested `b: {c}` must stay nested rather than being flattened.
	nested := false
	for _, property := range objectPattern.Properties {
		if _, ok := property.Value.(*ObjectPattern); ok {
			nested = true
		}
	}
	if !nested {
		test.Error("the nested object pattern was flattened; the shape a memoization rule reads is lost")
	}

	if arrayPattern == nil {
		test.Fatalf("the array destructure did not produce an ArrayPattern:\n%s", printed)
	}
	if arrayPattern.Rest == nil {
		test.Error("the array rest element was not bound")
	}
	// The hole in `[first, , third]` must be preserved: position is meaning in an array pattern.
	holes := 0
	for _, element := range arrayPattern.Elements {
		if element.Value == nil {
			holes++
		}
	}
	if holes != 1 {
		test.Errorf("the array pattern has %d holes, want 1; position is meaning here", holes)
	}
}

// TestLowerLogicalIsControlFlow pins that short-circuiting operators are terminals.
//
// `a && b` does not evaluate `b` on every path. An IR that lowered it to an instruction would tell
// every downstream analysis that it does, which is wrong in the direction that produces false
// positives about what a function reads.
func TestLowerLogicalIsControlFlow(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(a: unknown, b: unknown) {
			return a && expensive(b);
		}
	`)
	checkInvariants(test, function)

	found := false
	for _, block := range function.Blocks {
		if logical, ok := block.Terminal.(*Logical); ok {
			found = true
			if logical.Operator != "&&" {
				test.Errorf("the logical operator is %q, want &&", logical.Operator)
			}
		}
	}
	if !found {
		test.Errorf("&& did not produce a Logical terminal, so short-circuiting is invisible:\n%s", Print(function))
	}

	// The call must not be in the same block as the test: if it were, it would look unconditional.
	var testBlock static_single_assignment.BlockId
	var callBlock static_single_assignment.BlockId
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			if global, ok := function.Instructions[instructionId].Value.(*LoadGlobal); ok {
				if global.Name == "expensive" {
					callBlock = block.Id
				}
			}
		}
		if _, ok := block.Terminal.(*Logical); ok {
			testBlock = block.Id
		}
	}
	if callBlock == testBlock && callBlock != InvalidBlock {
		test.Error("the short-circuited operand shares a block with the test, so it looks unconditional")
	}
}

func TestLowerTernary(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(flag: boolean) {
			return flag ? left() : right();
		}
	`)
	checkInvariants(test, function)
	requireInstruction(test, function, "Ternary test=")
}

// TestLowerMethodCallKeepsReceiver pins the distinction an effect pass depends on.
func TestLowerMethodCallKeepsReceiver(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(list: number[]) {
			list.push(1);
			return list;
		}
	`)
	checkInvariants(test, function)
	requireInstruction(test, function, "MethodCall")

	for _, instruction := range function.Instructions {
		call, ok := instruction.Value.(*MethodCall)
		if !ok {
			continue
		}
		// The receiver must be visited with the receiver role, which is what lets a mutation be
		// attributed to `list` rather than to the loaded function.
		sawReceiver := false
		EachPlace(call, func(place Place, role PlaceRole) {
			if role == PlaceRoleReceiver && place.Identifier == call.Receiver.Identifier {
				sawReceiver = true
			}
		})
		if !sawReceiver {
			test.Error("the method call receiver is not visited with the receiver role")
		}
	}
}

// TestLowerGlobalStoreIsOneInstruction records that the `globals` rule's whole detection is this
// instruction existing.
func TestLowerGlobalStoreIsOneInstruction(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f() {
			someGlobal = 1;
		}
	`)
	checkInvariants(test, function)
	requireInstruction(test, function, "StoreGlobal someGlobal")
}

// TestLowerJsx proves the JSX variants carry what a React rule needs.
func TestLowerJsx(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function Component(props: {title: string}) {
			return <div className="a" {...props}>hello {props.title}</div>;
		}
	`)
	checkInvariants(test, function)

	if function.Kind != FunctionKindComponent {
		test.Errorf("Component was classified as %s, want component", function.Kind)
	}

	var jsx *JsxExpression
	for _, instruction := range function.Instructions {
		if candidate, ok := instruction.Value.(*JsxExpression); ok {
			jsx = candidate
		}
	}
	if jsx == nil {
		test.Fatalf("no JsxExpression was produced:\n%s", Print(function))
	}
	if jsx.Tag.Name != "div" {
		test.Errorf("the host element tag is %q, want div", jsx.Tag.Name)
	}
	if len(jsx.Properties) != 2 {
		test.Errorf("the element has %d props, want 2 (one named, one spread)", len(jsx.Properties))
	}
	spreads := 0
	for _, property := range jsx.Properties {
		if property.Spread {
			spreads++
		}
	}
	if spreads != 1 {
		test.Errorf("the element has %d spread props, want 1", spreads)
	}
	// Whitespace-only children are dropped; `hello ` and the interpolation remain.
	if len(jsx.Children) != 2 {
		test.Errorf("the element has %d children, want 2:\n%s", len(jsx.Children), Print(function))
	}
}

// TestLowerJsxComponentTagIsAValueReference pins that `<Foo />` is a use of Foo.
//
// A rule asking what a component depends on reads uses. If the tag stayed a string, every component
// reference would be invisible to that question.
func TestLowerJsxComponentTagIsAValueReference(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function Outer() {
			return <Inner value={1} />;
		}
	`)
	checkInvariants(test, function)

	for _, instruction := range function.Instructions {
		jsx, ok := instruction.Value.(*JsxExpression)
		if !ok {
			continue
		}
		if jsx.Tag.Place == nil {
			test.Errorf("the capitalized tag is a string, not a value reference; the use of Inner is invisible:\n%s",
				Print(function))
		}
	}
}

// TestLowerLabeledBreakAndContinue covers labeled jumps, which are where a jump-target stack is
// usually wrong.
func TestLowerLabeledBreakAndContinue(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(rows: number[][]) {
			outer: for (const row of rows) {
				for (const cell of row) {
					if (cell < 0) {
						continue outer;
					}
					if (cell > 100) {
						break outer;
					}
					use(cell);
				}
			}
		}
	`)
	checkInvariants(test, function)

	// Neither jump may become Unsupported: that is what lookupBreak and lookupContinue produce when
	// they fail to find the label, and it is silent.
	for _, block := range function.Blocks {
		if _, ok := block.Terminal.(*Unsupported); ok {
			test.Errorf("a labeled jump did not resolve and became Unsupported:\n%s", Print(function))
		}
	}
}

// TestLowerLabeledBlockBreak pins the non-loop label case, which takes a different path.
func TestLowerLabeledBlockBreak(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(flag: boolean) {
			block: {
				if (flag) {
					break block;
				}
				use(1);
			}
			return 2;
		}
	`)
	checkInvariants(test, function)
	requireInstruction(test, function, "Label block=")
}

// TestLowerNestedFunction pins that nested functions live in the arena, not inline.
func TestLowerNestedFunction(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function outer(items: number[]) {
			const doubled = items.map(item => item * 2);
			return doubled;
		}
	`)
	checkInvariants(test, function)

	if len(function.Functions) != 1 {
		test.Fatalf("the outer function holds %d nested functions, want 1", len(function.Functions))
	}
	nested := function.Functions[0]
	checkInvariants(test, nested)

	found := false
	for _, instruction := range function.Instructions {
		if expression, ok := instruction.Value.(*FunctionExpression); ok {
			found = true
			if int(expression.Function) >= len(function.Functions) {
				test.Errorf("the function expression names fn%d, out of %d",
					expression.Function, len(function.Functions))
			}
		}
	}
	if !found {
		test.Error("no FunctionExpression was produced for the arrow function")
	}
}

// TestLowerShorthandPropertyReadsLocalBinding pins the checker-specific symbol lookup for `{temp}`.
// TypeScript's generic symbol accessor returns the property symbol at the shorthand name; lowering
// needs the shorthand's value symbol so the read stays connected to the local declaration.
func TestLowerShorthandPropertyReadsLocalBinding(test *testing.T) {
	test.Parallel()

	function := lowerTypedFunctions(test, "shorthand.tsx", `
		export function makeObject() {
			const temp = compute();
			return {temp};
		}
	`)[0]
	checkInvariants(test, function)

	var local static_single_assignment.IdentifierId
	localFound := false
	var shorthandValue static_single_assignment.IdentifierId
	shorthandFound := false
	for _, instruction := range function.Instructions {
		switch value := instruction.Value.(type) {
		case *StoreLocal:
			if function.Identifiers[value.LValue.Identifier].Name == "temp" {
				local = value.LValue.Identifier
				localFound = true
			}
		case *ObjectExpression:
			for _, property := range value.Properties {
				if property.Key == "temp" {
					shorthandValue = property.Value.Identifier
					shorthandFound = true
				}
			}
		}
	}
	if !localFound || !shorthandFound {
		test.Fatalf("lowering did not produce the local and shorthand property:\n%s", Print(function))
	}

	for _, instruction := range function.Instructions {
		if instruction.LValue.Identifier != shorthandValue {
			continue
		}
		load, ok := instruction.Value.(*LoadLocal)
		if !ok {
			test.Fatalf("shorthand value lowered as %T, want *LoadLocal:\n%s", instruction.Value, Print(function))
		}
		if load.Place.Identifier != local {
			test.Fatalf("shorthand reads %s, want local %s:\n%s",
				function.PlaceString(load.Place), function.PlaceString(Place{Identifier: local}), Print(function))
		}
		return
	}
	test.Fatalf("shorthand value %d has no defining instruction:\n%s", shorthandValue, Print(function))
}

// TestLowerUnreachableCodeIsDropped pins that this IR removes what controlflow keeps.
//
// See ReversePostorder: a value defined only where control never arrives is a value no analysis
// should see.
func TestLowerUnreachableCodeIsDropped(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f() {
			return 1;
			const dead = expensive();
			return dead;
		}
	`)
	checkInvariants(test, function)

	printed := Print(function)
	if strings.Contains(printed, "LoadGlobal expensive") {
		test.Errorf("unreachable code survived reverse postorder:\n%s", printed)
	}
}

// TestLowerAsyncAndGeneratorModifiers pins the flags validators read.
func TestLowerAsyncAndGeneratorModifiers(test *testing.T) {
	test.Parallel()

	asyncFunction := lowerSource(test, `
		async function f() {
			return await load();
		}
	`)
	if !asyncFunction.IsAsync {
		test.Error("an async function was not marked async")
	}
	requireInstruction(test, asyncFunction, "Await")

	generator := lowerSource(test, `
		function* g() {
			return 1;
		}
	`)
	if !generator.IsGenerator {
		test.Error("a generator was not marked as one")
	}
}

// TestLowerOptionalChaining pins that optionality is represented as control flow and recovered as
// one access path. The guarded PropertyLoad itself is ordinary; the Optional terminal is the guard.
func TestLowerOptionalChaining(test *testing.T) {
	test.Parallel()

	function := lowerTypedFunctions(test, "optional.ts", `
		function f(input: {a?: {b: number}}) {
			return input.a?.b;
		}
	`)[0]
	checkInvariants(test, function)

	foundTerminal := false
	foundLoad := false
	for _, block := range function.Blocks {
		if optional, ok := block.Terminal.(*Optional); ok && optional.Optional {
			foundTerminal = true
		}
	}
	for _, instruction := range function.Instructions {
		if load, ok := instruction.Value.(*PropertyLoad); ok && load.Property == "b" {
			foundLoad = true
			if load.Optional {
				test.Error("the guarded load carries optionality as an instruction flag, want structural control flow")
			}
		}
	}
	if !foundTerminal || !foundLoad {
		test.Fatalf("optional access lowered without its terminal or guarded load:\n%s", Print(function))
	}

	want := []DependencyPathEntry{{Property: "a"}, {Property: "b", Optional: true}}
	for _, dependency := range CollectOptionalChainSidemap(function).TemporariesReadInOptional {
		if equalPaths(dependency.Path, want) {
			return
		}
	}
	test.Errorf("optional sidemap did not reconstruct input.a?.b:\n%s", Print(function))
}

// TestLowerUnsupportedSyntaxDoesNotFail pins the escape hatch.
//
// Lowering must never refuse a function, because a rule that cannot lower cannot report. An
// unmodelled construct becomes an opaque value with unknown effects, which is the conservative
// answer.
func TestLowerUnsupportedSyntaxDoesNotFail(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f() {
			class Inner {}
			return new Inner();
		}
	`)
	checkInvariants(test, function)

	found := false
	for _, instruction := range function.Instructions {
		if _, ok := instruction.Value.(*UnsupportedNode); ok {
			found = true
		}
	}
	if !found {
		test.Errorf("the class declaration did not become an UnsupportedNode:\n%s", Print(function))
	}
}

// TestLowerReturnsIsOneIdentifier pins that every return stores into one value.
func TestLowerReturnsIsOneIdentifier(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(flag: boolean) {
			if (flag) {
				return 1;
			}
			return 2;
		}
	`)
	checkInvariants(test, function)

	returns := 0
	for _, block := range function.Blocks {
		terminal, ok := block.Terminal.(*Return)
		if !ok {
			continue
		}
		returns++
		if terminal.Value.Identifier != function.Returns.Identifier {
			test.Error("a return stores into a value other than Function.Returns")
		}
	}
	if returns < 2 {
		test.Errorf("found %d Return terminals, want at least 2", returns)
	}
}

// TestFallthroughIsNotAnEdge is the property most likely to be got wrong by a later pass.
//
// If a fallthrough were an edge, an `if` would propagate values from the test directly into the
// join, bypassing both arms. The error is silent because the join is genuinely reachable.
func TestFallthroughIsNotAnEdge(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(flag: boolean) {
			let value = 0;
			if (flag) {
				value = 1;
			} else {
				value = 2;
			}
			return value;
		}
	`)
	checkInvariants(test, function)

	for _, block := range function.Blocks {
		ifTerminal, ok := block.Terminal.(*If)
		if !ok {
			continue
		}
		EachSuccessor(ifTerminal, func(id static_single_assignment.BlockId) {
			if id == ifTerminal.Fallthrough {
				test.Error("EachSuccessor yielded the fallthrough of an if with both arms; it is not an edge")
			}
		})

		// The join's predecessors are the two arms, never the test block.
		join, ok := function.Block(ifTerminal.Fallthrough)
		if !ok {
			test.Fatal("the if fallthrough block does not exist")
		}
		for _, predecessor := range join.Predecessors {
			if predecessor == block.Id {
				test.Error(
					"the block holding the If is a predecessor of the join; the fallthrough leaked in as an edge",
				)
			}
		}
	}
}

// TestEachSuccessorAndFallthroughReachesTheJoin is the complement: a structural walk must reach it.
func TestEachSuccessorAndFallthroughReachesTheJoin(test *testing.T) {
	test.Parallel()

	function := lowerSource(test, `
		function f(flag: boolean) {
			if (flag) {
				use(1);
			}
			return 2;
		}
	`)
	checkInvariants(test, function)

	for _, block := range function.Blocks {
		ifTerminal, ok := block.Terminal.(*If)
		if !ok {
			continue
		}
		reached := false
		EachSuccessorAndFallthrough(ifTerminal, func(id static_single_assignment.BlockId) {
			if id == ifTerminal.Fallthrough {
				reached = true
			}
		})
		if !reached {
			test.Error("EachSuccessorAndFallthrough did not yield the fallthrough")
		}
	}
}
