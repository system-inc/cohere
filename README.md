# cohere

cohere type-checks, lints, fixes and formats a TypeScript codebase in one process, against one type
graph. The TypeScript compiler builds the program once, and every phase reads that same graph:
TypeScript's own diagnostics, the lint rules, their fixes, and the formatter. Keeping it to one
process and one graph is what lets a codebase carry hundreds of rules without each one costing
another pass over the code.

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
  `singleQuote`, `trailingComma`, `bracketSpacing`, `bracketSameLine`, `arrowParens` and
  `endOfLine`. An option outside that list is an error, not something silently ignored, and
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
| `--explain FILE` | report what every rule did on one file, and why it ran or did not |
| `--print-config` | print each rule's resolved severity and options for one file (`index.ts` unless you name one), as JSON, and exit |
| `--rules` | print the rules cohere implements for your project's language, and exit |
| `--rules-enabled` | print the rules your settings turn on for one file (`index.ts` unless you name one), with severity, and exit |
| `--coverage` | name every rule under the coverage fact that describes it, not only count them |
| `--timing` | report what each rule cost, most expensive first |
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

## Caching

cohere keeps its cache in `.cache/cohere` at the project root, plus TypeScript's incremental build
information, so a run reuses what an earlier run already established. Add `.cache/` to your
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
