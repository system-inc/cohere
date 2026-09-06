package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/utilities/type_checking"
)

// AwaitThenable flags `await` applied to a value that is not a Thenable, `for await...of` over a
// value that is not async iterable, and `await using` on a value that is not async disposable.
//
//	valid:   await Promise.resolve('value')
//	valid:   await (async () => true)()
//	valid:   let anyValue: any; await anyValue
//	valid:   async function wrapper<T>(value: T) { return await value; }
//	invalid: await 0
//	invalid: await (() => {})
//	invalid: for await (const value of yieldNumbers())
//	invalid: declare const d: Disposable; await using x = d
//
// # Absorbed from tsgolint, which is the source of record for this rule
//
// Provenance: tsgolint `internal/rules/await_thenable/await_thenable.go`, vendored at commit
// `05b7fbc` and absorbed onto cohere's own rule interface here. It reaches for `NeedsToBeAwaited`,
// `GetWellKnownSymbolPropertyOfType` and `GetForStatementHeadLoc` because that is what upstream
// reaches for, and this note is why a reader finds those helpers in a file that otherwise looks
// native.
//
// tsgolint is not re-synced, so this file is now the only copy of the algorithm rather than a
// translation layer over a vendored one. The checker logic below is byte-identical to upstream's;
// what changed is the interface it speaks: `rule.RuleContext` became `rule.Context`,
// `RuleListeners` became `Listeners`, `rule.RuleMessage` became `rule.Message`, and each
// suggestion moved from `rule.RuleSuggestion{FixesArr: []rule.RuleFix{...}}` to
// `rule.Suggestion{Fixes: []rule.Fix{...}}`. The three fix constructors map one to one:
// `RuleFixRemoveRange` is `rule.RemoveRange`, and `RuleFixRemove(file, node)` is `ctx.RemoveNode`,
// which are the same expression under different names since `type_checking.TrimNodeTextRange` and
// `rule.TokenRange` are both `GetRangeOfTokenAtPosition(file, node.Pos()).WithEnd(node.End())`. No
// predicate, no tri-state and no traversal was touched.
//
// The reported SPANS are unchanged, and that is the part worth naming. While this rule was adapted,
// `upstream.Adapt` wrapped every node report in `rule.TokenRange(SourceFile, node)` because
// tsgolint's own runner does the same through `type_checking.TrimNodeTextRange`. Our native
// `ctx.ReportNodeWithSuggestions` applies exactly that trim itself, so absorbing the rule preserves
// the behavior rather than relying on the adapter to supply it. Passing `node.Loc` instead would
// include leading trivia, so an indented `await 0` would report the text "\n  await 0" and an
// `await using` initializer would report " disposable" with a leading space; that is the defect
// fixed in `8bdd70b`, and the span test in this package pins both shapes against it. The
// `for await...of` arm reports a computed head range rather than a node, so it goes through
// `ReportRangeWithSuggestions`, which trims nothing by design because the caller already said what
// it meant.
//
// oxc has no implementation. Its rule file declares `AwaitThenable(tsgolint)` and delegates, with
// fifty seven lines carrying documentation and no algorithm. So the behavior oxlint exhibits for
// this rule IS tsgolint's, literally: the release binary shells out to a `tsgolint` executable and
// refuses to run the rule when that binary is absent, which is what it does on this machine. All
// forty eight of tsgolint's own cases are replayed in the test file and all forty eight agree.
//
// # Where the two upstreams disagree, measured rather than read
//
// `@typescript-eslint` 8.67.0 carries a FOURTH listener that tsgolint has no counterpart for: a
// `CallExpression` arm reporting `invalidPromiseAggregatorInput` when a promise aggregator receives
// non-Thenable values. Driven through its Linter API on a real program, `await Promise.all([1, 2,
// 3])` reports three findings there and is silent here; `Promise.allSettled`, `Promise.race` and
// `Promise.any` behave the same way. tsgolint mentions no aggregator anywhere in the rule, its
// tests, or its utils directory, and it registers exactly three listeners.
//
// That divergence is reproduced as silence deliberately. oxlint is what the differential harness
// compares against and oxlint runs tsgolint, so adding the aggregator arm would report findings the
// gate does not, which costs the harness meaning for no gain. It is upstream drift rather than an
// oversight: the message id does not appear in tsgolint at all, so tsgolint predates that feature.
//
// The two agree on everything else. All thirty valid and eighteen invalid cases from tsgolint's own
// test file were additionally driven through `@typescript-eslint`, and it produced identical
// verdicts on all forty eight, including the suggestion ids.
//
// # What decides a finding
//
// `NeedsToBeAwaited` returns a tri-state and only `Never` reports. It resolves a generic to its
// base constraint first, treats an unconstrained generic and `any` and `unknown` as `May`, and asks
// `IsThenableType` for the rest, which walks the union parts of the apparent type looking for a
// `then` property whose call signatures take a callback first parameter. So `await value` inside
// `wrapper<T>(value: T)` is silent while `wrapper<T extends number>(value: T)` reports, and the
// corpus pins eight cases around exactly that boundary. A port written from the rule file alone
// would miss the constraint resolution entirely, because it lives in the helper.
//
// The other two arms ask a different question: whether the type carries the well-known symbol
// `asyncIterator` or `asyncDispose`, looked up through `GetPropertyNameForKnownSymbolName`. Those
// are not `then` checks and do not go through the tri-state.
//
// # The checker, and the nil guard that now lives here
//
// Every listener reads `ctx.TypeChecker` unconditionally, so this needs the checker and its
// fixtures use `RunTyped`. While this rule was adapted, the standing
// `if ctx.TypeChecker == nil { return }` could not be written, because the listeners were
// upstream's and editing them would have turned a re-sync into a merge. Absorbing the rule removes
// that constraint, so the guard is now written where the advice always wanted it.
//
// It is placed in `Run` rather than repeated at the top of all three listeners, which declines the
// file once instead of three times per node and is the cheapest thing a rule can do. The guard is
// unreachable through registration, since `NeedsTypeChecker` is declared right above; it is here
// for the harness path, where a Context can be built by hand. A test in this package pins both the
// declaration and the decline, so a later revert fails loudly rather than going vacuously green.
//
// # Cost
//
// The anchor is what to price, not the checker. `KindAwaitExpression` and `KindForOfStatement` are
// uncommon nodes, and `KindVariableDeclarationList` is common but exits on the first line for
// anything that is not `await using`. Measured on the real tree, see the test file's note.
var AwaitThenable = rule.Rule{
	Name: "@typescript-eslint/await-thenable",

	// All three listeners read ctx.TypeChecker unconditionally, so the checker is required.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil {
			return nil
		}

		return rule.Listeners{
			ast.KindAwaitExpression: func(node *ast.Node) {
				awaitArgument := node.AsAwaitExpression().Expression
				awaitArgumentType := ctx.TypeChecker.GetTypeAtLocation(awaitArgument)
				certainty := type_checking.NeedsToBeAwaited(ctx.TypeChecker, awaitArgument, awaitArgumentType)

				if certainty == type_checking.TypeAwaitableNever {
					ctx.ReportNodeWithSuggestions(node, buildAwaitMessage(), rule.Suggestion{
						Message: buildRemoveAwaitMessage(),
						Fixes: []rule.Fix{
							rule.RemoveRange(scanner.GetRangeOfTokenAtPosition(ctx.SourceFile, node.Pos())),
						},
					})
				}
			},
			ast.KindForOfStatement: func(node *ast.Node) {
				stmt := node.AsForInOrOfStatement()
				if stmt.AwaitModifier == nil {
					return
				}

				exprType := ctx.TypeChecker.GetTypeAtLocation(stmt.Expression)
				if type_checking.IsTypeAnyType(exprType) {
					return
				}

				for _, typePart := range type_checking.UnionTypeParts(exprType) {
					if type_checking.GetWellKnownSymbolPropertyOfType(typePart, "asyncIterator", ctx.TypeChecker) != nil {
						return
					}
				}

				ctx.ReportRangeWithSuggestions(
					type_checking.GetForStatementHeadLoc(ctx.SourceFile, node),
					buildForAwaitOfNonAsyncIterableMessage(),
					// Note that this suggestion causes broken code for sync iterables
					// of promises, since the loop variable is not awaited.
					rule.Suggestion{
						Message: buildConvertToOrdinaryForMessage(),
						Fixes: []rule.Fix{
							ctx.RemoveNode(stmt.AwaitModifier),
						},
					},
				)
			},
			ast.KindVariableDeclarationList: func(node *ast.Node) {
				if !ast.IsVarAwaitUsing(node) {
					return
				}

				declaration := node.AsVariableDeclarationList()
			DeclaratorLoop:
				for _, declarator := range declaration.Declarations.Nodes {
					init := declarator.Initializer()
					if init == nil {
						continue
					}
					initType := ctx.TypeChecker.GetTypeAtLocation(init)
					if type_checking.IsTypeAnyType(initType) {
						continue
					}

					for _, typePart := range type_checking.UnionTypeParts(initType) {
						if type_checking.GetWellKnownSymbolPropertyOfType(typePart, "asyncDispose", ctx.TypeChecker) != nil {
							continue DeclaratorLoop
						}
					}

					var suggestions []rule.Suggestion
					// let the user figure out what to do if there's
					// await using a = b, c = d, e = f;
					// it's rare and not worth the complexity to handle.
					if len(declaration.Declarations.Nodes) == 1 {
						suggestions = append(suggestions, rule.Suggestion{
							Message: buildRemoveAwaitMessage(),
							Fixes: []rule.Fix{
								rule.RemoveRange(scanner.GetRangeOfTokenAtPosition(ctx.SourceFile, node.Pos())),
							},
						})
					}

					ctx.ReportNodeWithSuggestions(init, buildAwaitUsingOfNonAsyncDisposableMessage(), suggestions...)
				}
			},
		}
	},
}

func buildAwaitMessage() rule.Message {
	return rule.Message{
		Id:          "await",
		Description: "Unexpected `await` of a non-Promise (non-\"Thenable\") value.",
	}
}

func buildRemoveAwaitMessage() rule.Message {
	return rule.Message{
		Id:          "removeAwait",
		Description: "Remove unnecessary `await`.",
	}
}

func buildForAwaitOfNonAsyncIterableMessage() rule.Message {
	return rule.Message{
		Id:          "forAwaitOfNonAsyncIterable",
		Description: "Unexpected `for await...of` of a value that is not async iterable.",
	}
}

func buildConvertToOrdinaryForMessage() rule.Message {
	return rule.Message{
		Id:          "convertToOrdinaryFor",
		Description: "Convert to an ordinary `for...of` loop.",
	}
}

func buildAwaitUsingOfNonAsyncDisposableMessage() rule.Message {
	return rule.Message{
		Id:          "awaitUsingOfNonAsyncDisposable",
		Description: "Unexpected `await using` of a value that is not async disposable.",
	}
}
