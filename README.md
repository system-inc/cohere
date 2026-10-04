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
cohere                  # type-check, lint, apply every available fix, and format, in one call
cohere --fix            # apply fixes and format, running no other phase
cohere --no-fix         # write nothing: report what fixing and formatting would change, and exit on it
cohere --lint           # report lint findings only: no fixes, no TypeScript diagnostics
```

A bare `cohere` formats the files not on record as formatted at their current bytes, so a warm run
formats what you edited and nothing else. Add `--no-format` to any of the first three to leave
formatting out.

## CohereSettings.json

With no settings file, cohere applies the house stack, each set where the code shows it fits:

- `cohere:typescript` on every file.
- `cohere:react` on each file that imports `react` or a `react-` package, or contains JSX.
- `cohere:next` on each file that imports `next`, and, once anything does, on Next's own files: everything
  under `app/` and `pages/`, and `middleware`, `instrumentation` and `next.config`.
- `cohere:tailwind` on every file when the root stylesheet the Tailwind rules find (`app/globals.css` and
  the others) imports `tailwindcss`. Without one, its rules are skipped by name.

package.json is never the evidence: a dependency the code never imports applies nothing. The run's
`sets:` line names each set and why. The house format applies too, so the first run rewrites files into
the house style.

To go your own way, write a `CohereSettings.json`. One that names no `cohere:` set keeps the detected
stack and applies its own rules and format on top:

```json
{
    "rules": { "no-continue": "off", "eqeqeq": ["error", "smart"] },
    "format": {}
}
```

An `"off"` there needs no reason, and `--coverage` names each one. A `format` block applies over
Prettier's defaults, so `{}` means Prettier's defaults. A file that names sets in `extends` chooses them
instead, and they apply to every file: `"extends": ["cohere:react", "cohere:next"]`.

A project that extends a `cohere:system-inc/*` set is one of ours, and stays strict: every off says why
under `reasons`, every departure from an inherited ruling says why under `departures`, and the format
comes only from the Nexus tier, so a `format` block anywhere else in the chain is refused.

The keys:

- `extends`: a rule set cohere carries (`cohere:<name>`), a path to another settings file, or a list of
  them, applied first and in order. A rule two sets that don't extend each other both configure is an
  error naming both.
- `rules`: each rule's severity, `"off"`, `"warn"` or `"error"`, with options where a rule takes them.
- `overrides`: a list of `{ "files": [...], "rules": {...} }` blocks that change rules for matching
  paths.
- `ignorePatterns`: paths cohere never checks, beyond what git ignores. Ignore matching follows
  gitignore(5): every `.gitignore` from the root down and `.git/info/exclude` (not `core.excludesFile`),
  case sensitive (`core.ignorecase` is not read). A `.gitignore` that is a symbolic link or larger than
  100 MiB is refused by name rather than skipped.
- `reasons` and `departures`: why this file turns a rule off, or sets it differently from the file it
  extends. Required in our tiers, optional elsewhere. cohere prints them, so a choice stays visible.
- `format`: the formatter's options, `printWidth`, `tabWidth`, `useTabs`, `semi`, `singleQuote`,
  `trailingComma`, `bracketSpacing`, `bracketSameLine`, `arrowParens`, `endOfLine`, and `ignore`, a list
  of paths the formatter leaves alone. An option outside that list is an error.

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
| `--fix` | apply fixes and format, running no other phase (`--no-format` to leave formatting out) |
| `--fix-passes N` | how many times a file may be re-linted while fixes keep landing (default 10) |
| `--no-fix` | write nothing to your source; report what fixing and formatting would change, and exit nonzero if anything would |
| `--no-format` | leave formatting out of a bare run, `--fix` or `--no-fix`: fix, type-check and lint only |
| `--format` | format the files not on record as formatted, or the paths you name; a bare run, `--fix` and `--no-fix` already do, so naming it there changes nothing |
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
| `--coverage` | name every rule under the coverage fact that describes it, not only count them |
| `--timing` | report what building the graph and each rule cost, most expensive rule first |
| `--single-threaded` | use one type checker instead of several |
| `--profile FILE` | write a Go CPU profile of the run to FILE |
| `--cache-dump` | print what this project's cache holds, and exit |
| `--version` | print the version, what this binary was built from, and the Swift contract it speaks, and exit |

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

`--no-fix` makes the run read-only, so CI reports what a developer's run would have fixed and
formatted instead of doing it on a machine nobody looks at. A nonzero exit fails the job, unformatted
files included. `--no-fix` does not stop cohere writing its own cache in `.cache/cohere`,
which is safe to persist between CI runs to speed them up, or to discard.

## Contributing

How cohere is built, and why, is in [CONTRIBUTING.md](CONTRIBUTING.md).
