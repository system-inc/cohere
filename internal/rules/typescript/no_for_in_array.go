package typescript

import (
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/upstream"
	"github.com/system-inc/verify/internal/upstream/tsgolint/rules/no_for_in_array"
)

// NoForInArray flags a `for...in` loop whose subject is array-like.
//
//	valid:   for (const x of [3, 4, 5]) {}
//	valid:   for (const x in { a: 1, b: 2, c: 3 }) {}
//	valid:   declare const obj: { [key: number]: number }; for (const key in obj) {}
//	invalid: for (const x in [3, 4, 5]) {}
//	invalid: declare const array: [number, string]; for (const key in array) {}
//	invalid: function f() { for (const a in arguments) {} }
//
// A for-in loop over an array skips holes, hands back indices as strings rather than numbers, and
// walks the prototype chain along with any other enumerable property, so it is almost never what
// the author meant.
//
// # This rule is wiring rather than a port
//
// oxc has no implementation. `no_for_in_array.rs` declares `NoForInArray(tsgolint)` and its body is
// `impl Rule for NoForInArray {}`, sixty four lines of documentation around an empty block. There
// is no oxc corpus either, so the extractor has nothing to extract. oxlint's behavior for this rule
// IS tsgolint's, by delegation, and on this machine oxlint refuses to run it at all with `Failed to
// find tsgolint executable` rather than falling back to something of its own.
//
// So the algorithm was vendored from tsgolint rather than retyped, at
// `internal/upstream/tsgolint/rules/no_for_in_array`, and `internal/rules/upstream.Adapt` converts
// it. Retyping it here would produce a second copy of logic we already carry, which would drift
// from the vendored one with nothing comparing them.
//
// Unlike `await-thenable`, this rule was NOT already vendored. It was fetched from tsgolint's tree
// and carries the same four import-line rewrites every vendored file carries, plus ONE real edit
// described below.
//
// # The one edit: upstream misspells its own rule name
//
// Upstream declares `Name: "no-for-in-array-rule"`. Every other rule in tsgolint spells its name as
// its directory with underscores turned to dashes, and this one appends "-rule".
//
// That was measured rather than eyeballed: all 38 rules under tsgolint's `internal/rules/` were
// fetched and each Name compared against its directory, and this is the only disagreement in the
// family. It is live upstream rather than absorbed downstream, because `cmd/tsgolint/main.go`
// builds its `ConfiguredRule` with `Name: r.Name` and no mapping table.
//
// The suffix cannot ship here. verify keys the catalog, the config file, and suppression comments
// on this exact string, so `no-for-in-array-rule` would register a rule that
// `typescript/no-for-in-array` in `VerifySettings.json` never enables, that the inventory entry
// never matches, and that no suppression comment an author would plausibly write could silence. It
// is corrected in the vendored file, at the line, with the measurement recorded beside it.
//
// # Where the two references disagree: they do not
//
// This is the opposite of what `await-thenable`'s port found, and it was established by running
// both rather than by reading them. `@typescript-eslint` 8.67.0 was driven through its Linter API
// on a real program over all 24 of tsgolint's own cases, and it agreed on every one: same verdicts,
// same finding counts, and the same line and column spans on all 20 diagnostics. Zero
// disagreements.
//
// Listener counts match too, which is the check `await-thenable` earned. Both sides register
// exactly ONE listener on the for-in statement. `@typescript-eslint` declares `schema: []`, so
// there is no option surface on either side, and the message id and its full text are identical
// strings. There is no fourth arm here of the kind `await-thenable` had.
//
// # What decides a finding
//
// The subject's type is resolved through its base constraint first, so a type parameter reports
// exactly when what it is constrained to would: `<T extends any[]>(arr: T)` fires. The predicate
// then recurses through unions and intersections and asks two things of each part, both of which
// must hold: it has a NUMBER index signature, and it has a `length` property whose type is
// number-like.
//
// Both halves are load-bearing and the corpus pins each one from the other side. `{ [key: number]:
// number }` has the index signature and no length, and it is a PASSING case; adding `length: 1` to
// that same object is a FAILING case. And because the recursion is a `some` rather than an `every`,
// a union reports when any part is array-like, which is why `string[] | null` and `boolean[] | { a:
// 1 }` both fire.
//
// So the set of things that report is wider than "an array", and it was read off the corpus rather
// than off the rule's name: arrays, tuples, `RegExpExecArray`, `HTMLCollection`, `NodeList`, an
// `arguments` object, a union of an array with a plain object, a type parameter constrained to an
// array, and any hand-written object carrying both a number index and a numeric length. A string is
// NOT among them and neither is a `Set`; upstream writes no case for either, and neither has a
// number index signature.
//
// # It reports and offers nothing
//
// No fix and no suggestion, on either side. `@typescript-eslint` reports with a bare `loc` and
// `messageId`, and the vendored rule calls `ctx.ReportRange` rather than any of the fix-carrying
// forms. Rewriting a for-in into a for-of changes what the loop binds, from a string key to a
// value, so there is no repair that preserves meaning and nothing to apply unattended.
//
// # The span is the head of the loop, not the statement
//
// Upstream reports `GetForStatementHeadLoc`, which runs from the `for` keyword to the start of the
// loop BODY, deliberately excluding the body itself. It is also the reason the node-report trivia
// question does not arise: this is a `ReportRange` of a range upstream computed, so the adapter's
// `TokenRange` trimming is not in the path at all, and the trimming that does happen is
// `TrimNodeTextRange` inside the helper.
//
// That span is asserted rather than assumed. All 18 of upstream's single-file diagnostics are
// replayed in the test file against their exact line and column, including the two cases where the
// head runs across ten lines through nested parentheses and trailing comments.
//
// # The checker
//
// Every listener reads `ctx.TypeChecker`, so this needs the checker and its fixtures use RunTyped.
// The standing `if ctx.TypeChecker == nil { return }` guard cannot live in this file: the listener
// is upstream's, and editing it is what keeps re-syncing a diff rather than a merge. The guard is
// one level up, where `upstream.Adapt` sets `NeedsTypeChecker` unconditionally, so the nil case is
// unreachable through registration. A test in this package pins that declaration so a later revert
// fails loudly instead of going vacuously green.
//
// Reaching the checker only through a vendored file in another package also means the registry's
// per-file textual guard, which looks for a `.TypeChecker` selector in the rule's own file, reports
// this as over-declared. That message is wrong and the declaration is right.
var NoForInArray = adaptNoForInArray()

// adaptNoForInArray wires the vendored rule, panicking at startup if it cannot be adapted.
//
// `MustAdapt` rather than `Adapt` because a rule that cannot be adapted is a build-time mistake:
// the registry is assembled at process start, so failing there stops the tool immediately instead
// of leaving a rule silently absent from a run that otherwise looks clean.
func adaptNoForInArray() rule.Rule {
	return upstream.MustAdapt(no_for_in_array.NoForInArrayRule)
}
