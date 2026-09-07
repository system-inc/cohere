# Manual-memoization parity investigation

`#89qdg8e` remains open. Acceptance is parity with ESLint, or additional findings supported by
evidence, not an arbitrary zero. This report records repaired mechanisms and the unresolved
population. No remaining finding is certified as a true positive here.

The namespace-ref correction is committed as `35af1d2`, and the independent capture-identity
correction as `396605e`, each with its regression fixtures. After both commits,
`go test ./internal/lint/... -count=1` and `go build ./...` pass. The state-result effect
intervention described below is private probe code only; it is not part of either repair.

## Controlled population differential

Independently built archives of `4c76e16` and `e1f9aa3` were run on the same private copied ahra
source/configuration snapshot and cloned dependencies with `--lint --no-fix`. Both binaries expose
the same 450-rule inventory. The copied program contains 10,114 files, 3,541 owned, versus 10,116
and 3,541 in the supplied historical log. The two program-file omissions remain unidentified.
Nevertheless, normalized rule findings match both historical logs exactly, including multiplicity.

| Transition | Before | After | Added | Removed | Unchanged |
| --- | ---: | ---: | ---: | ---: | ---: |
| Reverse-postorder correction | 287 | 292 | 19 | 14 | 273 |
| Namespace-ref stability correction | 292 | 217 | 0 | 75 | 217 |
| Nested capture-identity correction | 217 | 217 | 0 | 0 | 217 |

Keys are path, line, column, and message identifier. Counts preserve repeated anchors; they are
not unique locations. Unique keys move 221 to 225 to 183 across these three versions.
The current 217 occurrences comprise 105 DependencyMutable and 112 ValueUnmemoized findings.
An independent `git archive 396605e` build reproduces those exact 217 normalized occurrences on
the same snapshot, including the committed configuration resolver correction in `182d03a`.
This measurement does not depend on uncommitted shared-checkout changes.

## Confirmed mechanism: namespace ref calls lost stability

HIR `reactive.go` recognized a hook invocation in both `CallExpression` and `MethodCall` form,
but `useRefResultValues` seeded stable ref identities only from `CallExpression`. Consequently,
`useRef(...)` was recognized while `React.useRef(...)` was not. Both receive a RefObject type,
but the additional origin check rejected the namespace form.

The fix recognizes `MethodCall.Property` as well as `CallExpression.Callee` when seeding ref
origins. It changes reactivity inference, not the validator's reporting predicates. Existing local
copy propagation remains; custom-hook results and conditional choices are not newly made stable.
The broader name-based hook-recognition limitations are unchanged.

`SidebarDragContext.tsx` moves from 24 findings to zero. Babel compiles its actual
`SidebarDragProvider` successfully with 24 memo slots and 14 memo blocks. Declaration anchors
had concealed that these findings arose from later memoized callbacks capturing stable refs;
absence of a memo next to the anchor was not absence of a memo in the function.

## Reference instrument and its limits

The oracle resolves `@babel/core` 7.29.7 and `babel-plugin-react-compiler` 1.0.0 directly from
the copied dependency tree. It disables external Babel configuration, parses TypeScript/JSX,
enables `environment.validatePreserveExistingMemoizationGuarantees`, and records logger events,
transformed code, and thrown exceptions separately.

Controls execute before the population run: a memo returning `[props.value]` with `[]` produces
exactly one CompileError naming lost preservation; supplying `[props.value]` produces no
CompileError and a CompileSuccess. The namespace-import spelling of the bad control also
produces exactly one preservation error.

Across all 83 files carrying the original 292 findings, the direct compiler produced 70
CompileSuccess events and 27 CompileError events, with zero thrown parser/transform exceptions.
61 files had no CompileError. The 27 errors comprise 21 Todo, 1 Hooks, and 5 Suppression events;
none is a manual-memoization-preservation error. File/event counts differ because a file can
contain several functions or errors.

Mapping each Cohere finding's line to the compiler's enclosing function event yields:

| Covering function event | Original 292 | Remaining 217 |
| --- | ---: | ---: |
| CompileSuccess | 213 | 151 |
| CompileError:Todo | 71 | 58 |
| CompileError:Hooks | 2 | 2 |
| CompileError:Suppression | 6 | 6 |

All anchors have a covering event in this run. Successful compilation with preservation validation
is evidence against the corresponding Cohere preservation verdict. A Todo, Hooks, or Suppression
bailout is not: validation may never have been reached. Those 66 remaining occurrences must not
be relabeled true or false merely because the public ESLint rule reports zero.

## Tests and additional unresolved signal

`reactive_namespace_refs_test.go` adds five ref-reactivity cases, five pipeline cases, and three
stable tuple-position cases. They check
named/namespace forms, copied refs, custom-hook results, conditional choices, a reporting property-call
dependency mismatch, and a clean supplied dependency. Pipeline fixtures require a real checker,
visited functions, and actual memo markers.

An overlay replacing only `reactive.go` with its original `e1f9aa3` content makes exactly three
cases fail: namespace ref stability, copied namespace ref stability, and namespace callback
preservation. The fixed implementation passes those nine initial cases and the four added controls.
The complete `./internal/lint/...`
suite passed for the implementation change; both HIR and React packages were rerun successfully
after finalizing the added fixtures. No shared file was reverted to run the negative control.

`reactive_capture_identity_test.go` separately pins propagation through one and two nested closures,
including stable captures and nested parameters. The original walk reused the parent's identifier
set in the child's independent identifier table. The correction pairs `FunctionExpression.Captures`
with the child's `Context` instead. Both cases fail with the original implementation and pass with
the correction; the complete HIR and React package suites pass. The normalized population is
byte-identical at 217, not merely equal in count. This fixes a graph-identity defect, not the
remaining population. `ObjectMethod` does not retain the corresponding capture pairing and remains
a missing-information case rather than an excuse to reuse unrelated parent identifiers.

A separate control exposed an unresolved opposite-direction discrepancy: the typed HIR harness
reported zero for `React.useMemo(() => [props.value], [])` assigned to a local and rendered in JSX,
while the direct compiler reports one preservation error for that same namespace-import source.
It was not used as a passing positive assertion. The retained positive assertion uses the existing
property-call mismatch fixture instead. Zero population findings would not by itself settle parity.

## Confirmed reproduction: method call after state initialization

An independently minimized SeeClusterCard reproduction differs only between
`newName.trim().length > 0` and `newName.length > 0` in a derived expression. A later memoized
callback captures the stable setter from a different state declaration. The direct compiler
successfully compiles both exact sources with preservation validation enabled, caching that
callback once in both; the failing and passing dependency controls execute first.

A typed HIR probe using namespace imports reproduces one finding versus zero. Before scope
assignment, the method variant widens `newName` from its definition at 21 through 45; the property
variant leaves it at `[21,22)`. The resulting wide scope absorbs the intervening `setKept`
declaration. Scope-output propagation then promotes that initially stable setter, introducing the
callback dependency that triggers the report. The setter itself is not the initial cause.

The effect table has no `useState` signature and creates its result as mutable. React's compiler
signature returns Frozen, reads the callee, and freezes arguments. A private controlled intervention
changing only the useState result's Create effect from Mutable to Frozen keeps `newName` at
`[21,22)` and makes both halves silent. This is evidence for the missing hook-result effect, not a
shipped repair or a population measurement. No shared effects code was changed. A production fix
needed hook-origin resolution, paired regression fixtures, and a full differential; adding a
`trim` exemption or exempting setters in the validator would address the wrong layer.

The origin-safe state effect and paired fixtures are now implemented. `STATE_EFFECTS.md` records
the prerequisite and the attribution of all four changed corpus-count checks, including the raw
setter dependency that disappears during final pruning. Its population differential is separate
from these structural measurements; the 217 figure above still describes the preceding repair.
The committed state repair measures 177 on the same snapshot: 58 removed, 18 added and 159 unchanged.
Five additions are covered by successful reference compilation and remain false positives. See
`STATE_EFFECTS.md` for the reference verdict categories; the parent parity task stays open.

## Next trace

Continue with the 151 remaining occurrences in successfully compiled functions, retaining the 66
bailout-covered occurrences as a separate category. Resolve the missing-dependency control too.
Do not infer one cause from the most frequent remaining file or conflate declaration anchors with
the memo blocks that generated them. The architecture assessment identifies other integration gaps,
and the hook-result effect now has a controlled reproduction within this residue. Its population
reach is not yet measured.

Private source snapshots, binaries, oracle scripts, full compiler output, normalized deltas, and
test logs are under `/private/tmp/wisebeing-89qdg8e.KDVsxI/`. They are investigation artifacts,
not files to publish with this repository. The snapshot limitations are also recorded there.
