package typescript

import (
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/upstream"
	"github.com/system-inc/verify/internal/upstream/tsgolint/rules/no_array_delete"
)

// NoArrayDelete flags the `delete` operator applied to an element of an array or a tuple.
//
//	valid:   declare const obj: { a: 1; b: 2 }; delete obj.a
//	valid:   declare const obj: { a: 1; b: 2 }; delete obj['a']
//	valid:   declare const maybeArray: any; delete maybeArray[0]
//	valid:   declare const test: never; delete test[0]
//	invalid: declare const arr: number[]; delete arr[0]
//	invalid: declare const tuple: [number, string]; delete tuple[0]
//	invalid: delete [1, 2, 3][0]
//
// `delete arr[0]` leaves a hole rather than shortening the array: the length is unchanged, the slot
// reads back as `undefined`, and every later index keeps its old position. That is almost never
// what the author meant, which is why the repair is offered as a suggestion rather than applied.
//
// # This is wiring plus vendoring, not a re-implementation
//
// oxc declares `NoArrayDelete(tsgolint)` and carries no algorithm and no corpus, so the behavior
// oxlint exhibits for this rule IS tsgolint's: the release binary shells out to a `tsgolint`
// executable and refuses to run the rule when that binary is absent, which is what it does on this
// machine. That refusal is itself the proof, so oxlint cannot serve as ground truth here and
// tsgolint's own test file is the corpus instead.
//
// Unlike `await-thenable`, this rule was NOT already vendored. It was fetched from tsgolint's tree
// and placed at `internal/upstream/tsgolint/rules/no_array_delete`, byte-identical below the import
// block, which is the only edit: the module paths are rewritten from
// `github.com/microsoft/typescript-go/shim/...` to `github.com/microsoft/TypeScript/tsc/shim/...`
// and from `github.com/typescript-eslint/tsgolint/internal/...` to this tree's vendor path. Every
// utility it calls was already here, and every checker call it makes was confirmed reachable
// through our shims by a compiling probe rather than by a grep, since `Checker_isArrayOrTupleType`
// and `scanner.GetRangeOfTokenAtPosition` are neither of them on the list an earlier port
// established.
//
// Retyping the algorithm here would produce a second copy of logic we already carry, which would
// then drift with nothing comparing them. So the decision is to vendor and wire, and the
// verification effort went into proving the vendored rule reproduces upstream: all thirty one of
// tsgolint's own cases are replayed through the adapted rule, including all twenty two exact
// suggestion outputs.
//
// # What decides a finding
//
// The anchor is `KindDeleteExpression`. The operand has its parentheses skipped, so
// `delete ((a[b]))` reports, and it must then be an element access: `delete obj.a` is a property
// access and is silent even when `obj` is an array. The type of the RECEIVER is then resolved
// through `GetConstrainedTypeAtLocation`, which walks a type parameter to its base constraint, so
// both `getArray<T extends number[]>(): T` and `getArray<T extends number>(): T[]` report.
//
// The array test is asymmetric between the two composite forms and the corpus pins both directions.
// A union reports only when EVERY member is an array or a tuple, so `number[] | string[]` reports.
// An intersection reports when ANY member is, so `number[] & unknown` and `string & Array<number>`
// both report. Reading the rule file alone would suggest one predicate; it is two.
//
// `any`, `unknown` and `never` are silent, and they are silent as a consequence of the array test
// rather than through an explicit skip: none of them is an array or a tuple. That distinction
// matters for the fixture tsconfig, which pins `lib: ["ES2022"]`, because a type outside that
// library resolves to the error type whose flags report `any`. Nothing in this rule's corpus needs
// a type outside ES2022, so no case here is at risk of that; `Array` and tuples are all in scope.
//
// # Where the two upstreams disagree, and it is the REPAIR
//
// Both implementations register exactly ONE listener, carry the same two messages, ship the repair
// as a SUGGESTION rather than a fix, and compute `isUnderlyingTypeArray` identically, including the
// every/some asymmetry. Counted on both sides rather than assumed.
//
// They construct the repair completely differently, and the outputs differ observably.
//
// `@typescript-eslint` 8.x replaces the WHOLE node with a reconstructed string:
// it reads `object.getText()` and `property.getText()`, wraps the key in parentheses when it is a
// sequence expression, then hoists every comment inside the node up in front of the replacement,
// re-indented to the node's own column.
//
// tsgolint makes THREE surgical range edits instead: it removes the `delete` token, replaces the
// `[` token with `.splice(`, and replaces the `]` token with `, 1)`. Nothing else in the span is
// touched.
//
// Three consequences, all of them visible in the corpus and all of them reproduced here:
//
// A LEADING SPACE survives where `delete` was removed, because only the keyword token is taken and
// the space after it is not. Every one of upstream's twenty two expected outputs begins
// ` arr.splice(...)` rather than `arr.splice(...)`, and a repair that also ate the space would
// satisfy every message id while failing every output.
//
// COMMENTS STAY WHERE THEY WERE. Upstream's comment-heavy case keeps `/** multi line */` sitting
// between the removed keyword and the receiver, and keeps `/* inline */` inside the index
// expression, because those bytes were never in an edited range. The `@typescript-eslint` fixer
// would have collected them and stacked them above the call.
//
// A SEQUENCE EXPRESSION KEEPS ITS OWN PARENTHESES rather than being given new ones.
// `delete arr[(doWork(), 1)]` becomes ` arr.splice((doWork(), 1), 1)` because the parentheses are
// part of the untouched argument text, so tsgolint needs no special case where the other upstream
// has one. The two agree on the output for this input and reach it by different routes.
//
// tsgolint wins on every disagreement, because oxlint runs tsgolint and oxlint is what the
// differential harness compares against. Reproducing the other repair would be a difference the
// harness could see, for no gain.
//
// # The checker, and why the guard does not live here
//
// The listener reads `ctx.TypeChecker` unconditionally, so this needs the checker and its fixtures
// use `RunTyped`. The standing advice to write `if ctx.TypeChecker == nil { return }` at the top of
// every listener cannot be followed in this file: the listener is upstream's, and editing it is
// what would turn a re-sync into a merge.
//
// The guard is one level up. `upstream.Adapt` sets `NeedsTypeChecker` on every rule it wraps
// unconditionally, so the nil case is unreachable through registration. A test in this package pins
// that declaration so a later revert fails loudly rather than going vacuously green, which is the
// dangerous direction here: under the untyped harness this rule goes silent rather than panicking,
// and silence makes every clean fixture pass having proven nothing.
//
// Reaching the checker only through a vendored file in another package also means the registry's
// per-file textual guard, which looks for a `.TypeChecker` selector in the rule's own file, will
// report this as over-declared. That message is wrong and the declaration is right.
//
// # Two mutants survive the sweep and both are equivalent rather than unseen
//
// Rewriting `utils.TrimNodeTextRange(sourceFile, n)` to `n.Loc` for either the receiver range or the
// argument range compiles, changes bytes, and no fixture notices. That reads as a blind spot and it
// is not one, which matters because the trivia-trimming difference is a real defect this project has
// already shipped once at the adapter.
//
// It cannot bite HERE. `TrimNodeTextRange` is
// `GetRangeOfTokenAtPosition(file, n.Pos()).WithEnd(n.End())`, so it differs from `n.Loc` only in
// where the range STARTS. Both spellings end at `n.End()`. And this rule reads nothing but the end:
// `expressionRange` and `argumentRange` are each consumed exactly once, as `.End()`, to locate the
// bracket tokens. No code path reads either `Pos()`, so no input can distinguish the two versions.
//
// That claim is checked rather than asserted. The same mutation applied to the value actually read,
// turning `expressionRange.End()` into `expressionRange.End() + 1`, is CAUGHT by twenty six failing
// assertions, so the fixtures can see this range and the survival is about the mutation rather than
// about the coverage.
//
// The trimming still belongs in the vendored file exactly as upstream wrote it. It is inert for
// these two call sites today and would stop being inert the moment anything read a start offset.
//
// # Cost
//
// `KindDeleteExpression` is a rare anchor and the listener exits on the second line for anything
// that is not an element access, so the checker is consulted only for a genuine `delete x[y]`.
var NoArrayDelete = adaptNoArrayDelete()

// adaptNoArrayDelete wires the vendored rule, panicking at startup if it cannot be adapted.
//
// `MustAdapt` rather than `Adapt` because a rule that cannot be adapted is a build-time mistake:
// the registry is assembled at process start, so failing there stops the tool immediately instead
// of leaving a rule silently absent from a run that otherwise looks clean.
func adaptNoArrayDelete() rule.Rule {
	return upstream.MustAdapt(no_array_delete.NoArrayDeleteRule)
}
