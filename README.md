# cohere

cohere type-checks, lints, fixes and formats a TypeScript codebase in one process. The TypeScript
compiler builds the program once, and that one type graph is what TypeScript's own diagnostics and
every lint rule read, the rules in one walk of each file. The fix engine and the formatter run in the
same process. Fixes re-read each file from disk and re-lint it until they stop landing, so they never
apply to a stale copy. The formatter parses each TypeScript file it formats with the same TypeScript
parser, and prints every file it formats with cohere's own printers. Keeping the rules on one graph in
one walk is what lets a codebase carry hundreds of them without each one costing another pass over the
code.

## Install

```sh
pnpm add -D @system-inc/cohere
```

npm and yarn work the same way. The package installs two commands, `cohere` and its short form `c`,
and pulls in one prebuilt package for your platform: macOS, Linux or Windows, each on arm64 or x64.
No Go toolchain is needed. If no binary matches your platform, `cohere` exits with an error naming
the platform rather than doing nothing.

## Swift, on macOS only

A directory with a `Package.swift` at its root is checked as a Swift package, by `cohere-swift`, an
engine the two macOS packages ship beside `cohere`. It runs on macOS 15 and later, and types the
package with the Swift toolchain already on your machine. On Linux and Windows there is no Swift
engine, and `cohere` refuses a Swift package by name and exits 1 rather than checking nothing.

## Running it

Run it from the project root, where `tsconfig.json` and `CohereSettings.json` live.

```sh
cohere                  # type-check and lint, and apply every available fix
cohere --fix --format   # apply fixes and format, running no other phase
cohere --lint           # report lint findings only: no fixes, no TypeScript diagnostics
```

A bare `cohere` writes fixes to your files but does not format them; formatting runs only when you
pass `--format`. To see what would change without writing anything, add `--no-fix`.

A run prints the files it rewrote, then its findings, then one line:

```
🪄💅 app/os/SessionRow.tsx      prefer-const ×2, prefer-nullish-coalescing
  💅 modules/pensieve/Recall.ts
app/os/Session.ts:12:5 error nexus/consistency-no-abbreviated-identifier `ctx` is an abbreviation.
✗ ☠️ 0.8s • 1 finding (480 rules • 3.9K files • 2.4M nodes)
```

See [Output](#output) for what that line says, and for `--verbose` and `--json`.

## CohereSettings.json

cohere reads `CohereSettings.json` at the project root. The house rules ship inside cohere as rule
sets, and the formatting options come with `cohere:typescript`, so a minimal setup is one file:

```json
{
    "extends": "cohere:typescript",
    "rules": { "no-debugger": "error" },
    "ignorePatterns": ["dist/**"]
}
```

A React and Next.js project composes the sets it uses, each of which sits on `cohere:typescript`:
`"extends": ["cohere:react", "cohere:next", "cohere:tailwind"]`. A rule belongs to one set, and a rule
two sets that do not extend each other both configure is an error naming both.

A project with a tier of its own extends a file instead, and that tier, `NexusCohereSettings.json`,
holds the format block:

```json
{
    "format": { "tabWidth": 4, "singleQuote": true, "printWidth": 120 }
}
```

The keys:

- `extends`: a rule set cohere carries (`cohere:<name>`), or a path to another settings file, or a list
  of them, applied first and in order, so a shared base can hold most of the configuration.
- `rules`: each rule's severity, `"off"`, `"warn"` or `"error"`, with options where a rule takes
  them.
- `overrides`: a list of `{ "files": [...], "rules": {...} }` blocks that change rules for matching
  paths.
- `ignorePatterns`: paths cohere never checks.
- `departures`: for each rule this file sets differently from the file it extends, the reason why.
  cohere reports them, so a departure stays visible rather than becoming a quiet exception.
- `format`: the formatter's options. It belongs only in the Nexus tier, `cohere:typescript` or a
  `NexusCohereSettings.json` of your own, so every
  project that extends it formats the same way; a `format` key in any other settings file is an
  error that names the file. It accepts `printWidth`, `tabWidth`, `useTabs`, `semi`,
  `singleQuote`, `trailingComma`, `bracketSpacing`, `bracketSameLine`, `arrowParens`, `endOfLine`,
  and `ignore`, a list of paths the formatter leaves alone. An option outside that list is an error,
  not something silently ignored, and
  `--format` refuses to run when your settings do not extend a Nexus tier holding a `format` block. Linting and fixing need no such file.

The full reference, every key with an example, is [schema/CohereSettings.md](schema/CohereSettings.md).
It is generated from the loader itself, along with two JSON schemas an editor can validate against
through a `"$schema"` key: [schema/CohereSettings.schema.json](schema/CohereSettings.schema.json) for a
project's file and [schema/NexusCohereSettings.schema.json](schema/NexusCohereSettings.schema.json) for
the Nexus tier. Both ship in the package, so the path works offline and matches the installed cohere:

```json
{
    "$schema": "./node_modules/@system-inc/cohere/schema/CohereSettings.schema.json"
}
```

`cohere --rules` lists every rule cohere implements for your project's language, and
`cohere --rules-enabled` lists the ones your settings turn on for one file (`index.ts` at the project
root unless you name another).

## Flags

| Flag | What it does |
| --- | --- |
| `--fix` | apply fixes only, running no other phase; add `--format` to format as well |
| `--fix-passes N` | how many times a file may be re-linted while fixes keep landing (default 10) |
| `--no-fix` | write nothing to your source; report what would change |
| `--format` | format the files not on record as formatted, or the paths you name; with `--no-fix`, report them |
| `--format-all` | format every file, not only the ones not already on record as formatted (implies `--format`) |
| `--format-only` | format only, proposing no fixes and running no other phase (implies `--format`); with `--no-fix`, the commit gate's format check (see [Before you commit](#before-you-commit)) |
| `--lint` | run the lint rules only, reporting what they find without fixing it, and without TypeScript's diagnostics |
| `--types` | report TypeScript's diagnostics only, running no rules and fixing nothing |
| `--unused` | report code that is never used: unreferenced exports and unreachable statements |
| `--unused-all` | list the unused findings already marked `cohere-keep`, not only count them (implies `--unused`) |
| `--unused-deep` | also group the unused code into islands by what reaches what (implies `--unused`) |
| `--directory PATH` | the project root (default: the nearest `tsconfig.json` or `Package.swift` above you) |
| `--tsconfig PATH` | the tsconfig that defines the program (default: the nearest `tsconfig.json`) |
| `--lint-config PATH` | the settings file, relative to `--directory` if given, else to where you run it (default: `CohereSettings.json` at the project root) |
| `--no-cache` | read and write no cache, so every phase computes from source |
| `--stdin-filepath PATH` | with `--fix`, read one file from stdin and print the fixed text, writing nothing |
| `--explain FILE` | report what every rule did on one file, and why it ran or did not, writing nothing |
| `--print-config` | print each rule's resolved severity and options for one file (`index.ts` unless you name one), as JSON, and exit |
| `--rules` | print the rules cohere implements for your project's language, and exit |
| `--rules-enabled` | print the rules your settings turn on for one file (`index.ts` unless you name one), with severity, and exit |
| `--verbose` | print everything a run can say: each phase, the coverage summary, overrides, skips, notes, memory and the total |
| `--phases` | put where the time went (graph, fix, format, types, lint) first in the footer's parentheses |
| `--json` | print newline-delimited JSON for a program to read instead of the human view (see [Output](#output)) |
| `--coverage` | name every rule under the coverage fact that describes it, not only count them |
| `--timing` | report what building the graph and each rule cost, most expensive rule first |
| `--single-threaded` | use one type checker instead of several |
| `--profile FILE` | write a Go CPU profile of the run to FILE |
| `--cache-dump` | print what this project's cache holds, and exit |
| `--version` | print the version, what this binary was built from, and the Swift contract it speaks, and exit |

## Output

A run prints three things, in order:
- **The files it rewrote.** Each line has a 🪄 if fixes were applied and a 💅 if it was formatted, then
  the path, then the rules whose fixes it took, with a count past one. Past 20 files it says how many
  more, and `--verbose` lists them all.
- **Its findings,** one per line, as `path:line:col severity rule message`.
- **One footer line:** the verdict (✓ 💎 or ✗ ☠️), how long the run took, what it found, and in the
  parentheses the rules that ran, the files in scope and the syntax nodes it walked.

```
✓ 💎 0.7s (480 rules • 3.9K files • 2.4M nodes)
✗ ☠️ 0.8s • 1 type error • 2 findings (480 rules • 3.9K files • 2.4M nodes)
```

Anything the run did not check is in the footer even when it passes, so a green line never hides a
gap: `✓ 💎 2.4s (…) • ⚠ 1 file crashed`. The same goes for a phase that could not run, a rule that
skipped every file, formatting not checked, and a run narrowed to some of the files.

On a terminal the verdict and time are bold, what was found is red, and the parentheses are dim. A
pipe, a file or `NO_COLOR` gets no color codes at all.

`--phases` puts where the time went first inside the parentheses: 🕸 building the graph (read, parse,
bind), 🪄 fixing, 💅 formatting, 🔷 the type check, 👑 lint, and 🧹 unused when `--unused` ran. A phase
that did not run is left out. To make that a project's default, set it in `CohereSettings.json`:

```json
{ "output": { "phases": true } }
```

`--timing` is separate and still prints what each rule cost, which adds overhead; the phase times are
free.

`--verbose` prints everything a run can say: each phase and why any did not run, the coverage summary,
overrides, skips, notes, memory and the total. Its footer also says how many files this run checked
fresh against how many the cache answered for, or that the run was replayed whole.

`--json` is for a program to read. It prints newline-delimited JSON, one object per line, each with a
`kind`: a `finding` per finding, a `fixed` and a `formatted` per rewritten file, and a `summary` last.
The summary carries a `schemaVersion`, the verdict, the timings and counts, and `gaps`, everything the
run did not check, which a program deciding whether to trust a passing run should read too.
[schema/CohereOutput.schema.json](schema/CohereOutput.schema.json) describes every field. Read
`--json` rather than the human view, whose layout can change.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | no problems found |
| `1` | it found problems, or cohere itself could not run, for example on a settings error |
| `2` | the command line was wrong, such as an unknown flag |

`1` covers both findings and failures, so in CI the output says which one it was.

## Before you commit

```sh
cohere --no-fix --format-only --format-all
```

This is the commit gate's format check, and its exit code answers one question: would formatting change
any file, in this repository or in the nested repositories (declared submodules) it reads? It runs no
fixes, no type check and no lint, so a lint finding has no say in it.

- `0`: no file would change, here or in any nested repository.
- nonzero: each file that would change is a finding, a nested one naming its repository. So is each file
  the formatter could not read, such as one that does not parse, and a format walk that failed. A run that
  could not check a file never exits `0`.

Only declared submodules are read. A git repository inside yours that `.gitmodules` does not name (a
clone in an ignored directory, say) is skipped and nothing in it is checked: the scope line lists it
under `skipped nested repositories`, and `nested repositories:` does not count it. Declare it as a
submodule, or run the gate inside it.

Drop `--format-all` to check only the files not on record as formatted at their current bytes, or name
paths to check only those. Without `--no-fix`, `cohere --format-only` formats and writes nothing else.

## Caching

cohere keeps its cache in `.cache/cohere` at the project root, so a run reuses what an earlier run
already established. When your tsconfig sets `incremental`, it also keeps TypeScript's build information
where the tsconfig says, and a `--no-fix` run leaves that file untouched. Add `.cache/` to your
`.gitignore`. `--no-cache` reads nothing from those caches and writes nothing to them; use it when
you suspect the cache, or to time a run from scratch.

## CI

```sh
cohere --no-fix
```

`--no-fix` makes the run read-only, so CI reports what a developer's run would have fixed instead of
fixing it on a machine nobody looks at. A nonzero exit fails the job. Add `--format` to fail on
unformatted files too. `--no-fix` does not stop cohere writing its own cache in `.cache/cohere`,
which is safe to persist between CI runs to speed them up, or to discard.

## Contributing

How cohere is built, and why, is in [CONTRIBUTING.md](CONTRIBUTING.md).
