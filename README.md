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
and pulls in one prebuilt binary for your platform: macOS, Linux or Windows, each on arm64 or x64.
No Go toolchain is needed. If no binary matches your platform, `cohere` exits with an error naming
the platform rather than doing nothing.

## Running it

Run it from the project root, where `tsconfig.json` and `CohereSettings.json` live.

```sh
cohere                  # type-check and lint, and apply every available fix
cohere --fix --format   # apply fixes and format, running no other phase
cohere --lint           # run the lint rules only, without TypeScript's diagnostics
```

A bare `cohere` writes fixes to your files but does not format them; formatting runs only when you
pass `--format`. To see what would change without writing anything, add `--no-fix`.

## CohereSettings.json

cohere reads `CohereSettings.json` at the project root. A minimal one:

```json
{
    "format": { "tabWidth": 4, "singleQuote": true, "printWidth": 120 },
    "rules": { "no-debugger": "error" },
    "ignorePatterns": ["dist/**"]
}
```

The keys:

- `extends`: a path to another settings file, applied first, so a shared base can hold most of the
  configuration.
- `rules`: each rule's severity, `"off"`, `"warn"` or `"error"`, with options where a rule takes
  them.
- `overrides`: a list of `{ "files": [...], "rules": {...} }` blocks that change rules for matching
  paths.
- `ignorePatterns`: paths cohere never checks.
- `departures`: for each rule this file sets differently from the file it extends, the reason why.
  cohere reports them, so a departure stays visible rather than becoming a quiet exception.
- `format`: the formatter's options. It accepts `printWidth`, `tabWidth`, `useTabs`, `semi`,
  `singleQuote`, `trailingComma`, `bracketSpacing`, `bracketSameLine`, `arrowParens` and
  `endOfLine`. An option outside that list is an error, not something silently ignored, and
  `--format` refuses to run when neither this file nor anything it extends has a `format` block.

`cohere --rules` lists every rule the binary implements, and `cohere --rules-enabled` lists the ones
your settings actually turn on.

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
| `--lint-config PATH` | the settings file (default: `CohereSettings.json` at the project root) |
| `--no-cache` | read and write no cache, so every phase computes from source |
| `--stdin-filepath PATH` | with `--fix`, read one file from stdin and print the fixed text, writing nothing |
| `--explain FILE` | report what every rule did on one file, and why it ran or did not |
| `--print-config` | print each rule's resolved severity and options for a file, as JSON, and exit |
| `--rules` | print the rules this binary implements, and exit |
| `--rules-enabled` | print the rules your settings turn on, with severity, and exit |
| `--coverage` | name every rule under the coverage fact that describes it, not only count them |
| `--timing` | report what each rule cost, most expensive first |
| `--single-threaded` | use one type checker instead of several |
| `--profile FILE` | write a Go CPU profile of the run to FILE |
| `--cache-dump` | print what this project's cache holds, and exit |
| `--version` | print the version and exit |

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
