# #6d8y0pf: inliner allocation order

Base: cohere `839b0cdf364baa36372fa893a4edeb54410524ca`.

React at the vendored corpus revision `bd6ea412c6732b3b946a2827fcaac3a1c8f2e863`,
`compiler/packages/babel-plugin-react-compiler/src/Inference/InlineImmediatelyInvokedFunctionExpressions.ts:219-222`,
rewrites returns while walking the nested body's JavaScript Map in insertion order.
Cohere's corresponding order is `nested.Blocks`, the ordered slice already used to copy
blocks and instructions. `InlineRemap.BlockOrder` retains its copied parent ids.
Sorting block ids would impose a different order; the rewrite must use the saved slice.
`singleReturnExit` retains its map range because it only counts returns and throws.

The regression lowers each vendored memo fixture once, constructs and erases memoization,
then clones the same prepass graph 128 times and uses IncludingMemoCallbacks. It compares
raw instruction/lvalue ids and the range exporter bytes before later normalization can
hide fresh allocation differences. It requires nonempty output, blocks, splices, and the
presence and inlining of all five cases found to differ on the base code:

- preserve-memo-validation-error.useMemo-infer-less-specific-conditional-access.ts#0
- preserve-memo-validation-error.useMemo-infer-less-specific-conditional-value-block.ts#0
- preserve-memo-validation-prune-nonescaping-useMemo-mult-returns-primitive.ts#0
- preserve-memo-validation-prune-nonescaping-useMemo-mult-returns.ts#0
- preserve-memo-validation-useMemo-conditional-access-own-scope.ts#0

## Commands and controls

All Go commands ran inside cohere, with `/workspace/adamic-tools/env.sh` sourced:
`go version`: go1.27.1 linux/amd64; `which go`: /workspace/adamic-tools/go/bin/go.
Let `P=./internal/lint/ecmascript/high_level_intermediate_representation` below.

- `go test -v $P -run '^TestInlineMemoCallbacksRepeatIdentically$'` on unchanged
  production code: exit 1, 120 fixtures, 136 functions, 79 splices; exactly 5 functions
  failed byte equality. The actual failing runs were:

```text
inline_order_test.go:54: run 11 differs from run 0 (3982 bytes, 1 splices)
inline_order_test.go:54: run 5 differs from run 0 (3812 bytes, 1 splices)
inline_order_test.go:54: run 1 differs from run 0 (1797 bytes, 1 splices)
inline_order_test.go:54: run 10 differs from run 0 (2186 bytes, 1 splices)
inline_order_test.go:54: run 1 differs from run 0 (2256 bytes, 1 splices)
```

- The same command after the fix: exit 0, all 136 functions byte-identical across
  128 runs each (17,408 runs); all five required cases inline and pass.
- Restoring only `range remap.Blocks` in the fixed return rewrite, then the same
  command: exit 1, the same 5 functions failed. Mutant removed afterwards.
- Planting `out.Reset()` immediately before the nonempty-output assertion, then
  the same command: exit 1, all 136 function cases rejected empty output.
  Plant removed afterwards.
- `go test -json $P`: exit 0; 1,083 passing test events including subtests,
  47 skipped events, 0 failing events. The caller census initially rejected the
  new test caller; it now records that this guard intentionally reads ids before
  normalization and computes no reactive scopes.
- `gofmt -l internal/lint/ecmascript/high_level_intermediate_representation`:
  exit 0, 0 listed files. `go vet $P`: exit 0, 0 diagnostic bytes.
- `go test -v $P -run '^TestExport(Graphs|Ranges)ForAdamic$' -args
  -export-graphs <directory> -export-ranges <directory>` on both old and fixed code:
  2 passing exporters each time. Graphs: 355 fixtures, 695 functions. Ranges:
  355 fixtures, 1,010 functions, 407 closure answers. Comparing corresponding
  files byte for byte: 0/355 graph files and 0/355 range files changed in these runs.
  This is a comparison of the captured exports, not a claim about all possible old runs.

## Map range audit

`rg -n 'for .*range '`, followed by a Go AST/type inventory using `go/packages`
(`NeedSyntax|NeedTypes|NeedTypesInfo`), found 50 production map ranges:
48 in high_level_intermediate_representation, 0 in static_single_assignment,
and 2 in mutation_aliasing. Every map loop body was inspected. The references below
are base-code line numbers. Identifiers, Instructions, Functions, Blocks and phi operands
in `inline_remap.go` are slices; neither InlineRemap.Identifiers nor InlineRemap.Instructions
is ranged in production. The sole inliner id-allocating map range is the one fixed.

| File and lines | Observable ordering |
| --- | --- |
| inline_iife.go:379 | Allocates ids; fixed to nested block order. |
| inline_iife.go:433 | Counts only; addition commutes. |
| align_method_calls.go:123,134,169,172 | Keyed updates; member appends at 169 are sorted at 172 before use. |
| align_scopes.go:335 | Copies keyed range entries. |
| clone.go:79 | Copies the block lookup table; canonical Blocks slice copied first. Object allocations mint no IR ids. |
| context_identifiers.go:122 | Produces a membership set. |
| dependencies.go:1096,1158,1428,1545 | Copies keyed entries; overriding happens between loops, never between keys within a loop. |
| dependencies.go:1593,1606 | Membership predicate; break means existence of a match, not selection of its value. |
| disjoint.go:283 | Members sorted before grouping or output. |
| drop_manual_memoization.go:225 | Copies keyed dependencies. |
| graph.go:129,146 | Maximum bound; keyed deletion. |
| hoistable.go:199,245,390,613,633,635,745,761,764,809 | Existential test, sorted node indices, set copies/intersections/unions, sorted successor lists, set equality. |
| invoked_functions.go:70,74,181 | Reachability fixed point; only monotonic set insertion. |
| lower.go:465 | Writes each capture to its existing Context slice position; no append or id allocation. |
| memoization_graph.go:139,211,221 | Independent flag resets; collected keys sorted before traversal. |
| merge_scopes.go:512,514 | Scope lists sorted; outer position map read later via ordered positions. |
| postdominator.go:172,279,354 | Internal predecessor lists feed a commutative dominator intersection fixed point; existential test; frontier membership set. No ids or emitted lists. |
| preserve_manual_memoization.go:466 | Produces a flattened membership set. |
| preserve_manual_memoization.go:561 | Emits findings, but supported upstream marker graphs have at most one open memo block. See limitation below. |
| reactive.go:834,903 | Independent existential tests; children traversal marks a subtree set, while emitted frontier order comes from Function.Blocks. |
| reactive_build.go:780 | Copies keyed emitted flags. |
| scope_terminals.go:425,433 | Independent keyed bounds updates. |
| mutation_aliasing/ranges.go:571 | Invalid identifiers sorted before return. |
| mutation_aliasing/ranges.go:867 | Freezing identity members and source reachability; final frozen membership commutes, no ids or ordered output. |

One audit limitation: `compareInferredDependencies` can iterate several source dependency
lists for *nested open memo marker graphs*, which React rejects by its non-nesting
invariant. Cohere's fallback permits such graphs, so their finding order could be
observable. This change leaves that unsupported-input diagnostic behavior alone;
it is not a second inliner allocation bug and upstream provides no nested-block order
to reproduce. No claim of determinism is made for that fallback.

The supplied Adamic checkout is `98008bbba2881939e07a9d74031994102a3bbe36` and lacks
`passes/unit-3/stopped.md`, `TestGoInlineOracleOrderGap`, and
`stage1/cohere/mutation_aliasing/testdata/react_ranges.txt.gz`; `git cat-file -t a079cbe7`
also fails there. Consequently the five required cases above are identified by the
old-code repetition run, not confirmed against the unavailable Adamic 1,465-function
export. Their 128-run equality is checked directly by the new test. No Adamic edits
or tests, whole-module tests, main push, or landing were performed.
