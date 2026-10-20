# Rest-target verification (#6j6ekcs)

Branch: `lint-fix/6j6ekcs-rest-targets`, based on the requested
`becd3ec2731160d2147bddc21696c29412445077` (not origin/main).
TypeScript pin: `d92d9bfee114c80be2c375d72edae966176e3a4f`.
Go 1.27.1, Node 24.19.0, JavaScript tsc 6.0.3.

## Projection and cost

The whole rest target is checked as one slot. Array rest removes the consumed
prefix from each source tuple and preserves the resulting mutable tuple; an
array/iterable source produces an array of its element type. Object rest uses
the compiler's getRestType, with the names consumed by earlier pattern entries.
Known tuple lengths are preserved so safe tuple-valued slots remain quiet.

Projection runs once per rest target, before checking any target union members.
The owned traversal scans the pattern prefix once. The array source union is
iterated once; helpers receive the actual member, not the original union.
The object helper maps actual source members internally and is called once.
Neither target-member checks nor source-member loops re-project the whole union
into itself. Ordinary identifier, non-union and narrowed targets exit before
projection. Checker assignability and type normalization remain compiler work.

The whole-source-union mutant below is caught by a union of tuples whose
consumed prefixes differ but whose numeric remainders fit every target member.
This specifically controls the per-member whole-union re-expansion footgun.

## Old-code failure and runtime grounding

Before changing union_write.go:

```sh
go test -overlay=/tmp/rest-old-overlay.json ./internal/lint/rules/adamic -run '^TestUnionWriteRefused$/^rest-' -json
```

The overlay replaced only union_write.go with `git show becd3ec2:internal/lint/rules/adamic/union_write.go`.
On becd3ec2, the following **9 new refused subtests failed**, each with **0 tsgo
diagnostics** and **0 findings** instead of 1:

- `TestUnionWriteRefused/rest-array`
- `TestUnionWriteRefused/rest-array-generic`
- `TestUnionWriteRefused/rest-array-leading`
- `TestUnionWriteRefused/rest-array-tuple`
- `TestUnionWriteRefused/rest-array-union-source`
- `TestUnionWriteRefused/rest-object`
- `TestUnionWriteRefused/rest-object-generic`
- `TestUnionWriteRefused/rest-object-leading`
- `TestUnionWriteRefused/rest-object-union-source`

The final full package run passed **54 Node TypeError probes and all 54 removal
controls**. The initial baseline run passed its then-existing 52 probe/control
pairs; the last two source-union probes were added afterward. Each
new source ends its observation with toFixed on the number the types promised;
array writes put a string there, and object writes remove the required count.
Removing `probe()` makes the same program exit successfully. The leading object
case proves that a property consumed earlier is absent from the remainder.

Strict JavaScript tsc command (exit 0, **55 files**, **0 diagnostics**):

```sh
npm exec --yes --package=typescript@6.0.3 -- tsc --ignoreConfig --strict --noEmit --target es2024 --module nodenext internal/lint/rules/adamic/testdata/union-write/*.ts internal/lint/rules/adamic/testdata/union-write-legal/*.ts
```

## Final tests and controls

| Command/check | Result |
| --- | --- |
| `go test ./internal/lint/rules/adamic -json` | 48 top-level tests + 593 subtests passed; 0 failures |
| UnionWrite cases within that run | 4 top-level tests + 175 subtests |
| Refused sources | 54 sources, 0 tsgo diagnostics each; 1 finding each with expected span and slot message |
| Node probes | 54 TypeErrors + 54 successful removal controls |
| Legal neighbours | 67 quiet sources + 67 reported plants; 15 new pairs for rest |
| Delete control | still quiet; Node 0, removal control 1, string-write plant reported |
| `go test ./internal/testpolicy ./internal/lint/registry ./internal/guard -json` | 59 top-level tests + 26,124 subtests passed across 3 packages; 0 failures |
| `go vet ./internal/lint/rules/adamic github.com/microsoft/TypeScript/tsc/shim/checker` | 2 packages, 0 diagnostics |
| `gofmt -l internal/lint/rules/adamic/union_write.go internal/lint/rules/adamic/union_write_test.go TypeScript-shim/checker/shim.go` | 3 files checked, 0 printed |
| `go build ./command/cohere` | exit 0 before changes and on final code |
| `go run ./internal/types/tools/generate_shims` | exit 0; 3 methods exposed, no field/layout changes |
| `go run ./internal/settingsschema/tools/generate` | exit 0; 0 schema changes |

All new subtests use the existing t.Run bodies, whose first statement is
t.Parallel(). The legal neighbours cover compatible remainders, narrowing,
non-union targets, dropping a heterogeneous tuple prefix, an empty tuple tail,
union sources, excluding an object property, and tuple-valued slots.

### Mutants

Each used a Go overlay, leaving the working source unchanged, and ran:

```sh
go test -overlay=/tmp/rest-overlay-NAME.json ./internal/lint/rules/adamic -run '^TestUnionWrite(Refused|LegalNeighbours)$' -json
```

**9/9 build-valid mutants were caught.** Each overlay made the named edit to
union_write.go; actual failing tests are listed below.

| Mutant | Failing test |
| --- | --- |
| `drop-array-rest` | `TestUnionWriteRefused/rest-array` |
| `drop-leading-array-rest` | `TestUnionWriteRefused/rest-array-leading` |
| `drop-object-rest` | `TestUnionWriteRefused/rest-object` |
| `keep-consumed-array-prefix` | `TestUnionWriteLegalNeighbours/rest_tuple_drops_prefix` |
| `keep-excluded-object-properties` | `TestUnionWriteLegalNeighbours/rest_object_excludes_prefix` |
| `reexpand-whole-source-union` | `TestUnionWriteLegalNeighbours/rest_union_tuple` |
| `widen-remaining-tuple` | `TestUnionWriteLegalNeighbours/rest_tuple-valued_slot` |
| `drop-array-source-member` | `TestUnionWriteRefused/rest-array-union-source` |
| `drop-later-object-source-member` | `TestUnionWriteRefused/rest-object-union-source` |

The re-expansion mutant assigned `member = source` inside the array member loop.
The prefix/exclusion mutants used index 0 or an empty omitted-name list. The
widening mutant turned the sliced tuple back into an array. The three dropped
form mutants disabled array rest, rest after a leading element, or object rest.
The two source-member mutants kept only a safe source arm. Compiler union-member
order need not match declaration order: the array control used `[1:]` in checker
order, and the object control used `[0]`. Both lost the unsafe arm and were caught.

## Rule CPU from its own tests

After other jobs finished, with setup's original GOFLAGS:

```sh
go test ./internal/lint/rules/adamic -run '^TestUnionWrite(Refused|LegalNeighbours)$' -count=10 -cpuprofile=/tmp/rest-rule-cpu.pprof -o /tmp/rest-adamic-profile.test -json
go tool pprof -top -focus='union_write.go' /tmp/rest-rule-cpu.pprof
go tool pprof -top -focus='adamic\.init\.func10\.4|unionWriteRestType' /tmp/rest-rule-cpu.pprof
```

**adamic/union-write: 530 ms inclusive sampled CPU over 10 suite runs**
(20 top-level test executions, 1,210 subtest executions, 1,880 rule invocations
including plants). Rest handling accounts for **110 ms** of that sample.
Pprof reports **43.65 s total sampled test-process CPU**, **11.07 s wall profile
duration**, and **10 ms sampling period**. The source-file focus accounts for
0.53 s (53 samples, 1.21% of total); the rest callback/helper focus for 0.11 s
(11 samples). Each matching sample is counted once, including checker work below
that rule frame. Node subprocess probes were excluded from this profiling run.

These are sampled test-workload figures, not an ahra census or a timing limit.
The initial concurrent verification jobs approached the 16 GiB cgroup memory limit;
docs generation was stopped and subsequent measurement ran with no competing
verification job. The final profile above is the isolated run.

## Generation and handoff

`GOFLAGS='-buildvcs=false -p=2' go run ./internal/docsdata/tools/generate -capture`
passed: **43,351 records captured**, **15,413 distinct option-bearing cases
counted**. The command omits trimpath because the existing Tailwind fixture root
uses runtime.Caller, and limits concurrent packages to two after memory pressure
in the initial concurrent run. No persistent environment or Tailwind changes
were made. The unrelated generated Nexus example was restored; **0 schema and
0 docs/data files** are included in this unit. Original-GOFLAGS guards, vet and
build all passed.

No rule was added/renamed and no top-level package was added. The existing
cohere:adamic set remains enabled. No base/structure tier settings or ESLint core
known-gap rows changed; the registry corpus still passed all expected rows.

The consumer census was deliberately not run here: @system_cohere_lint owns it.
Before landing, run the ahra census with `--timing` and name
**adamic/union-write's CPU on ahra**. The figures above only satisfy the requested
sanity check from the rule's own tests. No Adamic Lower guard was removed.
