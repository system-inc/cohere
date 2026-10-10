# #xzpba0r indexed-access writes

Base: cohere `839b0cdf364baa36372fa893a4edeb54410524ca`.
Adamic census checkout: `98008bbba2881939e07a9d74031994102a3bbe36`.

## Ownership and probe

New rule `adamic/no-indexed-access-write`, enabled in `cohere:adamic`.
A readonly primitive indexed-access slot is not a mutable-container widening,
so widening `invariant-mutable` would give its name and diagnostic a different meaning.
The shared flow walker now offers primitive-valued generic indexed accesses.
The diagnostic names the indexed slot, source type, and constraint.

The supplied TypeScript 6.0.3 / Node 24 runtime probe satisfies #xjdce2d's
standing rule; the ruling does not substitute for a probe, and no hypothetical
constant fold is needed. Other relations retaining a parameter after constraint
expansion remain outside this change and require their own Node probe.

Reads from T remain accepted. tsgo reduces dot reads to their constraint's
property type, so the rule recovers their receiver and key. The same provenance
is preserved when the walker projects properties from a T source into nested slots.

## Fixtures and controls

`go test ./internal/lint/rules/adamic -run '^TestIndexedAccessWrite$' -v`:
10 subtests pass (6 refusal fixtures, 4 accepted fixtures with a refusal plant each).
All 10 source fixtures have 0 tsgo semantic diagnostics under Adamic's options.
Each refusal reports exactly 1 `indexedAccessWrite`, at `2`; each accepted fixture
reports 0 and each plant reports exactly 1 at `2`.

Before-change demonstration: restored `flow/sites.go` and `flow/walk.go` from
HEAD, temporarily removed the two new rule implementation/registration files,
and ran the same command with the new fixtures. The test's absent-rule adapter
represents the unregistered rule with an empty listener. All 10 subtests failed
with expected 1 finding, got 0 (exit 1). See `old-code-tests.log`.

Drop-check mutant: inserted an immediate return in the rule's site listener.
The same command failed all 10 subtests with expected 1 finding, got 0 (exit 1).
See `mutant-tests.log`. Both temporary changes were restored before validation.

Actual CLI gate, with `{ "extends": ["cohere:adamic"] }` and both probe files:
`cohere-{before,after} --directory <probe-directory> --lint-config <config>
--lint --no-cache --json`: 2 files checked in each run; before 0 findings,
after 1 finding at refused.ts:2:31 and 0 findings in accepted.ts.

Runtime command (from cohere):

```sh
node <typescript-6.0.3>/bin/tsc --ignoreConfig --strict --target es2024 \
  --module nodenext --outDir <output> \
  internal/lint/rules/adamic/testdata/indexed-access/refused.ts \
  internal/lint/rules/adamic/testdata/indexed-access/accepted.ts
node <output>/refused.js
node <output>/accepted.js
```

TypeScript: 0 diagnostics, exit 0. Node v24.19.0: refusal throws 1 TypeError
(`Cannot read properties of undefined (reading 'toUpperCase')`); accepted prints
`ONE`, exit 0. See `node-refused.log`.

## Initial package and static checks

Commands used provisioned `go version go1.27.1 linux/amd64`, with `which go`
returning `/workspace/adamic-tools/go/bin/go`. Tests were run one package at a time.
Counts include parent and subtest pass events reported by `go test -json`.

| Command | Result |
| --- | --- |
| `go test ./internal/lint/rules/adamic -json` | 387 pass events, 0 failures |
| `go test ./internal/lint/checking/flow -json` | 1 pass event, 0 failures |
| `go test ./internal/lint/configuration -json` | 130 pass events, 4 skips, 0 failures |
| `go test ./policy -json` | 26 pass events, 0 failures |
| `go vet ./internal/lint/rules/adamic ./internal/lint/checking/flow ./internal/lint/configuration ./policy` | exit 0, 0 diagnostics |
| `gofmt -l` on the 5 changed/added Go files | exit 0, 0 filenames |
| `git diff --check` | exit 0, 0 whitespace errors |

## Census and relay

Built each CLI with `go build -o <binary> ./command/cohere` from inside cohere.
Both census commands used the same `cohere:adamic` settings:

```sh
<binary> --directory /workspace/adamic --lint-config <config> --lint --no-cache --json
```

Before and after: 2,140 checked files, 1,068 findings, 0 rule crashes.
The after binary runs 36 rules versus 35 before. Comparing complete finding
records as multisets: 0 added sites and 0 removed sites. There are no newly
refused Adamic sites to classify. Exit 1 in both runs reflects existing findings.
The summary JSON files record the counts and coverage limits of this census.

Ahra census is owner-run. @system_cohere_lint confirmed that this worker does
not need to run it, since the repository is not reachable here.

For @system_cohere_lint to relay on #js89dcw: the Adamic counts and zero-site
delta above. Push this branch for review only; the owner lands through cohere's
landing tool and notifies @system_adamic of the landed SHA.

## Reviewed follow-up: spreads, methods, and parallel tests

Review base: `56560a66281757cd29c86fd0e313782cd844bbe5`.
Both `site.Spread` and `site.Method` can carry this indexed-access shape;
neither is excluded now. The walker also pairs methods on reused object values.
The override example `{ ...source, value: 2 }` was already caught at its explicit
property assignment. A copied concrete property was missed at the spread source.
Contextual method returns and bivariant method parameters were missed at their
method site; a reused method object was missed because method descent was off.

The same TypeScript 6.0.3 strict command from above was run over `spread*.ts`
and `method*.ts`: 6 files accepted, 0 diagnostics, exit 0. Running each emitted
file with Node v24.19.0 gives exit 1 and the same TypeError reading `toUpperCase`
from undefined (6/6 failures; `edges-node.log`).

| Refused fixture | Finding span | Runtime |
| --- | --- | --- |
| `spread.ts` | `source` | concrete property copied into the indexed slot |
| `spread-override.ts` | `2` | explicit override stores a concrete value |
| `method-return.ts` | `get` | contextual method returns a concrete value |
| `method-argument.ts` | `put` | wider caller passes 2 to an indexed parameter |
| `method-reused.ts` | `result` | reused object's method returns a concrete value |
| `method-mixed-return.ts` | `get` | one return reads T, another returns 2 |

Accepted controls cover a spread from T, a spread overridden by a read from T,
a contextual method returning that read (with an unrelated nested function),
and a method accepting the full constraint while returning a read from T.
Every accepted control has a tsc-accepted refusal plant. Inferred contextual
method reads need the same provenance recovery as direct reads; every actual
return must have the indexed type, and nested functions' returns are ignored.

`go test ./internal/lint/rules/adamic -run '^TestIndexedAccessWriteEdges' -v`:
10 subtests pass after (6 refusals, 4 accepted controls with plants); each refusal
and plant has exactly 1 finding at the span in its assertion. All 14 fixture and
plant programs have 0 tsgo semantic diagnostics under Adamic's options.
Restoring the rule from the review base gives 8 failed subtests, 2 passed, exit 1
(`edges-before.log`). The explicit override and its plant were already caught.
An immediate-return mutant dropping the check gives 10 failed subtests, exit 1
(`edges-mutant.log`). Both changes were restored before final checks.

All 3 test functions and all 4 `t.Run` callback definitions in
`indexed_access_write_test.go` now call `t.Parallel()` as their first statement;
fixtures, temporary programs and checker state are independent.

Commands at the spread/method follow-up:

| Command | Result |
| --- | --- |
| `go test ./internal/testpolicy -run TestEveryCohereTestRunsInParallel -v` | 1 pass, 0 failures (`testpolicy.log`) |
| `go test ./internal/lint/rules/adamic -json` | 399 pass events, 0 failures |
| `go vet ./internal/lint/rules/adamic` | exit 0, 0 diagnostics |
| `gofmt -l` on the 2 changed Go files | exit 0, 0 filenames |
| `git diff --check` | exit 0, 0 whitespace errors |

At the spread/method follow-up, the same Adamic census command checked 2,140 files and
reports 1,068 findings, 0 rule crashes. Comparing full finding records against
the prior approved binary gives 0 added and 0 removed sites. See
`adamic-edges-summary.json`; existing findings account for census exit 1.

## Clean function forms follow-up

Review base: `cf2e340bfbae3739fa28a263681dca581fdba972`.
Added three standalone accepted fixtures:

- `accepted-method.ts`: the plain block-bodied method shorthand `get() { return source.value; }`.
- `accepted-arrow.ts`: the expression-bodied property arrow `get: () => source.value`.
- `accepted-function.ts`: the function-expression property `get: function () { return source.value; }`.

All are quiet. Every fixture replaces `source.value` with `2` as a control,
which must report exactly 1 finding. The asserted spans are respectively `get`,
`() => 2`, and `function () { return 2; }`. Method shorthand goes through the
method site; arrow and function-expression properties go through their initializer
function relation. Expression bodies now use the same indexed-return predicate
as block return statements, as the function's one return.

Focused command:

```sh
go test ./internal/lint/rules/adamic \
  -run '^TestIndexedAccessWriteEdgesStayClean/(plain_block_method|expression_arrow|function_expression)$' -v
```

Before the fix: 1 failed subtest (the clean arrow expected 0 findings, got 1),
2 passed subtests (method shorthand and function expression, including plants),
exit 1. See `clean-functions-before.log`. After: 3 passed subtests, 0 failures;
each plant reports 1 finding. See `clean-functions-after.log`.
The drop-check mutant gives 3 failed subtests, each expecting 1 plant finding and
getting 0, exit 1. See `clean-functions-mutant.log`. The mutation was restored.
All 3 source fixtures and all 3 plants have 0 tsgo semantic diagnostics under
Adamic's options, asserted by the tests.

The TypeScript 6.0.3 strict command from above was also run over all 3 clean
fixtures and their 3 plants: 6 accepted programs, 0 diagnostics, exit 0.
Node v24.19.0 prints `ONE`, exit 0 for each clean fixture; each plant exits 1
with `TypeError: Cannot read properties of undefined (reading 'toUpperCase')`.
See `clean-functions-node.log`.

Current checks:

| Command | Result |
| --- | --- |
| `go test ./internal/testpolicy -run TestEveryCohereTestRunsInParallel -v` | 1 pass, 0 failures (`clean-functions-testpolicy.log`) |
| `go test ./internal/lint/rules/adamic -json` | 402 pass events, 0 failures |
| `go vet ./internal/lint/rules/adamic` | exit 0, 0 diagnostics |
| `gofmt -l` on the 2 changed Go files | exit 0, 0 filenames |
| `git diff --check` | exit 0, 0 whitespace errors |

No census was rerun for this follow-up: @system_cohere_lint owns the census
after this fix. The worker pushes the branch without landing it.
