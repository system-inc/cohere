# union-write verification (#agg5nsf)

Baseline: cohere `f657d46b71f48f18069789b6e7a9ee786134727b`, TypeScript submodule
`d92d9bfee114c80be2c375d72edae966176e3a4f`, Go 1.27.1, Node 24.19.0,
TypeScript JavaScript compiler 6.0.3. Commands ran inside cohere after the
required Adamic bootstrap and environment file were sourced.

## Behavior and limits

The rule now checks for-of and for-in targets, array and object assignment
patterns, and builtin Object.assign source properties. Source projection covers
matching tuple elements, matching properties, nested patterns, defaults (also
nested), and general iterables. Existing flow narrowing, nested receivers,
computed slots, write types and union constraints are preserved. Object.assign
checks later sources and each member of union-typed sources, skips absent optional
source properties while preserving real undefined, and names the
written slot in its message.

Direct array rest and object rest targets are deliberately left out. The compiler
assigned-type helper combines all tuple element types for array rest and does
not project object rest. Dedicated remainder projections are needed before those
targets can be checked precisely. This is an implementation limit, not a claim
that rest writes are safe. No base/structure tier settings were changed. The
existing cohere:adamic set already enables the rule. No rule was added/renamed,
no top-level package was added, and no ESLint core gap was changed.

For-in writes string; its compatible/overlapping neighbours therefore use
string, rather than the numeric/function source cases used for the other forms.
Delete remains quiet. Its separate fixture prints 0 in Node; removing the call
prints 1, and replacing delete with a string write is reported.

## Old-code run

The test/fixture changes were retained while replacing only union_write.go with:

```sh
git show f657d46b71f48f18069789b6e7a9ee786134727b:internal/lint/rules/adamic/union_write.go > internal/lint/rules/adamic/union_write.go
go test ./internal/lint/rules/adamic -run '^TestUnionWrite' -json
```

The old rule failed these **22** refused subtests (zero TypeScript diagnostics
in each). The remaining **23** refused subtests passed: the original 21 probes
plus array-default and object-default. Those two new probes preserve already
reported default writes; source-sensitive defaults expose the actual gap.

- `TestUnionWriteRefused/array-default-source`
- `TestUnionWriteRefused/array-matching-element`
- `TestUnionWriteRefused/array-pattern`
- `TestUnionWriteRefused/assign-generic`
- `TestUnionWriteRefused/assign-multiple-sources`
- `TestUnionWriteRefused/assign-nested-target`
- `TestUnionWriteRefused/assign-other-member`
- `TestUnionWriteRefused/assign-union-source`
- `TestUnionWriteRefused/for-in-target`
- `TestUnionWriteRefused/for-of-element`
- `TestUnionWriteRefused/for-of-iterable`
- `TestUnionWriteRefused/for-of-target`
- `TestUnionWriteRefused/loop-nested-target`
- `TestUnionWriteRefused/nested-array-default-source`
- `TestUnionWriteRefused/nested-array-pattern`
- `TestUnionWriteRefused/nested-object-default-source`
- `TestUnionWriteRefused/nested-object-pattern`
- `TestUnionWriteRefused/object-assign`
- `TestUnionWriteRefused/object-default-source`
- `TestUnionWriteRefused/object-matching-property`
- `TestUnionWriteRefused/object-pattern`
- `TestUnionWriteRefused/pattern-generic`

## Restored implementation

| Command / check | Result |
| --- | --- |
| `go test ./internal/lint/rules/adamic -json` | 48 top-level tests + 560 subtests passed; 0 failures |
| UnionWrite tests within that run | 4 top-level tests + 142 subtests passed |
| Refused fixture checks | 45 fixtures, 0 tsgo diagnostics each, exactly one report with expected span and slot message each |
| Node probe checks | 45 TypeErrors; removing `probe()` makes all 45 controls exit successfully |
| Legal neighbours | 52 clean sources; all 52 string-replacement plants reported |
| Delete fixture | 0 reports, Node prints 0; removal control prints 1; string-write plant reported |
| `go test ./internal/testpolicy ./internal/lint/registry ./internal/guard -json` | 3 packages, 59 top-level tests + 26,124 subtests passed; 0 failures |
| testpolicy / registry / guard breakdown | 4 / 36 / 19 top-level tests; 0 / 26,110 / 14 subtests |
| `go vet ./internal/lint/rules/adamic github.com/microsoft/TypeScript/tsc/shim/checker` | 2 packages, 0 diagnostics |
| `gofmt -l internal/lint/rules/adamic/union_write.go internal/lint/rules/adamic/union_write_test.go TypeScript-shim/checker/shim.go` | 3 files checked, 0 printed |
| `go build ./command/cohere` | exit 0 (also passed before implementation) |
| `go run ./internal/types/tools/generate_shims` | exit 0; exposes assigned-source and default-combiner methods, no field layout change |
| `go run ./internal/settingsschema/tools/generate` | exit 0; 0 schema file changes |

Strict JavaScript tsc command (exit 0, **46 files**, **0 diagnostics**):

```sh
npm exec --yes --package=typescript@6.0.3 -- tsc --ignoreConfig --strict --noEmit --target es2024 --module nodenext internal/lint/rules/adamic/testdata/union-write/*.ts internal/lint/rules/adamic/testdata/union-write-legal/*.ts
```

The submodule's tsc.js is a launcher for an unbuilt native executable, so this
check used the same JavaScript tsc version named by the existing probes.

## Mutation controls

Each mutation replaced only union_write.go, ran
`go test ./internal/lint/rules/adamic -run '^TestUnionWrite' -json`, and then
restored the original source. **28/28 build-valid mutants were caught**. The old
rule was also tested as a separate control. Representative actual failing test
names follow; legal-neighbour tests include both the clean case and its plant.

The compound-RHS mutant initially survived. The existing compound.ts probe was
strengthened so RHS 1 fits all members but the computed result corrupts a
literal-1 slot. That revised probe passes strict tsc, fails in Node, and catches
the RHS-only mutant. The final old-rule control was rerun after that revision.

| Mutant | Failing test |
| --- | --- |
| `drop-member-check` | `TestUnionWriteRefused/property` |
| `check-only-first-member` | `TestUnionWriteLegalNeighbours/array_pattern_defined` |
| `drop-union-guard` | `TestUnionWriteLegalNeighbours/generic_narrowing` |
| `drop-generic-constraint` | `TestUnionWriteRefused/assign-generic` |
| `read-instead-of-write-slot` | `TestUnionWriteLegalNeighbours/setter_write_type` |
| `drop-index-signature` | `TestUnionWriteRefused/array` |
| `check-only-first-key` | `TestUnionWriteRefused/union-key` |
| `drop-prefix` | `TestUnionWriteRefused/prefix` |
| `drop-postfix` | `TestUnionWriteRefused/increment` |
| `drop-assignment-guard` | `TestUnionWriteLegalNeighbours/array_pattern_narrowed` |
| `compound-uses-rhs` | `TestUnionWriteRefused/compound` |
| `logical-uses-result` | `TestUnionWriteLegalNeighbours/logical_fits` |
| `drop-object-assign` | `TestUnionWriteRefused/object-assign` |
| `drop-builtin-guard` | `TestUnionWriteLegalNeighbours/assign_shadow` |
| `drop-property-name-match` | `TestUnionWriteLegalNeighbours/assign_unrelated` |
| `drop-later-sources` | `TestUnionWriteRefused/assign-multiple-sources` |
| `drop-source-union` | `TestUnionWriteRefused/assign-union-source` |
| `defaults-use-rhs-only` | `TestUnionWriteLegalNeighbours/array_default` |
| `drop-missing-default` | `TestUnionWriteLegalNeighbours/object_default_absent` |
| `drop-for-of` | `TestUnionWriteRefused/for-of-target` |
| `drop-for-in` | `TestUnionWriteRefused/for-in-target` |
| `drop-array-pattern` | `TestUnionWriteRefused/array-pattern` |
| `drop-object-pattern` | `TestUnionWriteRefused/object-pattern` |
| `drop-element-listener` | `TestUnionWriteRefused/for-of-element` |
| `drop-literal-slot` | `TestUnionWriteLegalNeighbours/array_default` |
| `drop-literal-key` | `TestUnionWriteRefused/computed` |
| `drop-default-combiner` | `TestUnionWriteLegalNeighbours/array_default_absent` |
| `optional-source-reads-missing` | `TestUnionWriteLegalNeighbours/assign_optional_source` |

The optional-source mutant used a Go overlay instead of changing the working
source, so concurrent docs capture continued reading the restored implementation:

```sh
go test -overlay=/tmp/union-optional-overlay.json ./internal/lint/rules/adamic -run '^TestUnionWriteLegalNeighbours$/^assign_optional_source$' -json
```

That overlay replaced `getNonMissingTypeOfSymbol` with `getTypeOfSymbol` for
Object.assign source properties. The clean optional-source case failed, while
the restored implementation kept it quiet and reported its string-property plant.

## Documentation capture

`go run ./internal/docsdata/tools/generate -capture` initially failed in Tailwind
fixture loading. The actual snapshot exists, but setup supplies
`GOFLAGS='-buildvcs=false -trimpath'`; Tailwind's runtime.Caller-based root helper
then returns an import-relative path rather than a source path. A focused
`go test ./internal/lint/rules/tailwind/vendored -json` reproduced **1 failing
top-level test**, TestTheSnapshotIsWholeAndTheVersionItSays, on that wrong path.
No Tailwind code, snapshot, or global environment was modified.

`GOFLAGS= go run ./internal/docsdata/tools/generate -capture` passed: **43,312
records captured**, **15,413 distinct option-bearing cases counted**. It generated
one unrelated Nexus example change, which was restored; **0 docs/data files**
changed for union-write. The flag override applied only to this command. Required
repo guards, vet and the final build all passed with setup's original GOFLAGS.
