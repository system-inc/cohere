# Cross-enum narrowing probes (#t98h9pd)

Rule: `adamic/no-cross-enum-narrowing`, enabled in `cohere:adamic`; other sets inherit through their existing composition.
The name describes the checker failure rather than treating every cross-enum comparison as unsafe.

## Runtime grounding

Measured with Go 1.27.1, TypeScript 6.0.3, and Node v24.19.0. For each of the
17 `.ts` files here and `../cross-enum-whole-computed.ts`:

```sh
node <typescript-6.0.3>/lib/tsc.js --ignoreConfig --strict --target es2022 --module nodenext --noEmit <fixture>
node --experimental-transform-types <fixture>
```

18 compiler runs exited 0; 18 Node runs exited 1 with TypeError. `--ignoreConfig`
is required by tsc 6 when explicitly naming files inside a configured repository.
The five supplied programs (`t98a`, `t98b`, `t98d`, `t98e`, `t98g`) retain their original text.

The additional probes cover `==`, `!=`, reversed operands, const enums, const
aliases, import aliases, constant expressions (`1 << 0`), member initializers,
declaration merging, a whole compared enum, element access, and a comparison after
prior flow narrowing. The adjacent whole-computed probe proves that an unknown
computed member must not hide another member whose shared value is known.

All named cousins are covered. A proposed `v.kind !== Kind.B && v.kind !== Other.X`
variant was left out because strict tsc rejected its numeric return with TS2339.
Unknown runtime member values themselves cannot establish a shared constant and
are not guessed. Disjoint enum comparisons already rejected by TS2367 are outside
this rule; their quiet control is intentional.

## Fixture and old-engine checks

`go test ./internal/lint/rules/adamic -run '^(TestCrossEnumNarrowing|TestWholeComputedEnumNarrowing)' -v`
passes 18 refused fixture cases (19 findings), 12 quiet cases (0 findings), and 12
unsafe namespace plants (12 findings). Quiet cases include different values,
same-enum and self comparisons, a different member of the compared enum that tsc rejects, primitive literals, only the compared enum,
ordering, a narrowed write, a void function receiving a value-returning function,
and a defined value.

The same tests were run on the old engine using a Go overlay replacing both new
production files with `package adamic`. The registration-resolving test subject
therefore falls back to the pre-change empty rule. The 17 directory fixtures and
the adjacent whole-computed fixture all failed: 0 findings, where 19 were expected.
Before implementation, the initial fixture suite also failed with the production
files physically absent. The overlay avoids changing source while other checks run.

Each quiet case has an unsafe plant, and every refused fixture fails without the
rule. Targeted overlay mutants and the checks they fail are recorded below.

Reproduce all 13 with `python3 internal/lint/rules/adamic/testdata/cross-enum/mutants.py`. Each mutant was run with `go test -overlay=<mutant>.json ./internal/lint/rules/adamic -run '^(TestCrossEnumNarrowing|TestWholeComputedEnumNarrowing)' -v`. All 13 exited 1 with assertion failures; none survived or failed to compile. Production source remained unchanged.

| Dropped guard/form | Failing fixture subtests | One failing control |
| --- | ---: | --- |
| `unknown-member-hides-known` | 1 | `TestWholeComputedEnumNarrowing` |
| `matching-member-overlap` | 1 | `TestCrossEnumNarrowingLegal/different_member_(tsc_already_rejects)` |
| `shared-value` | 1 | `TestCrossEnumNarrowingLegal/different_values` |
| `different-enum` | 3 | `TestCrossEnumNarrowingLegal/same_enum` |
| `overlap` | 2 | `TestCrossEnumNarrowingLegal/no_overlap_(tsc_already_rejects)` |
| `single-enum` | 1 | `TestCrossEnumNarrowingLegal/self_comparison` |
| `declared-type` | 1 | `TestCrossEnumNarrowingRefused/declared-before-flow` |
| `strict-equal` | 26 | `TestCrossEnumNarrowingRefused/t98a` |
| `strict-unequal` | 2 | `TestCrossEnumNarrowingRefused/t98g` |
| `loose-equal` | 1 | `TestCrossEnumNarrowingRefused/loose-equal` |
| `loose-unequal` | 1 | `TestCrossEnumNarrowingRefused/loose-unequal` |
| `switch` | 1 | `TestCrossEnumNarrowingRefused/t98e` |
| `swapped-side` | 28 | `TestCrossEnumNarrowingRefused/t98a` |

`unknown-member-hides-known` rejects an entire enum when any member has an unknown
computed value. `matching-member-overlap` treats any member of the compared enum as an overlap, even when its value differs. `declared-type` substitutes the current flow type for the declared
type. `swapped-side` removes the second operand orientation. The other mutants
remove the named identity/value/overlap guard or comparison listener/operator.

## Package checks

- `go test ./internal/lint/rules/adamic -json`: 550 test/subtest pass records, 0 failures; 32 pass records belong to the new tests (30 fixture cases and two parent tests).
- `go test ./internal/testpolicy ./internal/lint/registry ./internal/guard`: 3 packages passed.
- `go vet ./internal/lint/rules/adamic ./internal/lint/configuration ./policy`: 3 packages, 0 diagnostics.
- `gofmt -l internal/lint/rules/adamic/no_cross_enum_narrowing*.go`: 0 files listed.
- `go run ./internal/settingsschema/tools/generate`: exited 0, changed 3 schema files.

No ESLint core corpus row is changed by this Adamic rule. No base or structure
settings were edited, no consumer census was run, and main was not pushed or landed;
those landing decisions belong to the lint owner.

`go build ./command/cohere` exited 0 with the final rule.

## Documentation capture

`GOFLAGS='-buildvcs=false' go run ./internal/docsdata/tools/generate -capture`
exited 0: 43,246 records captured, including 15,413 distinct option-bearing cases.
Five related docs/data files are retained (the two copied schemas, the new rule's
example, rules.json, and sets.json). One unrelated Nexus example selected during
recapture was restored. Generated set data reflects existing inheritance; the
only source setting changed is cohere:adamic.

The first capture failed in Tailwind tests: setup's `-trimpath` rewrites
runtime.Caller paths used to find vendored CSS fixtures. The fixtures were present;
`go test -trimpath=false ./internal/lint/rules/tailwind/vendored` passed (1 package).
Capture was rerun with source paths preserved. Its next run passed the lint tests
but refused the missing name in RuleNaming.json; adding the requested name to
that closed catalog completed generation. No Tailwind source was changed.

`GOFLAGS='-buildvcs=false' go run ./internal/docsdata/tools/generate -check-rendering`
exited 0 (0 stale rendered files), reading the retained examples rather than recapturing.
