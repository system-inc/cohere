package typescript

import (
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/upstream"
	"github.com/system-inc/verify/internal/upstream/tsgolint/rules/switch_exhaustiveness_check"
)

// SwitchExhaustivenessCheck flags a switch over a union of literal types that does not handle every
// member, and, under one option, a default case on a switch that already handles every member.
//
//	valid:   declare const day: 'a' | 'b'; switch (day) { case 'a': break; case 'b': break; }
//	valid:   declare const day: 'a' | 'b'; switch (day) { case 'a': break; default: break; }
//	invalid: declare const day: 'a' | 'b'; switch (day) { case 'a': break; }
//	invalid: declare const value: number; switch (value) { case 0: break; }   // requireDefaultForNonUnion
//
// A switch over `'a' | 'b'` that handles only `'a'` compiles and then falls through at runtime, and
// the compiler will not tell you when a third member is added to the union later. This rule is the
// thing that does tell you, which is why it is the one type-aware rule in this family whose value
// grows with the codebase rather than being constant per site.
//
// # This is wiring plus vendoring, not a re-implementation
//
// oxc declares this rule as `SwitchExhaustivenessCheck(tsgolint)` and carries no algorithm and no
// corpus, so the behavior oxlint exhibits IS tsgolint's: the release binary shells out to a
// `tsgolint` executable and refuses to run the rule when that binary is absent, which is what it
// does on this machine. That refusal is itself the proof, so oxlint cannot serve as ground truth
// here and tsgolint's own test file is the corpus instead.
//
// Like `no-array-delete` and unlike `await-thenable`, this rule was NOT already vendored. It was
// fetched from tsgolint's tree and placed at
// `internal/upstream/tsgolint/rules/switch_exhaustiveness_check`, byte-identical below the import
// block, which is the only edit: `github.com/microsoft/typescript-go/shim/...` becomes
// `github.com/microsoft/TypeScript/tsc/shim/...` and
// `github.com/typescript-eslint/tsgolint/internal/...` becomes this tree's vendor path. Every
// utility it calls was already here — `TypeRecurser`, `GetConstrainedTypeAtLocation`,
// `UnionTypeParts`, `IntersectionTypeParts`, `IsTypeFlagSet`, `Some`, `Every`, `Ref` — and the
// package compiles with no other change.
//
// # What decides a finding
//
// The anchor is `KindSwitchStatement`, one listener, and every case runs all three checks against
// one shared `SwitchMetadata` computed once. The metadata is three facts about the switch:
//
// MISSING LITERAL BRANCHES. The discriminant's type is resolved through
// `GetConstrainedTypeAtLocation` (so a type parameter walks to its base constraint) and then
// recursed with `TypeRecurser`. A recursed member counts as missing when it is literal-like and is
// not pointer-identical to any case expression's type. Literal-like means the flags
// `Literal | Undefined | Null | UniqueESSymbol`, which is why `case undefined` and `case null` are
// real branches here and why an enum member is one too.
//
// CONTAINS A NON-LITERAL TYPE. True when SOME union member has EVERY intersection member
// non-literal-like. The nesting is the part a reader gets wrong: it is some-over-union of
// every-over-intersection, so `'foo' & { bar: 1 }` contains a non-literal type because the
// intersection is not wholly literal, while `'foo' | number` contains one because of the `number`
// arm. This is what makes a default case never superfluous on a switch that admits `string`.
//
// THE DEFAULT CASE, or nil when there is none.
//
// From those, three independent checks, and they can BOTH fire on one switch: four of upstream's
// invalid cases report twice on the same statement, once for a missing branch and once for the
// missing default. A fixture asserting one id per input would have passed while dropping half the
// findings, which is why the fixtures here assert an ordered id list rather than a set.
//
// # The undefined special case, which no amount of reading the message text would suggest
//
// `missing`, `optional` and `undefined` are three different runtime type objects that all carry
// `TypeFlagsUndefined`. So once ANY case expression's type carries that flag, EVERY undefined-ish
// member of the discriminant is treated as covered, rather than matching them pairwise. Without it,
// `switch (x)` over `string | undefined` with `case undefined` would report the optional-flavored
// undefined as still missing.
//
// # The option surface is THREE live options, and the inventory says zero
//
// `rule-inventory.json` carried `"options": "no"` for this rule. That is wrong, and it is the
// eighth inventory error found in this lane and the most consequential, because each of these
// options flips a whole class of input rather than tuning an edge:
//
//	allowDefaultCaseForExhaustiveSwitch  default TRUE   false turns dangerousDefaultCase ON
//	considerDefaultExhaustiveForUnions   default FALSE  true makes any default case satisfy the union
//	requireDefaultForNonUnion            default FALSE  true reports a non-union switch with no default
//
// Sixty-eight of tsgolint's own hundred and six cases set at least one of them, and the same source
// text flips between reporting and silent depending only on which is set. The entry is corrected in
// this commit. The parity guard reads only rule presence from that file and never the options
// column, so the wrong value was inert rather than enforced — worth saying, because "the guard is
// green" was not evidence the column was right.
//
// # And a FOURTH option that exists in the struct and is never read
//
// `SwitchExhaustivenessCheckOptions` also declares `DefaultCaseCommentPattern *string`, and
// tsgolint's rule body never mentions it: one occurrence in the file, the field declaration itself.
// The feature — a `// no default` style comment standing in for a real default clause — is
// implemented in `@typescript-eslint` and unimplemented here.
//
// Upstream knows. All SIX of its `Skip: true` cases are exactly the cases that would need it, each
// marked `TODO(port): add support for DefaultCaseCommentPattern`, so upstream does not run them
// either. Those six are excluded here for the same reason and named individually in the test file,
// rather than dropped silently.
//
// The trap is that the field is settable. A config writing `defaultCaseCommentPattern` binds
// cleanly through `encoding/json`, produces no error, and does nothing at all. That is a silent
// no-op reaching a user, so `TestSwitchExhaustivenessCheckIgnoresTheCommentPattern` pins it as a
// measured fact: the same source reports identically with the pattern set and unset. If a later
// tsgolint sync implements it, that test fails and tells the next reader the option came alive.
//
// # There are no fixes and no suggestions, and the brief expected some
//
// This rule is the one in the family that would ship a repair which WRITES NEW CODE — adding a
// missing `case` clause, not deleting or rewriting an existing one. tsgolint ships neither.
// `buildAddMissingCasesMessage` is declared and never called, and every one of the fifty-two
// expected suggestion outputs in upstream's corpus is COMMENTED OUT under
// `TODO(port): add support for suggestions`. `checkSwitchNoUnionDefaultCase` carries its own
// `// TODO(port): missing suggestion` at the site.
//
// So `addMissingCases` is an unreachable message id here, and there is nothing for `ruletest` to
// apply and nothing needing a hand-rolled suggestion applier. That is a real behavioral gap against
// `@typescript-eslint`, and it is upstream's gap, reproduced rather than improved on.
//
// # The message text is also deliberately degraded upstream, and that is visible to a user
//
// `buildSwitchIsNotExhaustiveMessage` takes a `missingBranches` argument and THROWS IT AWAY: the
// body is `fmt.Sprintf("Switch is not exhaustive")` with the interpolating half of the format
// string commented out beside it. Every call site computes or passes a value that never appears.
// `@typescript-eslint` renders "Switch is not exhaustive. Cases not matched: 'b' | 'c'", which is
// most of what makes the diagnostic actionable, and we render the bare sentence.
//
// Reproduced rather than repaired, because oxlint runs tsgolint and the differential harness
// compares against oxlint, so restoring the list would read as a difference the harness can see.
// It is the single most user-visible drift in this family so far and the next sync should check
// whether upstream has closed it.
//
// # Where the two references disagree, counted rather than assumed
//
// Both register exactly ONE listener, on the switch statement, so there is no missing-arm drift of
// the kind `await-thenable` had — counted on both sides rather than assumed. Both carry the same
// three message ids and the same four schema keys. The disagreements are all in what is
// IMPLEMENTED behind them, and all three were measured by driving `@typescript-eslint`'s Linter API
// over a real program rather than by reading its source:
//
//	MESSAGE TEXT.  On `'a' | 'b' | 'c'` covering only 'a', it renders
//	               `Switch is not exhaustive. Cases not matched: "b" | "c"`. We render the bare
//	               sentence, because tsgolint's builder takes the list and discards it.
//
//	SUGGESTIONS.   The same input carries one SUGGESTION and no fix there. We carry neither.
//
//	COMMENT PATTERN. `// no default` under `requireDefaultForNonUnion` produces an EMPTY diagnostic
//	               list there, because its default pattern is `/^no default$/i`. We report.
//
// The last one is the sharpest for a user: a codebase that adopted the `// no default` convention
// under typescript-eslint lights up under oxlint. All three are reproduced rather than repaired,
// because oxlint runs tsgolint and the differential harness compares against oxlint, so closing any
// of them would read as a difference the harness can see. They are gaps rather than contradictions
// — no input makes the two report a DIFFERENT id, only inputs where one reports and the other is
// silent, plus every input where the rendered text differs.
//
// # The checker, and why the guard does not live here
//
// The listener reads `ctx.TypeChecker` unconditionally, so this needs the checker and its fixtures
// use `RunTyped`. The standing advice to write `if ctx.TypeChecker == nil { return }` at the top of
// a listener cannot be followed in this file: the listener is upstream's, and editing it is what
// would turn a re-sync into a merge.
//
// The guard is one level up. `upstream.Adapt` sets `NeedsTypeChecker` on every rule it wraps
// unconditionally, so the nil case is unreachable through registration, and a test in this package
// pins that declaration. That direction matters here more than usual: under the untyped harness
// this rule does not panic, it goes silent, and silence makes every clean fixture pass having
// proven nothing — and forty-nine of this rule's ninety-seven cases are clean ones.
//
// Reaching the checker only through a vendored file in another package also means the registry's
// per-file textual guard, which looks for a `.TypeChecker` selector in the rule's own file, will
// report this as over-declared. That message is wrong and the declaration is right.
//
// # Cost
//
// `KindSwitchStatement` is an uncommon anchor, but unlike the rest of this family the listener has
// no cheap syntactic exit: it consults the checker for the discriminant and for every case
// expression on every switch it sees. A switch with N cases costs N+1 type resolutions plus a
// recursion over the discriminant's union. That is the honest price of the only question this rule
// can ask.
var SwitchExhaustivenessCheck = adaptSwitchExhaustivenessCheck()

// adaptSwitchExhaustivenessCheck wires the vendored rule, panicking at startup if it cannot be
// adapted.
//
// `MustAdapt` rather than `Adapt` because a rule that cannot be adapted is a build-time mistake:
// the registry is assembled at process start, so failing there stops the tool immediately instead
// of leaving a rule silently absent from a run that otherwise looks clean.
func adaptSwitchExhaustivenessCheck() rule.Rule {
	return upstream.MustAdapt(switch_exhaustiveness_check.SwitchExhaustivenessCheckRule)
}

// SwitchExhaustivenessCheckOptions is the configuration surface, re-exported from the vendored rule
// so the registration below and a config reader name one type rather than two.
//
// It is upstream's struct rather than a translation of it, which is what makes
// `rule.DecodeOptionsInto` sufficient here where `no-this-alias` needed a hand-written decoder:
// nothing is inverted and nothing is renamed, so `encoding/json`'s case-insensitive field matching
// binds `allowDefaultCaseForExhaustiveSwitch` onto `AllowDefaultCaseForExhaustiveSwitch` directly.
// Measured with a probe rather than assumed, because that matching is a property of the standard
// library rather than of anything declared in this tree.
//
// The pointer fields are load-bearing and must not be flattened to plain bools. Two of the three
// live options default to a value that is not the zero value — `allowDefaultCaseForExhaustiveSwitch`
// defaults to TRUE — so a `bool` field could not tell "the user wrote false" from "the user wrote
// nothing", and the rule's own defaulting block reads exactly that distinction.
type SwitchExhaustivenessCheckOptions = switch_exhaustiveness_check.SwitchExhaustivenessCheckOptions
