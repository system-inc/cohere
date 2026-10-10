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

## Package and static checks

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

**Ahra remains blocked and is required before landing.** No local checkout was
available. `git ls-remote https://github.com/system-inc/ahra.git HEAD` returned
exit 128: `remote: Repository not found.` An accessible URL or checkout path was
requested; no substitute corpus was used and no ahra counts are claimed.

For @system_cohere_lint to relay on #js89dcw: the Adamic counts and zero-site
delta above, plus the blocked ahra census. Push this branch for review only;
land through cohere's own landing tool after the outstanding census. Notify
@system_adamic of the landed SHA then; this worker does not claim a landed SHA.
