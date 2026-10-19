# union-write evidence (#wcvkh0d)

`adamic/union-write` is separate from `invariant-mutable`: a value can exactly
match one member at a call, so there is no widening to report there. The unsafe
operation is the subsequent write through the union view. Only the cohere:adamic source settings are
changed; the structure/base tiers are left for the owner's consumer census.

## Fixtures and controls

There are 21 refused `.ts` files here. Each is accepted by strict TypeScript
6.0.3 and throws a TypeError on Node 24.19.0. `discriminant.ts` uses
`shape.side.toFixed`, and `optional.ts` uses `(item.label ?? 0).toFixed`, so these
are runtime failures rather than a wrong numeric result. The rule tests also
check zero tsgo diagnostics under the Adamic compiler options, one finding, its
exact target span, and a message naming the slot.

The 11 clean cases in `TestUnionWriteLegalNeighbours` cover narrowed receivers
(including nested and generic narrowing), overlapping and identical slots,
value-returning functions related to void-returning function types, defined
values, ordinary nonunion targets, logical assignments, setter write types, and
reads. Each clean case has a planted unsafe write that must report once.

`TestUnionWriteNodeProbes` executes every refused file and its control with
`node`. The control removes the `probe()` call; all 21 controls exit successfully.
Removing that call from a refused input makes the runtime assertion fail, so it
cannot pass just because Node is unavailable or every execution fails.

## Before and after

Baseline: origin/main `06af2e399c6c1fb93fd570b774027942747400b4`.

Commands ran from cohere after sourcing `/workspace/adamic-tools/env.sh`:

```sh
go build ./...
go test ./internal/lint/rules/adamic -run '^TestUnionWrite' -json
npm exec --yes --package typescript@6.0.3 -- tsc --ignoreConfig --strict --noUncheckedIndexedAccess --exactOptionalPropertyTypes --noImplicitReturns --noFallthroughCasesInSwitch --erasableSyntaxOnly --target es2024 --module nodenext --noEmit internal/lint/rules/adamic/testdata/union-write/*.ts
go test ./internal/lint/rules/adamic -json
```

- Baseline build: exit 0.
- Old engine, with the new test/fixture files and no union-write implementation:
  all 21 refused fixture subtests fail with
  `expected 1 findings [unionWrite], got 0 []`. All 21 Node subtests pass. The
  nine clean-case plants present at that run also fail to report. (Two further
  clean controls, for reads and generic narrowing, were added afterward.)
- Fixed engine: all 21 refused fixture subtests and 21 Node subtests pass.
- Strict tsc command: exit 0 for all 21 refused files.
- Full Adamic package: 458 tests/subtests pass, 0 fail, 0 skip. This includes
  all 11 legal neighbours with their plants and all 21 runtime controls.
- The literal-array-index fixture also failed before replacing exact-index
  lookup with applicable-index lookup: 1 refused subtest, 1 failure.
- The setter clean fixture failed before using the member's write type instead
  of its read type: 1 clean subtest, 1 false positive.


| Fixture | Old findings / required | New findings | Node / no-call control |
| --- | --- | --- | --- |
| `array.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `array-literal.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `compound.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `computed.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `discriminant.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `generic.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `generic-array.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `generic-key.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `generic-nested.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `increment.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `logical.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `nested.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `nested-element.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `optional.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `other-member.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `prefix.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `property.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `setter.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `symbol.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `tuple.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |
| `union-key.ts` | 0 / 1 (FAIL) | 1 (PASS) | TypeError / exit 0 |

## Mutants

Every mutant was removed after its run. The first 15 ran with:

```sh
go test ./internal/lint/rules/adamic -run '^TestUnionWrite(Refused|LegalNeighbours)$' -json
```

Each run had 30 leaf subtests (21 refused plus the nine clean cases then present).
The last two used Go source overlays and targeted the named clean subtests.

| Mutant | Failed / passed leaf subtests | One test catching it |
| --- | --- | --- |
| first-member-only | 20 / 10 | `TestUnionWriteLegalNeighbours/narrowed` |
| merged-member-slot | 27 / 3 | `TestUnionWriteRefused/generic-key` |
| drop-property-form | 21 / 9 | `TestUnionWriteLegalNeighbours/identical` |
| drop-element-form | 9 / 21 | `TestUnionWriteRefused/computed` |
| drop-constraint | 1 / 29 | `TestUnionWriteRefused/generic-key` |
| drop-nested-form | 4 / 26 | `TestUnionWriteRefused/generic-nested` |
| drop-compound-form | 1 / 29 | `TestUnionWriteRefused/compound` |
| logical-result-instead-of-rhs | 1 / 29 | `TestUnionWriteLegalNeighbours/logical_fits` |
| drop-prefix-form | 1 / 29 | `TestUnionWriteRefused/prefix` |
| read-type-instead-of-write-type | 1 / 29 | `TestUnionWriteLegalNeighbours/setter_write_type` |
| drop-index-signature | 3 / 27 | `TestUnionWriteRefused/array` |
| first-key-only | 1 / 29 | `TestUnionWriteRefused/union-key` |
| drop-key-property | 3 / 27 | `TestUnionWriteRefused/generic-key` |
| identity-instead-of-assignability | 6 / 24 | `TestUnionWriteLegalNeighbours/setter_write_type` |
| drop-postfix-form | 1 / 29 | `TestUnionWriteRefused/increment` |
| declared-type-instead-of-narrowed | 2 / 0 | `TestUnionWriteLegalNeighbours/generic_narrowing` |
| drop-assignment-guard | 1 / 0 | `TestUnionWriteLegalNeighbours/read` |

The two new checker shims call upstream's `getWriteTypeOfSymbol` and
`getApplicableIndexInfo` on the pinned TypeScript checkout. The setter and
literal-array-index controls distinguish those operations from reading a
property's type and looking up only an exact index signature.

## Scope

The listeners cover direct property/element assignment operators and prefix or
postfix updates, including targets at arbitrary access depth and generic
receivers/keys constrained to unions. Computed keys include literal, union,
symbol and numeric index-signature keys. The receiver's flow type preserves
narrowing; the value is tested for assignability, so a function returning a value
can still fill a void-returning function slot.

Destructuring patterns and loop assignment targets are not analyzed by this
commit: their source parts need a separate projection, and this fixture set does
not establish that projection. `delete` and method-call mutations are also not
analyzed: they do not assign an expression's value through these target forms.
No new refusal is claimed for those forms. No casts, aliases, or nonunion
mutable-widening relations were added to this rule.

## Integration checks

```sh
go run ./internal/types/tools/generate_shims
go run ./internal/settingsschema/tools/generate
go test ./internal/testpolicy ./internal/lint/registry ./internal/guard -json
go vet ./internal/lint/rules/adamic ./internal/lint/configuration ./TypeScript-shim/checker
gofmt -l internal/lint/rules/adamic/union_write*.go TypeScript-shim/checker/shim.go
go test ./internal/lint/configuration ./policy ./internal/types/program -json
```

- Shims and schema generators: exit 0; schema writes 3 files.
- Required guards: testpolicy 4 passed; registry 26,131 passed, 2 skipped;
  guard 33 passed; 0 failures. The registry run includes the ESLint core corpus;
  no known-gap row changes for this Adamic rule.
- Vet: exit 0. gofmt: exit 0, 0 filenames. `git diff --check`: exit 0.
- Configuration: 130 passed, 4 skipped. Policy: 26 passed, 0 skipped.
- Program integration: 270 passed, 1 failed. The failure is
  `TestProjectFilesGuardFiresOnMismatch`: on this case-sensitive filesystem,
  its deliberately uncanonicalized comparison still matches both files. The
  same test failed against an origin/main production-code overlay: 1 run,
  1 failure, same message. No change to that unrelated test is included.

The setup environment adds `GOFLAGS=-buildvcs=false -trimpath`. The first
`go run ./internal/docsdata/tools/generate -capture` fails because Tailwind's
runtime.Caller-based fixture lookup treats trimmed module paths as filesystem
paths. The recapture is retried with `GOFLAGS=-buildvcs=false`; no Tailwind
source or fixture is changed.

`GOFLAGS=-buildvcs=false go run ./internal/docsdata/tools/generate -capture`
exited 0: 44 passing packages, 23 packages without
tests, 0 failed packages; 42,996 captured records (15,411 distinct cases with
options). Five docs/data files change for union-write. One stale Tailwind
example message also changed during recapture and was restored as unrelated.
The authored structure/base tier settings are untouched; generated set listings
reflect their existing inheritance from cohere:adamic.
