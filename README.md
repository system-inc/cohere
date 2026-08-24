# verify

One binary that type-checks, lints, fixes, and formats a TypeScript codebase — in one process,
over one AST, against one type graph.

## Why

Writing code is becoming free; knowing it is correct is not. A gate that is slow gets skipped, and
a skipped gate enforces nothing. The goal is unlimited rules checked in about a quarter second, so
that encoding a judgment stops needing justification.

Measured on a 3,416-file, 24.7 MB TypeScript codebase, the existing toolchain costs 9.3 seconds —
517x slower than running a regex over the same bytes. Almost none of that is compute. It is
boundaries: rules crossing into a JavaScript isolate, a Tailwind plugin making a blocking
cross-thread round trip per class literal, and a type checker rebuilding a 9,530-file program from
scratch on every run, once per shard.

`verify` removes the boundaries by standing in the type checker's own language.

## Built on

- [microsoft/typescript-go](https://github.com/microsoft/typescript-go) — Apache 2.0. The TypeScript
  compiler in Go. Vendored as a submodule and reached through generated shim modules.
- [typescript-eslint/tsgolint](https://github.com/typescript-eslint/tsgolint) — MIT. The shim
  generator and the type-aware rule catalog. We do not fork it: its architecture is one-shot and
  stateless by design, which is precisely what we are replacing.
- [web-infra-dev/rslint](https://github.com/web-infra-dev/rslint) — MIT, copyright Bytedance Inc and
  typescript-eslint contributors. Its `internal/utils/cfg` is vendored at
  `internal/utils/controlflow`: a basic-block control-flow graph with dominators, reachability, and
  correct `try`/`catch`/`finally` edges, built on the same typescript-go AST we use. Vendored rather
  than depended on, because it is a package rather than a library and we change it when we change a
  shared decision. The header on `internal/utils/controlflow/cfg.go` names the commit and what was
  changed.

## Development

```sh
git submodule update --init --depth 1 typescript-go
go build ./...
go run ./cmd/verify
```

`verify` cannot verify itself — it is a Go program and its phases check TypeScript. This repo is
gated by Go's own toolchain: `gofmt -l .`, `go vet ./...`, `go test ./...`, `go build ./...`.

## Porting a rule

Five things, each of which caught a defect the others could not. They are ordered by what they cost
and what they find, not by importance.

**Read the upstream corpus first.** oxc ships its pass and fail cases inline in every rule file, sixty
to eighty per rule, at `~/Projects/system/oxc/crates/oxc_linter/src/rules/`. Reading them costs two
minutes. They encode the cases a porter does not think of, **by definition**: a porter who had thought
of them would have written them. A rule shipped past a twenty-case fixture pair and a three-guard
mutation sweep, and oxc's own passing case found the false positive in ten minutes.

It is also the thing that tells you what the rule **operates on**, which is what a neighbouring rule
most easily misleads you about. `no-nonoctal-decimal-escape` looks like a regex rule and is not:
`/\8/` is a pass in its corpus. Ported by analogy from the rule finished an hour earlier, the code and
its fixtures would have shared one wrong belief about the rule's surface, and every fixture would have
passed.

**Port from oxc, not from another implementation.** oxc is what the gate runs, so the differential
compares against it. A rule ported from `@typescript-eslint` or from rslint can be correct and still
read as a difference, which makes the harness less meaningful for no gain. Cite the others as a second
opinion; where they disagree, oxc wins and the disagreement earns a line in the commit.

**Measure the precondition before writing.** Count what the rule is about on the real tree, and prefer
a two-sided key: "3 violations, each carrying a disable comment" proves far more than a bare zero,
because a zero is equally satisfied by a rule that cannot see. Re-measure your own count when it moves;
a number large enough to feel like evidence stops you asking whether you counted the right thing.

**Ship a fixture pair and run a mutation sweep.** Both directions, same commit, and every mutant
confirmed to compile before it is trusted: a mutant that fails to build emits no failure line and reads
exactly like a passing sweep. A survivor is one of three things and only the first wants a fixture:

- the fixtures do not measure that branch, so write one
- the branch cannot change an answer, so delete it
- one behavior is held redundantly by two guards, so mutate them together
- the fixtures measure the right branch but assert the wrong layer

The fourth is the expensive one. A rule's flag was forced always-on and thirty fixtures stayed green,
because every fixture covering that input asserted **which rule fired** and never **what it offered**.
A finding with a wrong repair reads as a correct finding. For a rule carrying fixes or suggestions,
assert the repair rather than the id: the id is satisfied by a correct detection with a wrong fix.

**Run against the real tree before committing, not after.** Two false positives shipped past a full
fixture pair because the fixtures were written from the same wrong belief as the code. The tree is the
only check that does not share the author's assumptions.

### When the sweep itself is the thing that lied

Four faults have produced a clean sweep from a probe that never ran, and every one reads identically
to a mutant nothing catches. Three appeared twice, in different members' harnesses, within one night.

- **A mutant that does not compile.** `go test` reports `[build failed]` and zero failing tests. Assert
  the mutant builds before reading its result.
- **A mutation that did not apply.** A replacement string that does not match the source changes
  nothing and fails nothing. Assert the file changed before running tests.
- **A filter that cannot match.** `grep -c` counts matching lines per file and `grep -o` counts
  occurrences; a total built from the wrong one reports zero against output that has failures in it.
- **A restore that silently did not run.** An unset variable in the cleanup leaves the mutant in the
  tree, and a restore that did nothing looks exactly like one that worked. Verify with `cmp`.

The shape is always the same and it is the same shape the rules themselves fail in: **the probe did not
run, and nothing said so.** A sweep is what everything else here is trusted on, so it is the one
instrument whose own failure modes have to be checked rather than assumed.

### What a fix costs

`ReportNodeWithFixes` rewrites source, and the text is only half of it: a correct replacement over the
wrong range writes the right characters into the wrong place. Pointing one deletion at an enclosing
node instead of a statement removed an entire function with every existing assertion still passing. So
`ruletest.ExpectFixedSource` is required for any rule that proposes fixes, and the fixture-pair guard
enforces it.

A port is not obliged to carry a defect it can see. Two rules here report without the fix their
original ships, because those fixers drop type annotations, lose `async`, or replace a whole
`VariableDeclaration` while reporting per declarator, which silently deletes a component. Reporting
without a fix is the subset you can show correct.

### Fidelity

A port is faithful to what a rule **decides**, not to how it **obtains what it needs**. The originals
walk the filesystem and read `process.cwd()` because ESLint hands them one file at a time and gives
them no program. `verify` has the program. Reproducing a workaround for a constraint we do not have is
not fidelity.

Where a rule has no tree exposure and the upstream gives no reasoning, fidelity is the only available
authority, and your own sense of what the rule should do is the thing most likely to be wrong. Record
the intuitive reading beside the actual one so the next reader does not correct it back.

## The dispatcher

Rules are compiled in rather than loaded, which is what makes them free to run. The cost is that
adding a rule means rebuilding, so `cmd/verify-dispatch` pays that cost automatically: it hashes
everything the binary is built from, looks for `.cache/verify/bin/verify-<platform>-<hash>`, and
execs it when present or builds it first when absent. Editing a rule costs one rebuild; every run
after it is a stat and an exec.

```sh
verify                    # resolve, rebuild if the rules moved, exec
verify --dev              # build to a stable path instead of a hash-named one
verify --dispatch-verbose # say so when a rebuild fires
```

Anything the dispatcher does not own is forwarded to the real binary untouched.

`--dev` exists because Go caches package compilation but not linking, so a hash-named binary is a
new filename and therefore a full link on every change. Measured on a comparable binary: a leaf rule
edit costs 2.03s, while a genuinely unchanged tree at a stable path costs 0.29s. The stable path is
what makes that floor reachable during rule authoring. It records its hash beside the binary and
compares it, so a stable name never means a stale binary.

The build flags are fixed at `-trimpath -ldflags="-s -w"`. They make the link marginally faster and
the binary 29% smaller, and `-trimpath` is a build-input change rather than a link flag: turning it
on invalidates the entire compile cache, measured at 33s on a warm 2.0 GB cache. Flipping it per run
would pay that repeatedly, so it does not vary. `GOCACHE` is pinned inside `.cache/verify/` so that
other Go work neither shares it nor evicts it — Go's default cache trims entries unused for about
five days, which would quietly turn a warm rebuild into a cold one.

**A missing binary is a loud error, never a fallback.** With no Go toolchain the dispatcher runs the
binary shipped for the platform, and if there is none it exits non-zero naming the platform and
everywhere it looked. There is deliberately no path where it execs something that might do nothing:
the gate this tool replaces printed green over zero files for days because a resolver found no
binary, fell through to a bare command name, and an empty file list is indistinguishable from a
clean tree.

## Releasing

`verify` reaches a machine as an npm install. One thin dispatcher package resolves a per-platform
binary package, the same shape oxlint and tsgo use:

```
verify                  the package you install; a Node launcher, no binary
@verify/darwin-arm64    ~43 MB Go binary, stripped
@verify/darwin-x64
@verify/linux-arm64     for CI and containers
@verify/linux-x64
@verify/win32-arm64
@verify/win32-x64
```

Every size here names the build that produced it, because the same commit measures 43.1 MB stripped
and 61.9 MB from a plain `go build`. An 18 MB spread under one label is not a measurement, it is two
measurements sharing a name, and two people quoting sizes from different builds will both be right
and still disagree.

Most of that is architecture rather than catalog. Attributed by symbol on an unstripped build,
measured at 06:23: 23.2 MB of Go runtime and type metadata, 7.2 MB more of `go:` metadata, 9.1 MB of
typescript-go, 1.54 MB of goja, and 0.18 MB of our rules. Against the 56 rules the binary reported
running in that same measurement, that is about 3.3 KB per rule, so porting the remaining hundred
adds under half a megabyte. Embedding a whole type checker and a whole JavaScript interpreter is what
costs, and both were decided long before the rules were.

Two traps live in that paragraph, and both cost someone time tonight. `go tool nm` reports every
symbol as size zero on a stripped binary, so an attribution run against a release build sums to
nothing rather than failing — the numbers above come from an unstripped build for that reason. And a
standalone program measures a dependency's marginal cost including everything it drags in, which is a
different quantity from its share of a binary that already paid for most of that: goja measures
1.54 MB by attribution and 8.47 MB as the marginal cost of adding it to an empty program. Both
are correct; quoting one to answer the other's question is not.

The platform packages are `optionalDependencies` pinned to the exact version, and npm picks one by
matching the `os` and `cpu` fields against the machine. Those fields are generated from the same
target that cross-compiles the binary, because npm's spelling and Go's disagree in two places —
`win32` against `windows`, `x64` against `amd64` — and a package published under the Go spelling
installs correctly and is never found, which on the machine is indistinguishable from a platform we
never shipped.

```sh
go run ./cmd/verify-release --version 0.1.0 --output dist
```

One command builds all six from one machine, in about two minutes, and it stages rather than
publishes. It refuses a release it cannot complete: a target that fails to build fails the whole
run, because a version missing one platform resolves to nothing there and gets reported as a bug
against a release that looked fine everywhere else.

The launcher is Node rather than Go, which is the one surprising choice. A Go dispatcher would have
to be cross-compiled per platform, making it a seventh platform package and defeating the point of
installing one thing. Node is present by construction in an npm install, and `require.resolve` asks
the package manager where a package actually is instead of modeling pnpm, npm, and yarn layouts by
hand.

`verify --version` reports the version, the platform, the Go toolchain, and the vendored compiler as
`owner/name@commit`, so a bug report names what was running rather than "latest". The stamps go in
at link time; an unstamped local build says `dev` and says why.

The compiler's repository is stamped rather than written down, because it has already moved once:
the pin was against `microsoft/typescript-go` until that repository was archived, and is now against
`microsoft/TypeScript`. Both spellings are forty hex characters and resolve in different places, so a
hardcoded label would have survived the migration while quietly becoming false.

### The formatter's bundles

The formatter runs our Prettier fork inside a JavaScript engine, and those bundles are build output
of `~/Projects/system/prettier` rather than anything this repository pins. `--embed-formatter` pulls
them in and stamps the fork's `HEAD` beside the compiler pin, so `--version` grows a `formatter:`
line naming exactly which Prettier a binary formats with.

That stamp exists because the fork is reached by a path on a build machine, not by a version. Without
it, two binaries built from the same verify commit can format the same file differently and neither
can say why, and formatting differences are the worst kind to debug from a report: every diff after
the first one is noise.

The flag is off by default, because the formatter is not wired into verify yet and a release should
not demand a built fork for a feature nothing reaches. When it is on, the build refuses a fork that
is absent, unbuilt, missing any required bundle, holding a zero-byte one, or whose bundles are older
than its tracked source. It refuses before cross-compiling anything, so a stale fork costs a second
rather than six builds. The bundle list is `prettier.BundleFiles` itself rather than a copy of it —
eight bundles, 2.0 MB measured, against 12 MB for the fork's whole `dist/prettier`. Keeping a second
list here was a real defect and not a hypothetical one: this file once named three bundles, chosen as
the minimal set that formats TypeScript, while the engine hard-errors on any of its eight being
absent. The guard would have passed a release missing five, and the binary would have died the first
time anyone formatted markdown.

Staleness is measured by modification time against the fork's newest tracked source file, not by
recording a commit beside the bundles. A recorded commit only catches a rebuild someone remembered
to re-record; the case that actually happens is an edited working tree that was never rebuilt, where
the commit has not moved and the bundles are wrong anyway. `AHRA_VERIFY_PRETTIER_FORK` points at a
checkout somewhere other than the default path.

`AHRA_VERIFY_BINARY=/path/to/verify` points every `verify` on the machine at a local build. A broken
override is fatal rather than a fallback, even when a good install is sitting right there: someone
who sets it has stated which binary they want, and quietly running a different one would hand them
results they would read as their own build's.

### macOS signing

Measured, because the answer decides how much this matters. An unsigned binary with no quarantine
attribute runs normally, and that is what an `npm install` produces — package managers extract
tarballs without setting `com.apple.quarantine`, so consumers installing from the registry are not
blocked. The same binary *with* quarantine set is killed by the kernel: exit 137, SIGKILL, and zero
bytes on both stdout and stderr.

That silent kill is the reason to sign. It is reached whenever the binary travels as a file rather
than as a package — a release asset from a browser, a binary copied out of CI — and it fails in the
worst available way, with no output to explain it. The launcher's signal handling turns it into a
loud 137 rather than a green nothing, and signing removes it entirely.

Signing is wired and credential-gated. It needs a **Developer ID Application** certificate
specifically; an Apple Development certificate signs successfully and is then rejected by the notary
service at the end of a release. Staging without credentials is supported and says so in its own
summary, because an unsigned release is fine and an unsigned release that looks signed is not.

Note that a notarization ticket cannot be stapled to a bare executable — only to a bundle, a disk
image, or an installer package — so a notarized Mach-O is validated by an online check on first
launch. A machine that is entirely offline the first time it runs a quarantined `verify` is still
blocked. The npm path does not set quarantine, so this does not affect it.
