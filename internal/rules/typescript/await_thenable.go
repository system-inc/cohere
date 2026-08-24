package typescript

import (
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/upstream"
	"github.com/system-inc/verify/internal/upstream/tsgolint/rules/await_thenable"
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
// # This rule is wiring rather than a port, and that is the whole finding
//
// oxc has no implementation. Its rule file declares `AwaitThenable(tsgolint)` and delegates, with
// fifty seven lines carrying documentation and no algorithm. So the behavior oxlint exhibits for
// this rule IS tsgolint's, literally: the release binary shells out to a `tsgolint` executable and
// refuses to run the rule when that binary is absent, which is what it does on this machine.
//
// tsgolint's rule is already vendored in this tree at
// `internal/upstream/tsgolint/rules/await_thenable`, byte-identical to upstream, and
// `internal/rules/upstream.Adapt` already converts it. Both compile against the same shims over the
// same typescript-go commit. What was missing was the registration: `MustAdapt` had zero call sites
// outside its own test, so the vendored rule ran on nothing. That is exactly the "present and
// blind" failure the adapter's own doc comment names, sitting one line away from being fixed.
//
// Rewriting the algorithm by hand here would produce a second copy of logic we already carry, which
// would then drift from the vendored one with nothing comparing them. The decision this file makes
// is therefore to wire rather than to re-implement, and the verification effort went into proving
// the vendored rule reproduces upstream rather than into retyping it. See the test file: all forty
// eight of tsgolint's own cases were replayed through the adapted rule and all forty eight agree.
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
// # The checker, and why the guard question does not arise here
//
// Every listener reads `ctx.TypeChecker` unconditionally, so this needs the checker and its
// fixtures use `RunTyped`. The standing advice is to write `if ctx.TypeChecker == nil { return }`
// at the top of every listener in a typed rule, and this file cannot follow it: the listeners are
// upstream's and editing them is what keeps re-syncing a diff rather than a merge.
//
// The guard is not missing, it is one level up. `upstream.Adapt` sets `NeedsTypeChecker` on every
// adapted rule unconditionally, precisely because it cannot see whether the rule it wraps reads the
// checker, and it comments that under-declaring risks a nil checker inside a type-aware rule while
// over-declaring only costs a lock. So the nil case is unreachable through registration. A test in
// this package asserts the declaration is present, so a later revert fails loudly rather than going
// vacuously green.
//
// Reaching the checker only through a vendored file in another package also means the registry's
// per-file textual guard, which looks for a `.TypeChecker` selector in the rule's own file, will
// report this as over-declared. That message is wrong and the declaration is right.
//
// # Cost
//
// The anchor is what to price, not the checker. `KindAwaitExpression` and `KindForOfStatement` are
// uncommon nodes, and `KindVariableDeclarationList` is common but exits on the first line for
// anything that is not `await using`. Measured on the real tree, see the test file's note.
var AwaitThenable = adaptAwaitThenable()

// adaptAwaitThenable wires the vendored rule, panicking at startup if it cannot be adapted.
//
// `MustAdapt` rather than `Adapt` because a rule that cannot be adapted is a build-time mistake:
// the registry is assembled at process start, so failing there stops the tool immediately instead
// of leaving a rule silently absent from a run that otherwise looks clean.
func adaptAwaitThenable() rule.Rule {
	return upstream.MustAdapt(await_thenable.AwaitThenableRule)
}
