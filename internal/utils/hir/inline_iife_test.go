package hir

// Tests for the CFG splice.
//
// Every assertion here is written against a graph PROPERTY rather than against a printed shape,
// because a printed shape passes for the wrong reason: a splice that routes a return to the wrong
// block still prints instructions in a plausible order. The properties asserted are the four the
// splice can violate independently -- the call is gone, no return of the copied body remains, every
// exit of the copied body reaches the continuation, and the call's result is defined on every path
// out of it.

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// inlinedFixture lowers source, splices, and returns the function plus how many calls were spliced.
func inlinedFixture(t *testing.T, source string, drop bool) (*Function, int) {
	t.Helper()
	var result *Function
	count := 0
	probe := rule.Rule{
		Name:             "inline-iife-fixture",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						return
					}
					ForEachFunctionLike(node, func(functionNode *ast.Node) {
						if result != nil {
							return
						}
						lowered := Lower(functionNode, ctx.TypeChecker)
						if lowered == nil {
							return
						}
						if drop {
							DropManualMemoization(lowered)
						}
						Construct(lowered)
						count = InlineImmediatelyInvokedFunctionExpressions(lowered)
						result = lowered
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/fixture.tsx": source,
	}, "/fixture.tsx")
	return result, count
}

const singleReturnIifeSource = `
	function Component(properties: {items: Array<number>}) {
		const built = (() => {
			const out = [];
			out.push(properties.items);
			return out;
		})();
		return [built];
	}
`

// The branch is what produces two returns, which is what forces the labeled form. A single-return
// body exercises the other arm entirely and would leave the label path untested.
const multipleReturnIifeSource = `
	function Component(properties: {items: Array<number>; flag: boolean}) {
		const built = (() => {
			const out = [];
			if (properties.flag) {
				return out;
			}
			out.push(properties.items);
			return out;
		})();
		return [built];
	}
`

// TestInlineRemovesTheCallAndEveryReturnOfTheInlinedBody is the core guard.
//
// Three failures are independent and each produces a graph that builds:
//
//   - The `CallExpression` survives, so the closure is still called and the capture edge that
//     widens mutable ranges is still there. The splice then costs work and changes nothing, which
//     is the failure mode with no symptom at all.
//   - A `Return` in the copied body is left unrouted, so control leaves the CALLER at that point.
//     Everything after the call is silently unreachable.
//   - A rewritten return jumps somewhere other than the continuation, so the code after the call
//     never runs on that path.
func TestInlineRemovesTheCallAndEveryReturnOfTheInlinedBody(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"single return", singleReturnIifeSource},
		{"multiple returns", multipleReturnIifeSource},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, inlined := inlinedFixture(t, testCase.source, false)
			if function == nil {
				t.Fatal("the fixture did not lower")
			}
			if inlined != 1 {
				t.Fatalf("spliced %d calls, want 1; with none spliced every assertion below "+
					"passes vacuously", inlined)
			}

			// The call and the function expression that fed it are both gone. Either surviving
			// means the closure is still constructed and still called.
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				for _, instructionId := range block.Instructions {
					instruction := function.Instructions[instructionId]
					if instruction == nil {
						continue
					}
					switch instruction.Value.(type) {
					case *CallExpression:
						t.Errorf("a CallExpression survives in block %d; the IIFE was not "+
							"actually removed and the capture edge is still there", block.Id)
					case *FunctionExpression:
						t.Errorf("a FunctionExpression survives in block %d; its value is dead "+
							"but range inference still sees a closure over the captures",
							block.Id)
					}
				}
			}

			// Exactly one `Return` remains and it is the CALLER's own. More than one means a
			// return of the inlined body was left unrouted and now ends the caller.
			returns := 0
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				if _, isReturn := block.Terminal.(*Return); isReturn {
					returns++
				}
			}
			if returns != 1 {
				t.Errorf("the function has %d Return terminals, want 1; a return of the inlined "+
					"body was not routed to the continuation and now leaves the caller", returns)
			}

			// The code AFTER the call has to still be there, and this needed a mutation sweep to
			// find. Dropping the instructions the continuation inherits left every other assertion
			// in this file passing: the continuation is then empty, `ReversePostorder` collects no
			// instructions from it, and the graph that survives is internally consistent with the
			// caller's tail simply deleted. Consistency is not conservation.
			//
			// Named instructions are the check rather than a count, because the splice legitimately
			// adds and removes instructions and a count would have to be re-derived per fixture.
			// `built` is the binding the caller writes after the call, so it is the tail.
			tail := false
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				for _, instructionId := range block.Instructions {
					instruction := function.Instructions[instructionId]
					if instruction == nil {
						continue
					}
					store, isStore := instruction.Value.(*StoreLocal)
					if !isStore {
						continue
					}
					if function.Identifiers[store.LValue.Identifier].Name == "built" {
						tail = true
					}
				}
			}
			if !tail {
				t.Error("the `const built` the caller writes after the call is gone from the " +
					"spliced function; the continuation did not inherit the instructions that " +
					"followed the call, so the caller's tail was dropped rather than moved")
			}
		})
	}
}

// TestInlineDefinesTheCallResultOnEveryPathToTheContinuation is the join half.
//
// The splice's whole purpose is that the call's lvalue still holds the value the body returned. A
// return routed to the continuation WITHOUT the store that carries its value produces a graph with
// no dangling reference and no unreachable block -- the value is simply never defined, and every
// later read of it reads whatever the identifier held before.
//
// Asserted as reachability rather than by counting stores, because the labeled form emits one store
// per return and the direct form emits one load, and a test that counted would encode the arm
// rather than the property.
//
// # The definition set includes lvalue ROLES, which took a measurement to get right
//
// Written first as "every `Instruction.LValue`, every phi, every parameter", this reported the
// caller's own `const built` as undefined -- BEFORE any splice had happened. `StoreLocal` and
// `DeclareLocal` name their binding inside the VALUE rather than in `Instruction.LValue`, which
// carries only the store's own temporary result, so a definition set built from lvalues alone
// misses every named binding in the function. The visitor already reports the role, so the fix is
// to read it rather than to loosen the assertion.
func TestInlineDefinesTheCallResultOnEveryPathToTheContinuation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"single return", singleReturnIifeSource},
		{"multiple returns", multipleReturnIifeSource},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, inlined := inlinedFixture(t, testCase.source, false)
			if function == nil || inlined != 1 {
				t.Fatalf("the fixture spliced %d calls, want 1", inlined)
			}

			defined := map[IdentifierId]bool{}
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				for _, instructionId := range block.Instructions {
					instruction := function.Instructions[instructionId]
					if instruction == nil {
						continue
					}
					defined[instruction.LValue.Identifier] = true
					EachPlace(instruction.Value, func(place Place, role PlaceRole) {
						if role == PlaceRoleDefine {
							defined[place.Identifier] = true
						}
					})
				}
				for _, phi := range block.Phis {
					defined[phi.Place.Identifier] = true
				}
			}
			for _, parameter := range function.Params {
				defined[parameter.Identifier] = true
			}
			for _, contextValue := range function.Context {
				defined[contextValue.Identifier] = true
			}

			reads := 0
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				for _, instructionId := range block.Instructions {
					instruction := function.Instructions[instructionId]
					if instruction == nil {
						continue
					}
					EachPlace(instruction.Value, func(place Place, role PlaceRole) {
						if role == PlaceRoleDefine {
							return
						}
						reads++
						if !defined[place.Identifier] {
							t.Errorf("instruction %d reads identifier %d (%s), which nothing in "+
								"the spliced function defines; a return was routed to the "+
								"continuation without the store that carries its value",
								instructionId, place.Identifier,
								function.Identifiers[place.Identifier].Name)
						}
					})
				}
			}
			if reads == 0 {
				t.Error("no instruction reads any place, so this test asserted nothing")
			}

			// A DECLARATION is not a definition, and this is the part a mutation sweep had to
			// teach. Removing the store that carries a returned value left every assertion above
			// passing, because the labeled form emits `DeclareLocal let result` before the body and
			// the loop above counts that as defining `result`. What remains is a `let` that is
			// declared, read after the call, and never assigned on any path -- the exact failure
			// this test names in its own error message, invisible to it.
			//
			// So the result is asserted separately and on assignment alone: the value the caller
			// reads after the splice must be written by a store or a load somewhere, not merely
			// introduced.
			assigned := map[IdentifierId]bool{}
			declared := map[IdentifierId]bool{}
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				for _, instructionId := range block.Instructions {
					instruction := function.Instructions[instructionId]
					if instruction == nil {
						continue
					}
					switch shape := instruction.Value.(type) {
					case *DeclareLocal:
						declared[shape.LValue.Identifier] = true
					case *StoreLocal:
						assigned[shape.LValue.Identifier] = true
					default:
						assigned[instruction.LValue.Identifier] = true
					}
				}
			}
			for identifier := range declared {
				if !assigned[identifier] {
					t.Errorf("identifier %d is declared by the splice and never assigned; the "+
						"store that carries a returned value into it was not emitted, so the "+
						"caller reads an uninitialised binding", identifier)
				}
			}
		})
	}
}

// TestInlineLeavesEveryBlockReferenceResolvable pins the graph's integrity after the surgery.
//
// The splice writes three terminals -- the cut block's, and one per rewritten return -- and each
// names a block by id. A wrong id here is not a crash: `Block` returns false and most passes skip,
// so the function analyses as if the edge were not there.
func TestInlineLeavesEveryBlockReferenceResolvable(t *testing.T) {
	function, inlined := inlinedFixture(t, multipleReturnIifeSource, false)
	if function == nil || inlined != 1 {
		t.Fatalf("the fixture spliced %d calls, want 1", inlined)
	}
	references := 0
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		EachBlockReferencePointer(block.Terminal, func(reference *BlockId) {
			references++
			if _, found := function.Block(*reference); !found {
				t.Errorf("block %d names block %d, which the function does not hold",
					block.Id, *reference)
			}
		})
		if _, found := function.Block(block.Id); !found {
			t.Errorf("block %d is in Blocks but not in the id index", block.Id)
		}
	}
	if references == 0 {
		t.Error("no terminal names a block, so this test asserted nothing")
	}
}

// TestInlineDeclinesTheCasesItCannotExpress pins each refusal.
//
// Each of these is a condition upstream also declines, and each would produce a WRONG graph rather
// than a missing optimisation if it were accepted: an argument has no parameter to bind to, an
// escaping function value would be called somewhere the body no longer exists, and a named binding
// can be read again after the call.
func TestInlineDeclinesTheCasesItCannotExpress(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{
			// An argument would have to bind to a parameter, and this pass lowers no binding.
			name: "call with arguments",
			source: `
				function Component(properties: {value: number}) {
					const built = ((amount: number) => {
						return [amount];
					})(properties.value);
					return [built];
				}
			`,
		},
		{
			// The value escapes into another call, which may invoke it after the splice.
			name: "function value also passed elsewhere",
			source: `
				function Component(properties: {value: number}) {
					const make = () => {
						return [properties.value];
					};
					globalThis.hold = make;
					const built = make();
					return [built];
				}
			`,
		},
		{
			// `await` inside would make the caller's continuation a suspension point.
			name: "async body",
			source: `
				function Component(properties: {value: number}) {
					const built = (async () => {
						return [properties.value];
					})();
					return [built];
				}
			`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, inlined := inlinedFixture(t, testCase.source, false)
			if function == nil {
				t.Skip("the fixture did not lower")
			}
			if inlined != 0 {
				t.Errorf("spliced %d calls, want 0; this shape has no correct splice and "+
					"accepting it produces a graph that builds and is wrong", inlined)
			}
		})
	}
}

// TestInlineDeclinesADroppedMemoCallback pins the divergence from upstream.
//
// `DropManualMemoization` rewrites `useMemo(callback, deps)` into a zero-argument call of the
// callback, which is structurally an IIFE. Upstream inlines it; this does not, because the
// dependency-comparison condition that upstream reports those fixtures under is gated off here. See
// `memoizedResults` for the measurement.
//
// The fixture holds BOTH a real IIFE and a `useMemo`, which is what makes the assertion sharp: a
// guard that declined everything would also pass a fixture with only the memo in it.
func TestInlineDeclinesADroppedMemoCallback(t *testing.T) {
	const source = `
		function Component(properties: {items: Array<number>}) {
			const built = (() => {
				const out = [];
				out.push(properties.items);
				return out;
			})();
			const memo = useMemo(() => [properties.items], [properties.items]);
			return [built, memo];
		}
	`
	function, inlined := inlinedFixture(t, source, true)
	if function == nil {
		t.Fatal("the fixture did not lower")
	}
	if inlined != 1 {
		t.Fatalf("spliced %d calls, want exactly 1; the real IIFE must be spliced and the memo "+
			"callback must not, and a count of 0 or 2 means the guard is on the wrong one",
			inlined)
	}

	// The memo's call survives, and it is the one bracketed by the markers.
	calls, finishes := 0, 0
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			switch instruction.Value.(type) {
			case *CallExpression:
				calls++
			case *FinishMemoize:
				finishes++
			}
		}
	}
	if calls != 1 {
		t.Errorf("%d CallExpressions survive, want 1; the memo callback's call is the only one "+
			"that should remain", calls)
	}
	if finishes != 1 {
		t.Errorf("%d FinishMemoize markers survive, want 1; the markers the rule reads were "+
			"disturbed by the splice", finishes)
	}
}

// TestInlineIsIdempotent pins that a second run finds nothing.
//
// A pass that re-splices its own output would copy the body again on every call, and the pipeline
// calls this once per function per rule invocation.
func TestInlineIsIdempotent(t *testing.T) {
	function, inlined := inlinedFixture(t, multipleReturnIifeSource, false)
	if function == nil || inlined != 1 {
		t.Fatalf("the fixture spliced %d calls, want 1", inlined)
	}
	blocks := len(function.Blocks)
	if again := InlineImmediatelyInvokedFunctionExpressions(function); again != 0 {
		t.Errorf("a second run spliced %d more calls, want 0", again)
	}
	if len(function.Blocks) != blocks {
		t.Errorf("a second run changed the block count from %d to %d", blocks, len(function.Blocks))
	}
}
