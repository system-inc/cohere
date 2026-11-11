# Contributing to cohere

How cohere is built, and why it is built that way. For installing and running it, see
[README.md](README.md).

## Why

Writing code is becoming free; knowing it is correct is not. A gate that is slow gets skipped, and
a skipped gate enforces nothing. The goal is unlimited rules checked in about a quarter second, so
that encoding a judgment stops needing justification.

Measured on a 3,416-file, 24.7 MB TypeScript codebase, the existing toolchain costs 9.3 seconds —
517x slower than running a regex over the same bytes. Almost none of that is compute. It is
boundaries: rules crossing into a JavaScript isolate, a Tailwind plugin making a blocking
cross-thread round trip per class literal, and a type checker rebuilding a 9,530-file program from
scratch on every run, once per shard.

`cohere` removes the boundaries by standing in the type checker's own language.

## The name

`cohere` is a transitive verb. You cohere something: it takes an object and changes it, which is
what this does. It lints, autofixes and formats, and `ReportNodeWithFixes` is the heart of the rule
system, so the most consequential surface here is the one that rewrites your source.

From _cohaerēre_, to stick together. The physics sense is the useful one: coherent light is not
light that is similar, it is light that is phase-locked, and nothing is added to make it so. The
photons stop cancelling each other. That is the problem this solves for a codebase written by many
hands at once.

## Coherent is not the same as correct, and `verify` is the program that will say so

Logic has the pair:

- **coherent** means the parts hang together, with no internal contradiction
- **correspondent** means the whole thing matches the world outside it

A perfectly coherent story can be entirely false. Coherence is a relation among the parts and says
nothing about whether they touch reality, which is why a green run over a probe that never fired
feels exactly like a green run over a clean tree.

Those are two jobs rather than two features of one program. `cohere` answers _is this well-formed
and in phase_, from the graph alone, which is what lets it be fast enough to leave on. A separate
`verify` will answer _did it actually do the thing_, which cannot be answered from inside the code
and needs running it and reading what came out. The oracles belong there: the fixture pairs, the
mutation sweeps, the known-dirty controls, the differential against the gate this replaces. Today
they live here, in `internal/lint/rules/tools/` and beside the rules they hold.

## Built on

- [microsoft/typescript-go](https://github.com/microsoft/typescript-go) — Apache 2.0. The TypeScript
  compiler in Go. Vendored as a submodule and reached through generated shim modules.
- [typescript-eslint/tsgolint](https://github.com/typescript-eslint/tsgolint) — MIT. The shim
  generator and the type-aware rule catalog. We do not fork it: its architecture is one-shot and
  stateless by design, which is precisely what we are replacing.
- [web-infra-dev/rslint](https://github.com/web-infra-dev/rslint) — MIT, copyright Bytedance Inc and
  typescript-eslint contributors. Its `internal/utils/cfg` is vendored at
  `internal/lint/ecmascript/control_flow_graph`: a basic-block control-flow graph with dominators, reachability, and
  correct `try`/`catch`/`finally` edges, built on the same typescript-go AST we use. Vendored rather
  than depended on, because it is a package rather than a library and we change it when we change a
  shared decision. The header on `internal/lint/ecmascript/control_flow_graph/builder.go` names the commit and what was
  changed.

## Development

```sh
git submodule update --init TypeScript
go build ./...
go run ./command/cohere
```

`cohere` checks itself: a bare `cohere --no-fix` at the root runs the Go engine over this module and the
Swift engine over `swift/`, and both read 0. Go's own toolchain gates it too: `gofmt -l .`,
`go vet ./...`, `go test ./...`, `go build ./...`.

Heavy Go work goes through `cohere-dev`, which takes a token from the machine's pool and runs with Go
held to the token's share of the cores, niced (#qhg0ntb): `go run ./command/cohere-dev test ./...` for the
landing gate, `test <package>` or `test --fast` while editing, `build` and `vet`, and `exec -- <command>`
for anything else heavy, a bench or a cohere run. A run that finds every token held waits its turn in
arrival order and names who holds them; `cohere-dev status` shows the pool and the line. Plain `go test`,
`go build` and `go vet` run unbudgeted on every core, and a dozen of them at once held the machine at load
110 to 180, made a 27s gate take 7 to 10 minutes, and failed tests on their deadlines. Inside a token's run
nothing more is held back: its packages and `t.Parallel` tests use the whole share.

A mutant or probe in code that starts processes runs bounded: `cohere-dev test --bounded <package>`, or
`cohere-dev exec --bounded -- <command>` (#fp1td9e). The run may start 512 processes beyond the account's
count when it begins (`--bounded-margin`), and its whole process group is stopped after 15 minutes
(`--bounded-deadline`) and whenever its command ends. A review mutant with a guard removed forked its own
binary about 7,700 times on 2026-11-10 and filled the machine's process table (#excff9x). A fixed `ulimit -u`
is the wrong recipe: the kernel counts every process the account owns, about 900 on Kirk's Mac, so a limit
of 512 refuses every fork.

Work lands on main through `go run ./command/cohere-dev land`, run in the worktree being landed, with
its work committed (#fz6xejy). It takes the machine's one land lock in arrival order and holds it from
merge to fast-forward. It makes one merge commit of the worktree's head onto current main, first parent
main and second the head, checks the worktree out at it, gates that tree through a pool token, and moves
main to it, checkout and all (#4v9pmef). The gate runs `go vet`, the rule CPU budget, the whole module's
tests, the docs data check, and last cohere itself on the tree (#06g3f7x): it builds cohere at that tree and
refuses the landing unless a bare `cohere --no-fix` there reads 0 findings, 0 type errors and no gap, every
engine, printing what it found. Main's
first-parent line gains that one commit per landing, and the branch's own history, its merges of main
included, stays one hop off it. The branch itself doesn't move, and the worktree is left at the landing,
detached. Nothing landed this way moves main during another's gate. A commit to main by hand does, and
when one happens mid-gate, land merges onto it and gates again. A merge that conflicts is refused with its
files named, and the worktree is left as it was, unless every conflicted file is under `docs/data`, which the
docs data generator writes whole: then land takes main's side of them, reruns the generator from the merged
sources in a scratch worktree, requires a second run to change nothing, and gates that (#y84h1te).
Every landing runs main's own land: when the worktree's `command/cohere-dev` differs from main's, land
says so and runs main's from the main checkout, against the worktree (#8wpk5tp), so a worktree on older
code can't land the old way. On a loaded morning a branch gated green eight times without landing, and
every one of those gates compiled a merge that never existed again.

A commit's subject reads `area: what changed (#task)`, up to 72 characters, and the story goes in the
body. The area is a folder in the tree, by its name or its path (`go_engine`, `release/dispatch`), or a
top-level file's name lowercased (`contributing`). Land refuses a landing whose commits break that, naming
each commit, before it waits for the lock: a merge needs no area, but is held to the length. The landing
commit's subject is `Land #<task>: <what landed>`: the task is the one the commits' subjects name, or
`--task <id>`, and what landed is the one commit's subject, or `--message` for a branch of several. Its
body lists the commits and says what the gate ran. Before this, 114 of main's last 300 first-parent
commits were merges like "Merge main into the work being landed", subjects averaged 105 characters, and
one area went by five prefixes.

Bisect main with `git bisect start --first-parent`. Main's first-parent chain is where a regression is
traced to the unit that brought it. Commits off it are the inside of a unit's branch, and some never
built alone: in cohere-go, 608feeca is the first half of a two-commit merge, and ff0a005e finishes it.
History is never rewritten to fold them, since rewriting moves every later sha that tasks and commit
messages cite, so a commit that doesn't build is skipped with `git bisect skip` instead. Before cohere-go
landed, each of its 136 first-parent commits since main was vetted through the pool, packages and tests
(#ry0vkmq), with 608feeca as the control that showed the check fails. 134 pass. Two don't, and the commit
after each builds: bc0b576a (a merge that missed one argument to `load.Packages`, fixed by 2c807fdc) and
bdc5c078 (format's doc builders taking `Doc` before their callers did, fixed by a612b6a6). Skip both.

Work starts in a worktree from `go run ./command/cohere-dev worktree <name>`, which prints its path,
`../cohere-wt-<name>` beside the checkout of main, and makes it at main the first time, TypeScript a
symlink to main's. After that it moves the same worktree to main with `git switch`, refusing one with
changes or commits main does not hold, so the files that didn't change keep their modification times
and Go's test cache keeps its hits: a fresh worktree's first fast run found 45 of 95 packages cached and
took 65s, a reused one 79 and 28s. A name is per hand, not per member, so a lead fanning out several
hands at once gives each its own, such as `system_cohere_tools_1`. `--branch <branch>` starts a branch
in it, and `worktree remove <name>` retires one, under the same refusals.

The Go build cache is held under a cap, 64 GB unless `COHERE_DEV_CACHE_GB` says otherwise (#jc6ca7r).
When a token's run ends, at most once every ten minutes for the machine, `cohere-dev` looks at the
cache in the background, and when it is over the cap it takes every token, trims the least recently
used entries to three quarters of the cap, and gives the tokens back. `cohere-dev status` shows the
last look. Go alone keeps entries for five days, and the house refilled 200 GB in two and a half hours.

A gate run never passes `-count=1`, and the wrapper refuses one for the whole module. Go's test cache
skips every package whose inputs did not change, and after a one-file change `-count=1` cost 81% more
CPU for the same verdict (1,180 against 2,136 core-seconds). A flaky test is fixed rather than re-run
around; name its package to re-run it alone.

## Taking a Go tree to zero

Cohere's own Go went from 37,889 findings to 0 in two days, and this is how, so another tree can do it the same
way. The names, the line lengths and the file layout went through typed codemods, written once and checked to
the end. What was left went through waves of light agents, each holding one package's judgment.

**The codemods are in `tools/go_codemods`.** Every rename goes through the rename engines, a library
in `internal/go_rename`: `rename` (one package) and `global_rename` (across packages). A rename is accepted only if every identifier still resolves to
the same object under the new names, checked to a fixpoint. The rewrite is reloaded in memory and compared
before anything is written, and checked again from disk. A name someone else owns (a field an encoder reads by
name, a method an interface of another package requires, an export outside the repository) is found by type and
refused, and the refusal is listed with its reason. The passes:

- `test_names`, `error_names`, `context_names`, `receiver_names`, `loop_names`: the handle of a test, an error
  named for what produced it, a context for where it came from, a receiver spelled from its type, a loop variable
  for what it holds.
- `word_names`, `initialisms`: the abbreviation vocabulary in `policy/Abbreviations.json` and initialisms
  written as words. `-global` reaches across packages. A word with several meanings (`pos`) is listed for a
  person, never given its first.
- `named_renames`, `hand_names`: a person's chosen names, one declaration a line, through the same engines.
- `octal_prefix`: each literal the octal-prefix rule flags, `0644` written as `0o644`, through the same reload,
  every literal of a rewritten file proven to keep its constant value.
- `line_wrap` (120 columns, each file proven to keep its syntax tree and comment words; a bare `cohere` runs the
  same wrap, `internal/line_wrap`, on the long-line rule's findings once the types load sound, never touching a
  comment or taking a file past the long-file limit), `type_split` (one type
  with methods to a file, each file named for it), `folder_names` (a package folder moved with its clause, its
  import paths and every qualifier).

`tools/go_codemods/house_names.sh` runs the naming passes in the order they landed, each built from the tree it
runs over. A branch runs it before merging main, so the two sides spell every name the same way, and again after
the merge. It writes nothing on a tree that already has the names.

**Each mechanical pass is one commit**, proven three ways. A census before and after, keyed by path, rule and
message, shows what the pass removed, and a pass that creates any finding is refused. The tests of every package
it touched pass. The bare run at the root says the same as the census. Rename passes go first, then the wrap,
since a longer name makes a longer line, then the type split.

**What stays judgment goes in waves.** Single letters, whose name depends on what they hold, dropped errors,
whose cure depends on what failing means there, and the rest of the correctness rules have no autofix.

- Brief one light agent whole, on an ordinary package, not the hardest: the goal and why, the exact files, the
  done-when, what not to touch, and that stopping with a reason is a good answer.
- Read its diff, and fix the brief where it was wrong.
- Then send the rest at once, one package or one set of files each, so no two agents touch a file. Each works in
  its own worktree off the branch and commits there. A codemod hand hands back its codemod's head instead, and the
  lead re-runs it on the branch.
- The lead reads every diff, merges, runs `house_names.sh` and the tests, and censuses again. An agent's report
  is relay; the diff is the record.

A whole class of findings sometimes has one cure, and then it isn't 400 judgments. A command's dropped
`fmt.Printf` errors are cured by writing to `internal/streams`, whose buffered writes can't fail partway and whose
delivery `streams.Finish` checks once at exit, the way `command/cohere-dev` does.

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
  tree, and a restore that did nothing looks exactly like one that worked. Cohere with `cmp`.

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
them no program. `cohere` has the program. Reproducing a workaround for a constraint we do not have is
not fidelity.

Where a rule has no tree exposure and the upstream gives no reasoning, fidelity is the only available
authority, and your own sense of what the rule should do is the thing most likely to be wrong. Record
the intuitive reading beside the actual one so the next reader does not correct it back.

## The dispatcher

Rules are compiled in rather than loaded, which is what makes them free to run. The cost is that
adding a rule means rebuilding, so `command/cohere-dispatch` pays that cost automatically: it names
the binary for the checkout's HEAD commit, looks for `.cache/cohere/bin/cohere-<platform>-<hash>`,
and execs it when present or builds it first when absent. A new commit costs one rebuild; every run
after it is a stat and an exec.

**It builds from the committed tree, never the working tree.** The checkout is shared, and a gate
that built from disk ran whatever any member had half-written: on 2026-10-02 one member's
uncommitted rule work turned 22 findings on in ahra before it was classified. So a build extracts
HEAD with `git archive`, extracts the compiler at the commit HEAD pins, and builds that. The
compiler is kept under `.cache/cohere/compiler/`, one per pin, because it is 66,000 files. Before a
binary is cached, its own `--version` must name the commit, or it is refused.

```sh
cohere                    # build HEAD's committed tree if it has no binary yet, then exec
cohere --dev              # build the working tree instead, uncommitted edits (anyone's) included
cohere --dispatch-verbose # say so when a rebuild fires
```

A default run's output is cohere's alone. While the launcher builds, a terminal shows one dim line that
clears before cohere prints, and a pipe or a file gets nothing; what a prune or a trim removed goes to
`.cache/cohere/prune.log`. `--verbose`, which the launcher reads on its way to cohere, or
`--dispatch-verbose` print each step and each removal in full. A failure always prints.

Anything the dispatcher does not own is forwarded to the real binary untouched.

`--dev` is the explicit way to run uncommitted work, and it says so on every run. It builds to a
stable path because Go caches package compilation but not linking, so a hash-named binary is a new
filename and therefore a full link on every change. Measured on a comparable binary: a leaf rule edit
costs 2.03s, while a genuinely unchanged tree at a stable path costs 0.29s. It records its hash beside
the binary and compares it, so a stable name never means a stale binary. `go run ./command/cohere`
in your own checkout works too, and its `--version` reports the uncommitted changes.

The build flags are fixed at `-trimpath -ldflags="-s -w"`. They make the link marginally faster and
the binary 29% smaller, and `-trimpath` is a build-input change rather than a link flag: turning it
on invalidates the entire compile cache, measured at 33s on a warm 2.0 GB cache. Flipping it per run
would pay that repeatedly, so it does not vary. The launcher builds through the same Go cache every
`go build` and `go test` on the machine uses (#3kr3x59). The house sets `GOFLAGS=-trimpath`, so a
member's test run compiles the packages the launcher's next build needs, and a private cache only
stored them twice: it had reached 40 GB beside the default cache's 81 GB. The launcher still builds
with cgo off, which compiles only the few packages that use cgo differently, measured at 46 MB.

**Go does not bound the cache, so cohere-dev does.** Go trims by age alone, five days
unused, and developing cohere filled the default cache far faster: on 2026-10-03 it regrew 223 GB in
nine hours and filled the disk (#3sgjy0h), most of it `TestEveryReleaseTargetCompiles` building cohere
for six platforms on every `go test ./...`, until `3dc6e70` made it type-check them instead.
`cohere-dev` holds the cache at a 64 GB cap (`command/cohere-dev/cache.go`): when a token's run ends,
at most every ten minutes for the machine, it measures the cache in the background, and when it is over
the cap it holds every token of the pool and trims the least recently used entries down to a 48 GB
target. The layout and the delete live in the `go_cache` module, which Loom shares. What each look did
is logged to `gocache-trim.log` beside the pool's tokens.

**No trim removes an entry used in the last two hours**, so a burst can stand above the cap by what it
used inside that window. Go marks an entry used at most once an hour, so an entry a build looked up a
moment ago can carry a time almost an hour old; `go_cache` refuses any spare under that hour plus an
hour of margin. Holding the pool keeps out only the builds that go through it, and removing an entry a
build outside it still reads fails that build with `could not import ... no such file`, which reads as
a compile error. A gate that trimmed young entries to hold a burst under the cap did exactly that. About
28 GB of the cache is used each hour under the house's load, so the cap sits above the two hours' worth
the trim can never take.

**A missing binary is a loud error, never a fallback.** With no Go toolchain the dispatcher runs the
binary shipped for the platform, and if there is none it exits non-zero naming the platform and
everywhere it looked. There is deliberately no path where it execs something that might do nothing:
the gate this tool replaces printed green over zero files for days because a resolver found no
binary, fell through to a bare command name, and an empty file list is indistinguishable from a
clean tree.

cohere decides what changed from content hashes and reads ignore files itself, so its own code starts
no git process. Two exceptions sit outside the checking: the dispatcher extracts the committed tree
with `git archive`, and SwiftPM clones and describes a Swift package's dependencies.

## Releasing

`cohere` reaches a machine as an npm install. One thin dispatcher package resolves a per-platform
binary package, the same shape oxlint and tsgo use:

```
@system-inc/cohere                 the package you install; a Node launcher, no binary
@system-inc/cohere-darwin-arm64    ~43 MB Go binary, stripped
@system-inc/cohere-darwin-x64
@system-inc/cohere-linux-arm64     for CI and containers
@system-inc/cohere-linux-x64
@system-inc/cohere-win32-arm64
@system-inc/cohere-win32-x64
```

```sh
pnpm add -D @system-inc/cohere
```

The command is still `cohere`, with `c` beside it: npm's `bin` names the command, not the package.
The scope is there because the bare name `cohere` is taken on npm. The platform packages carry the
dispatcher's name as a stem rather than a scope of their own, the shape biome and rollup use,
because `@system-inc` holds other tools and `@system-inc/darwin-arm64` would not say whose binary it
is. Each one stages in a directory named for its package without the scope, so `dist/` reads the
same way `node_modules/@system-inc/` does.

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
go run ./command/cohere-release --version 0.1.0 --output dist
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

`cohere --version` reports the version, the platform, the Go toolchain, and the vendored compiler as
`owner/name@commit`, so a bug report names what was running rather than "latest". The stamps go in
at link time; an unstamped local build says `dev` and says why.

The compiler's repository is stamped rather than written down, because it has already moved once:
the pin was against `microsoft/typescript-go` until that repository was archived, and is now against
`microsoft/TypeScript`. Both spellings are forty hex characters and resolve in different places, so a
hardcoded label would have survived the migration while quietly becoming false.

Each released binary carries the Go standard library's analysis results, so a project's first Go run
reads them instead of computing them (task #p1r7v9v). The table is generated per target at release
time, by a binary of the build machine's own stamped with the release's version, commit and Go
toolchain, and compiled in with `go build -overlay` in place of the committed
`internal/go_engine/standard_library_table/table.bin`, which stays empty, as does a local build's. It
holds go1.27.0 through go1.27.3, each with `CGO_ENABLED` 1 and 0, about 2.2 MB per target. win32-arm64
ships none, since no runner here can verify one. `cohere go-engine verify-std-facts` recomputes every
variant on the target itself: the release job runs it for darwin-arm64 and, under Rosetta, darwin-x64,
`swift-on-intel` for darwin-x64 natively, `windows` for win32-x64, and `clean-machine` for darwin-arm64,
linux-x64 and linux-arm64, after its proof that the install needs no Go.

### Versioning and the changelog

cohere is semver, starting at 0.1.0, and `release.Build` refuses any version below that or not semver
(a prerelease like `0.1.0-rc.1` is allowed). Kirk set the start at 0.1.0 on 2026-11-03: the configuration
and the rule sets are still settling, which is what a 0.x says.

Below 1.0.0 the minor is semver's breaking slot. A minor (0.2.0) may do anything the list below gives a
major or a minor, and a patch (0.1.1) may only relax or fix. npm's caret already reads 0.x this way, so a project
on `^0.1.0` takes 0.1.x patches and never a 0.2.0 it didn't choose.

From 1.0.0, what each part means, as Kirk set it:

- **Major** breaks a configuration or a command line that worked: a key, a flag or a set name removed or
  reinterpreted, so an existing `CohereSettings.json` or script no longer means what it did.
- **Minor** can fail a project that passed: a rule turned on in a set, a severity raised, a rule's
  options changed, a setting like an ignore pattern changed. A project takes that by taking the minor.
- **Patch** can only relax or fix: a rule turned off, a severity lowered, a bug fixed.

The rule sets are where most of a release's visible change lives, and they can be read exactly, so their
part of `CHANGELOG.md` is generated:

```sh
go run ./internal/release/tools/changelog --version 0.2.0 --previous 0.1.0
```

That diffs every set between the tag `v0.1.0` and `HEAD` and prints the release's section, with one
heading per changed set listing each rule turned on or off, each severity and option change, and each
other setting that moved. It also refuses a version smaller than the diff allows: a set that turned a
rule on can't ship as a patch, and a set that was removed can't ship without a major, or below 1.0.0
without a minor. The section goes
above the last one in `CHANGELOG.md`, and what changed in cohere outside the sets is written under the
same heading by hand. Each release is tagged `v<version>`, which is what the next one diffs against.

The Swift engine's contract is not a compatibility surface. The front door and `cohere-swift` ship in
one package, and the release refuses to stage a pair that speak different contract versions, so a
contract change needs no version of its own.

### Releasing

A release is a tag, and only a tag. Main moves many times a day and an npm version is permanent, so a
merge never publishes anything. Kirk set it this way on 2026-11-03:

1. **Prepare.** When main holds enough to ship, the changelog tool, run against the last tag, says the
   smallest version the change allows, and the release's section goes into `CHANGELOG.md`.
2. **Decide.** Kirk reads the section and says go.
3. **Tag.** `v<version>` goes up on a commit main contains. `release.yml` runs on it: the module
   check, the six platform packages, every proof, then npm, then the GitHub release, then
   `consumer.yml`, which installs the version from the registry with npm and pnpm on fresh runners. A
   tag main doesn't contain is refused before anything builds.

A tag with a prerelease suffix, `v0.2.0-rc.1`, runs the whole thing and publishes nothing, and so does
the button: `gh workflow run release.yml -f version=0.2.0`.

**Keys.** The steady state stores none. The GitHub release and the attestations use the run's own
token. npm uses trusted publishing: GitHub proves to npm which repository and workflow are running, so
no npm key exists to leak, and provenance comes with it. The VS Code extension is the exception: opted
in with the repository variable `PUBLISH_EXTENSION=true`, it needs `VSCE_PAT` and `OVSX_PAT`, and the
Marketplace's expires.

**0.1.0 was published by hand,** because npm can only trust a workflow for packages that already exist.
Its tag run (41766910807) built and proved everything, then a granular token with Bypass 2FA was
refused with `EOTP`: npm is retiring such tokens for direct publishing. The maintainer published that
run's own tarballs from a laptop, platform packages first and the dispatcher last, after checking them
against `SHA256SUMS` and the run's attestations. So 0.1.0 alone has no npm provenance; its binaries
still verify against the GitHub release and the build attestations. The token is revoked and
`NPM_TOKEN` is gone.

**The lock.** On a machine where agents act through the maintainer's GitHub account, nothing GitHub
enforces can tell the maintainer from an agent. npm's staged publishing can. Without `NPM_TOKEN`,
`release.yml` stages each package with `npm stage publish` over trusted publishing, and a package goes
live only when a maintainer approves it on npm with 2FA, which a workflow token cannot do. Until then,
the GitHub release waits as a draft. Once every package is approved, run
`gh workflow run consumer.yml -f version=<version>`. It proves the install from the registry, publishes
the draft, and then runs the three verification commands from the README against the public release.

**Trusted publishing** is set on all seven packages, stage-only, so the workflow can stage a version
but never put one live. It was set with npm 11.15.0 or later, which `npm trust` needs, signed in with
2FA, once per package:
`npm trust github @system-inc/<package> --file release.yml --repo system-inc/cohere --allow-stage-publish`.
`npm trust list @system-inc/<package>` shows it. A package added later needs the same line once it
exists on npm. `npm stage publish` takes
`--provenance`, and the workflow passes it, but npm's docs don't say an approved version keeps it, so
the first staged release checks with `npm audit signatures` before the README's promise is trusted.

### The formatter carries no JavaScript

A released binary formats with cohere's native Go printers and embeds none of the Prettier fork's
bundles, so `--version` names no formatter: there are no bytes in it for a stamp to vouch for. The
bundles remain in `internal/format/prettier` as the test oracle the printers are measured against,
and `prettier.DigestBundles` keys the oracle's cache so a rebuilt fork invalidates every cached
answer rather than comparing against output from bundles nobody loads.

`COHERE_BINARY=/path/to/cohere` points every `cohere` on the machine at a local build. A broken
override is fatal rather than a fallback, even when a good install is sitting right there: someone
who sets it has stated which binary they want, and quietly running a different one would hand them
results they would read as their own build's.

### macOS signing

Measured, because the answer decides how much this matters. An unsigned binary with no quarantine
attribute runs normally, and that is what an `npm install` produces — package managers extract
tarballs without setting `com.apple.quarantine`, so consumers installing from the registry are not
blocked. The same binary _with_ quarantine set is killed by the kernel: exit 137, SIGKILL, and zero
bytes on both stdout and stderr.

That silent kill is the reason to sign. It is reached whenever the binary travels as a file rather
than as a package — a release asset from a browser, a binary copied out of CI — and it fails in the
worst available way, with no output to explain it. The launcher's signal handling turns it into a
loud 137 rather than a green nothing, and signing removes it entirely.

Signing is wired and credential-gated. It needs a **Developer ID Application** certificate
specifically; an Apple Development certificate signs successfully and is then rejected by the notary
service at the end of a release. Staging without credentials is supported and says so in its own
summary, because an unsigned release is fine and an unsigned release that looks signed is not.

Releases sign as "Developer ID Application: Kirk Ouimet (6YC8868VS9)", from the secrets
`MACOS_CERTIFICATE` (the certificate, its key and Apple's G2 intermediate, as a base64 `.p12`),
`MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY` and `KEYCHAIN_PASSWORD`. They notarize with an
App Store Connect team key with the Developer role: `APPLE_API_KEY` (the `.p8`), `APPLE_API_KEY_ID`
and `APPLE_API_ISSUER`. A key reaches only that team. An Apple ID with an app-specific password would
also work, and would carry the whole account's iCloud mail, contacts and calendars with it, so it isn't
used. The certificate expires on 2027-12-03.

Note that a notarization ticket cannot be stapled to a bare executable — only to a bundle, a disk
image, or an installer package — so a notarized Mach-O is validated by an online check on first
launch. A machine that is entirely offline the first time it runs a quarantined `cohere` is still
blocked. The npm path does not set quarantine, so this does not affect it.
