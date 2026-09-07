# Control flow, SSA, and mutation analysis

Assessment for `#g7e3a51`, gating `#1frcd6n`. Implementation inspected and tested at
`e1f9aa38f54b74f6f05ac5d6885b22ebf249e63a`; documentation-only commit `bb48734` landed during
the assessment. This is an inventory and recommendation, not a claim of complete compiler correctness.

## Decision

Do not commission a new CFG, SSA implementation, or wholesale mutation-and-aliasing port.
All already exist, have production callers, and have executable tests. Keep the two existing
graph representations: they serve different consumers and model different observable behavior.

Reframe `#1frcd6n` around a specific capability gap demanded by a named consumer. It is neither
an unstarted primitive nor a finished general interprocedural analysis. The historical generic-line
ratio does not establish current missing scope or the behavior obtainable by porting those lines.

## Existing facilities and consumers

Paths below are relative to `internal/`.

| Facility | Implementation | Production use |
| --- | --- | --- |
| Event-oriented CFG | `lint/ecmascript/control_flow_graph/`: 9 non-test Go files; basic blocks, reachability, dominators, forward/backward lattice solver | 9 importing files: consistent-return judgment, array-callback-return, no-unreachable-loop, no-useless-return, dead-store analysis, require-atomic-updates and its escape helper, rules-of-hooks, unused-export reachability |
| Value-oriented CFG and SSA | `lint/ecmascript/high_level_intermediate_representation/`: 52 non-test Go files; three-address instructions, lowering, SSA construction/verification/elimination | 8 importing files, all React rules: immutability, purity, refs, set-state-in-effect, set-state-in-render, static-components, no-deriving-state-in-effects, preserve-manual-memoization |
| Per-instruction effects | HIR `effects.go`, `InferAliasingEffects` | Mutable-range inference; rule-specific interpretation also exists in React immutability |
| Mutation/alias range propagation | HIR `ranges.go`, `InferMutableRanges` and `InferMutableRangesWithEffects` | Scope assignment and manual-memoization analysis |
| Union-find over mutable values | HIR `disjoint.go`, `FindDisjointMutableValuesWithRanges` | Mutable-value grouping and reactive-scope assignment |
| Symbol-based rename | `rename/plan.go`, `resolve.go` | Checker-symbol rename planning, collision checks, and explicit refusals; no HIR import |

Importing-file counts are not rule counts, execution counts, or coverage percentages.

The manual-memoization pipeline actually calls range inference, disjoint grouping, scope
construction, dependency collection, and validation in HIR `preserve_manual_memoization.go`.
`InferMutableRanges` calls `InferAliasingEffects`; these are not unused declarations.
HIR `cache.go` provides checker-backed, per-file cached lowering plus SSA. A separate cache view
handles manual-memoization erasure. Consumers must not mutate a shared graph to obtain their own
preferred pipeline state.

## Why retain two graphs

The event CFG stores caller-defined events and successor edges, not the value produced by every
subexpression. Its hooks serve path questions over reads, writes, and suspension points. Its
documented ESLint-style finally layout duplicates the body for normal and abrupt completion.

HIR explicitly lowers expression results and assigns value identities, then constructs SSA.
Consumers need argument/result relationships, phis, captures, and reactive-scope layout. The design
rationale is in `high_level_intermediate_representation.go`; the interfaces and callers support
keeping this distinction. Replacing either graph with the other would be a semantic migration
requiring differential evidence, not a deduplication of equivalent structs.

Neither a Try terminal nor a passing SSA verifier proves complete exception semantics. HIR lowering
does not emit instruction-level `MaybeThrow` terminals. Unsupported syntax becomes `UnsupportedNode`,
rather than a nil function, so consumers must explicitly decide what that permits them to conclude.

## Actual gaps

These are executable API declarations and inspected implementation boundaries:

- `EffectGapInterproceduralParameters`: no general substitution of a named local callee's
  parameter effects into its caller. Its test verifies conditional unknown-call mutation rather
  than precise mutation inferred from the callee body. Some callback/capture handling exists;
  that is not whole-program call-graph analysis.
- `EffectGapSignatureTable`: the known-call registry is partial. Unknown calls receive conservative
  effects, not evidence that a callee is read-only.
- `EffectGapTypeDirectedShapes`: lookup uses syntax names, including a small qualified receiver
  table, rather than general checker-directed receiver shapes. An unrelated same-named method can
  inherit a built-in signature, allowing errors in either direction.
- `RangeGapLoopCarriedInversion`: a loop-carried interval gap remains. An unset range is not proof
  of immutability.
- `DisjointGapPrimitiveCallResult`: allocation classification uses the partial name-directed
  registry. Mutable-value union-find is not an exact heap-alias oracle.
- `ReactiveGapMutation` and `ReactiveGapAliasing`: `InferReactive` still uses identifier-keyed
  reactivity without consuming mutable ranges or disjoint representatives. Those analyses exist
  and feed scope assignment, but their existence has not closed this separate integration gap.
- Reactive-tree conversion retains measured instruction-loss/duplication cases. A well-formed
  source HIR does not prove every downstream representation preserves it.

`EffectGaps()`, `RangeGaps()`, `DisjointGaps()`, and `ReactiveFunctionGaps()` have no non-test call
sites at this revision. They expose limitations for tests and readers, but do not automatically
make production consumers refuse affected inputs. These global lists also do not identify which
particular result is affected. A new safety-sensitive consumer needs a per-result unknown/refusal
contract rather than treating every returned table as a proof.

## What this does and does not buy

- Binding dead-store analysis already runs backward liveness on the event CFG. More precise dead
  stores through object aliases or across calls need heap/callee facts, not merely SSA.
- `require-atomic-updates` already models suspension and has rule-local escape checks over
  declarations, captures, and parameters. This does not establish a reusable analysis of every
  object mutation after an async boundary.
- Checking a callee against a read-only contract needs trustworthy callee effects and alias/escape
  facts. A partial registry and unknown-call fallback cannot prove that contract.
- Mutable-value equivalence classes do not prove rename safety. Binding identity, import/export
  handling, capture, and shadowing remain relevant. Preserve the rename planner's checker-symbol
  interface rather than substituting union-find.

The general infrastructure is real. Broader applications remain separate consumer designs, not
automatic consequences of matching React's diagnostic policy. Keep React compatibility and new
general-purpose guarantees distinct, with separate acceptance tests where they differ.

## Rerun measurements

All three complete package suites passed:

```sh
go test ./internal/lint/ecmascript/control_flow_graph ./internal/lint/ecmascript/high_level_intermediate_representation ./internal/rename -count=1
```

Selected corpus/gap tests additionally ran verbosely to confirm execution rather than skips:

| Test | Observed result |
| --- | --- |
| `TestSSAOverRealCodebase` | 400 files, 1,891 functions, 3,008 phis, 742 functions with phis, 5,368 named values, 79,137 uses, 0 SSA violations |
| `TestDisjointClassSizesAreNotAllSingletons` | 200-file sample, 345 functions, 13,020 members, 1,890 classes, 1,203 singleton classes, largest class 462 |
| `TestBuildReactiveFunctionCorpusConservation` | 680 converted; 6 loss cases with value terminals, 7 with scoped-loop values, 0 otherwise unexplained loss cases; 4 double emissions, 0 unmatched gotos, 0 non-implicit scope breaks |
| Parameter-inference and effect/range/disjoint gap tests | All pass, including the test asserting precise interprocedural parameter inference is absent |

These tests use bounded live-tree samples and their own typed harnesses, not one shared whole-project
compilation. Counts are observations, not fixed acceptance thresholds. Zero SSA violations proves
the checked invariants on those inputs, not semantic equivalence to JavaScript or React. Passing
gap tests can deliberately establish an absent capability.

## Recommended next work

1. Continue `#89qdg8e`'s finding-level trace. The traversal repair moved occurrences 287 to 292:
   19 added, 14 removed, and 273 unchanged. Subsequent namespace-ref recognition removes 75,
   leaving 217; a separate capture-identity correction leaves that population unchanged.
   A controlled state-result effect probe identifies another mechanism, not yet a production fix.
   No cause for the complete population has been established. See `MANUAL_MEMOIZATION.md`.
2. Scope `#1frcd6n` to a missing capability demanded by a named consumer. Candidate interfaces are
   receiver-aware signatures, local parameter-effect summaries, and per-result completeness
   reporting. Select from failing/clean paired probes, not this inventory alone.
3. Require controls for known/unknown callees, same-named unrelated methods, aliases, branches,
   loops, captures, and unsupported/exceptional paths relevant to the chosen capability. Prove
   the instrument fails before the repair; retain unchanged and negative cases afterward.
4. Measure consumer findings and runtime on identical inputs. Do not silently broaden all React
   rules' interpretation of effects to implement a new general-purpose rule.

## Source chronology

The sender reports the three named research documents lost; the Desktop directory is absent.
No recommendation depends on presumed contents. Current sources include `README.md`, the rule-port
standard, per-rule notes, and implementations/tests.

Some source headers describe earlier states: the HIR package header says there is no effect
inference, while `effects.go` and its callers implement it; `lower.go` says Optional terminals are
not produced, while `lower_optional.go` emits them. The README's absent CFG path was corrected
concurrently in `bb48734`. Prefer definitions, callers, and executed tests over present-tense claims
retained from an earlier stage of construction.
