# The engine contract

What an external engine prints and how the Go front door renders it. cohere-swift is the first
external engine; the TypeScript engine stays in process and never speaks this. The contract exists
so a Swift run reads exactly like a TypeScript run: same finding lines, same phase line, same
coverage notes, same accounting, same exit code. Someone who knows how to read one already knows
how to read the other.

The fixtures in `Contract/` are the agreement made executable. The Go renderer's tests and the Swift
encoder's tests both read them, so neither side can drift without the other's test failing.

| Fixture | What it pins |
|---|---|
| `Clean.jsonl` | Every phase ran or was reused. One file is excluded with its reason. Exit 0. |
| `Findings.jsonl` | A compiler warning with its group, and a rule finding whose message has a newline. Lint still runs after a warning. The binary was built from a modified tree. Exit 1. |
| `TypesBail.jsonl` | Under `--no-fix`, a compiler error cuts lint off as `notReached`. The run is incomplete. Exit 1. |
| `CrashWithoutSummary.jsonl` | The stream stops after the fix phase. The front door must say nothing was checked and exit 1. |
| `NothingChanged.jsonl` | `--changed` with nothing changed: no package described, every phase skipped, `0 files checked`, exit 0. |

## Why records and not text

The engine could print cohere's text lines itself. It does not, because the lines are the front
door's to keep. `edit.Summary.String` carries a comment about a refusal breakdown that drifted
away from the count it qualifies. `printRuleDiagnostic` carries one about message newlines that
silently dropped four findings. Each printer carries rules like these, learned the hard way. A
second implementation of those printers in Swift would have to relearn every one, and the two
would disagree the first time either changed. So the engine reports what happened as data, and
the front door feeds that data to the printers it already has. A `phase` record becomes a
`pipelineReport.record` call. A `fix` record becomes an `edit.Summary` printed by its own `String`.

## The transport

- **stdout:** one JSON object per line, UTF-8, nothing else. A line that does not parse as a
  record is a protocol error. The front door stops and says the engine is broken; it does not skip
  the line, because a skipped line is a finding nobody sees.
- **stderr:** prose for a human, passed through untouched. Notes that the TypeScript engine prints
  to stderr (a submodule it could not read, a file lost from scope) go here.
- **Records stream in pipeline order.** The front door prints each one as it arrives, so a slow
  types phase shows its fix line first, the way the TypeScript engine does.

## The invocation

The front door resolves the project root (the nearest directory holding `Package.swift` or
`tsconfig.json`, nearest wins), then runs:

```
cohere-swift --contract 1 --root <absolute package root> [flags] [paths]
```

`--contract` is the version the front door speaks. An engine speaking a different version refuses
the run, prints a sentence on stderr naming both versions, and exits 2. The first record the engine
writes is `provenance`, which carries its own `contract` field. The front door refuses a mismatch
there too, because the record and the flag are two independent ways for the versions to disagree.

## Records

Every record has `kind`. Fields not listed are not part of the contract and the front door ignores
them. Durations are integer milliseconds. Paths are absolute, as `sourceFile.FileName()` is for
TypeScript.

### `provenance`, always first

```json
{"kind":"provenance","contract":1,"engine":"cohere-swift","version":"0.1.0","commit":"<40 hex or dev>","sourceTreeModified":false,"toolchain":"swiftlang-6.4.0.34.1","swiftSyntax":"604.0.0","swiftFormat":"604.0.0"}
```

`sourceTreeModified` drives the same warning the TypeScript run prints: this binary was built from a
modified tree, so no commit reproduces these findings. `--version` prints exactly this record and
nothing else.

### `project`, the Swift counterpart of `graph built in`

```json
{"kind":"project","root":"/…/ahraos-macos","package":"AhraOs","elapsedMilliseconds":412,
 "filesInPackage":152,"filesOurs":149,"filesInScope":149,"scopeDescription":"",
 "targets":[{"name":"AhraOsCore","kind":"library","files":14,"languageMode":"6"}],
 "excluded":[{"file":"/…/Generated.swift","reason":"marked // @generated"}]}
```

- `filesInPackage` is every `.swift` file in a root target. `filesOurs` is the subset that is
  tracked by git and not excluded. Dependencies, including local ones like `Vendor/SwiftTerm`, are
  never counted, because they are not root targets.
- `filesInScope` equals `filesOurs` on a whole-package run. A run narrowed by named paths or
  `--changed` sets it lower and describes the request in `scopeDescription`, which the front door
  prints in the parenthesis of `N in scope (…)`.
- `elapsedMilliseconds` is the time spent describing the package. The accounting line bills it as
  the graph, because it is the step nothing can skip.
- Written on every run except one that finished before the package was described, which today is
  only `--changed` with nothing changed. That run's summary carries `nothingToCheck`, and the
  accounting line says no package was described, as it says `no graph was built` for TypeScript.

Rendered (whole package, then scoped):

```
package described in 412ms — 152 Swift files in the package, 149 of them ours
package described in 412ms — 152 Swift files in the package, 149 of them ours, 3 in scope (Sources/AhraOsIpc)
```

Each excluded file prints as a coverage note under the lint line: `  not checked: <file> (<reason>)`.

### `finding`

```json
{"kind":"finding","source":"rule","file":"/…/ByteRing.swift","line":159,"column":61,"endLine":159,"endColumn":62,
 "severity":"error","rule":"cohere-swift/no-force-unwrap","messageId":"forceUnwrap",
 "message":"…why, not only what…","fixes":[],"suggestions":[]}
```

- `source` is `compiler`, `rule`, or `format`.
- `line` and `column` are 1-based. Columns count UTF-8 bytes, which is what the TypeScript printer
  already prints (`GetECMALineAndByteOffsetOfPosition`) and what swift-syntax, sourcekitd and the
  compiler's serialized diagnostics all use. `endLine` and `endColumn` are exclusive and optional.
- `severity` is `error` or `warning`. Both are findings: both count toward the exit code. The
  severity is kept so the line can say which one the compiler called it.
- `fixes` hold edits as `{"start":<byte>,"end":<byte>,"text":"…"}`, using 0-based UTF-8 byte
  offsets into the file as the engine read it. The engine applies fixes itself, in its own fix phase.
  Fixes in a record describe what a `--no-fix` run withheld; they are never instructions for the front door.
- `suggestions` are `{"message":"…","fixes":[…]}`: repairs offered and never applied.

Rendered, one finding per line. Newlines in `message` are collapsed to spaces, as
`singleLineDescription` does:

```
/…/ByteRing.swift:159:61 - force unwrapping crashes on nil … [cohere-swift/no-force-unwrap/forceUnwrap]
/…/Broken.swift:3:23 - error: cannot convert value of type 'String' to specified type 'Int'
/…/Warning.swift:2:9 - warning: variable 'neverMutated' was never mutated; consider changing to 'let' constant [#VariableNeverMutated]
/…/Unformatted.swift:12:1 - not formatted as .swift-format says [cohere-swift/format/notFormatted]
```

The compiler line is the TypeScript shape `file:line:col - error TS2322: message`. Swift
diagnostics have no number, so the compiler's warning group takes the place of the code, written
the way swiftc writes it, `[#Group]`. A compiler finding with no group has no bracket. For compiler
findings, `rule` holds the group name without the `#`, or `""`.

### `fix`, the fix and format phase's summary

```json
{"kind":"fix","filesConsidered":149,"filesRewritten":2,"fixesApplied":5,"fixesRefused":1,
 "refusalsByReason":{"overlaps another fix":1},"filesReformatted":2,"filesNotFormatted":0,
 "notFormattedReasons":{},"formatScope":"changed files (working tree against HEAD): 2"}
```

The front door fills an `edit.Summary` from it and prints that summary's `String()`, then
`format scope: <formatScope>`. Fixes and formatting are separate counts, as they are for
TypeScript. A file the formatter declined (no `.swift-format` at or above it, for one) counts in
`filesNotFormatted` with its reason. A formatter that declined everything therefore never reads
like a formatter that found everything already correct.

### `types`

```json
{"kind":"types","diagnostics":1,"files":149,"elapsedMilliseconds":2187,
 "filesWithoutRecord":[],"build":"swift build, scratch ~/Library/Caches/cohere/swift/<hash>"}
```

Rendered as `types: 1 diagnostics over 149 files in 2.187s`. Each entry in `filesWithoutRecord`
prints `  types: no compiler record for <file>, so its diagnostics are unknown`, and any such file
makes the run incomplete. The types phase reads each file's serialized diagnostics (`.dia`), not
the build's stdout, because an incremental build does not reprint warnings in files it did not
recompile. So a file with no `.dia` is a file the engine cannot vouch for.

**Only compiler errors cut lint off.** In TypeScript every diagnostic the checker reports is an
error, so "any type diagnostic bails" and "any type error bails" were the same rule. In Swift they
are not. A warning (`variable was never mutated`) is a finding and fails the run, but the code
still means what it says, so lint runs and its findings are not noise. An error means the semantics
are wrong, so lint is recorded `notReached` with the detail
`types bailed: N type errors — lint findings against wrong semantics are noise`.

The types phase always covers the whole package, even on a scoped run. In Swift a change in one
file changes what other files in the module mean, and the compiler checks the whole module
regardless. Narrowing what is reported to the scope would hide findings the edit caused. The
front door prints `  types: covered the whole package, not only the N files in scope` when the two
differ.

### `lint`

```json
{"kind":"lint","findings":3,"rulesRun":15,"filesWalked":149,"nodesVisited":412233,"elapsedMilliseconds":96,
 "reusedFrom":"","rulesSilent":["cohere-swift/no-print"],"rulesWatchedAndQuiet":9,
 "crashes":[{"file":"/…/X.swift","error":"…"}],
 "rulesScopedOff":{"cohere-swift/no-print":12},"rulesNotConfigured":[],"configNote":""}
```

Rendered by the existing lint line and coverage printers:
`lint: 3 findings — 15 rules over 149 files, 412233 nodes visited, in 96ms`, followed by the rule
coverage, crash, and config notes. `reusedFrom` names the phase whose walk was reused (`fix`), and
the line then says so instead of printing a duration.

### `phase`

```json
{"kind":"phase","name":"types","outcome":"notReached","elapsedMilliseconds":0,"detail":"1 type diagnostics — lint findings against wrong semantics are noise"}
```

`name` is `fix`, `types`, `lint`, or `unused`. `outcome` is `ran`, `skipped`, `notReached` or
`reused`; these are the phases.go outcomes, spelled in camelCase. The engine writes exactly one
`phase` record per phase, every run, in pipeline order, including phases that did not run. A
missing phase record is a protocol error, for the reason `markRemainingNotReached` gives: a phase
absent from the report reads the same as one the reporter forgot.

### `rule`, only under `--rules` and `--rules-enabled`

```json
{"kind":"rule","name":"cohere-swift/no-force-unwrap","severity":"error"}
```

`--rules` prints names, sorted. `--rules-enabled` prints `name<TAB>severity`. The development-build
note goes to stderr, as it does for TypeScript.

### `summary`, always last

```json
{"kind":"summary","findings":4,"complete":true,"nothingToCheck":"","exitCode":1}
```

- `findings` is the sum of every `finding` record. The front door counts the findings it received
  and refuses a summary that disagrees. Two counts that should agree and do not are a defect.
- `complete` is false when any file or phase that could have produced a finding did not run. The
  front door prints `this run did not check everything` from its own phase records anyway, and a
  disagreement here is a protocol error.
- `nothingToCheck` is the reason when the run had nothing to look at, such as `--changed` with
  nothing changed. That is a clean answer over zero files, printed as `<reason>: 0 files checked`.
- `exitCode` is what the engine will exit with.

## Exit codes

| Engine exit | Meaning | Front door |
|---|---|---|
| 0 | summary written, `findings` 0, `complete` true | exits 0 |
| 1 | summary written, findings or an incomplete run | exits 1 |
| 2 | the engine could not run: bad flag, contract mismatch, no package, toolchain missing | prints the engine's stderr, then `cohere: the Swift engine did not run, so nothing was checked`, exits 1 |
| other, or killed | crash | `cohere: the Swift engine exited <code> without a summary, so nothing was checked`, exits 1 |

An exit of 0 or 1 without a `summary` record is a crash. An exit code that disagrees with the
summary is a protocol error. The front door owns the final exit code, and there is no path through
it where a run that did not finish prints green.

## Flags

| Flag | Swift meaning in phase 1 |
|---|---|
| `--no-fix` | Writes nothing to the project: no source, no `.build`, no `Package.resolved`. Builds go to the engine's cache outside the project, and package resolution is disabled. Fix and format record what they would change as findings. |
| `--fix` | Fix and format only. |
| `--types`, `--lint` | That phase alone, as for TypeScript. |
| `--format` | Formatting is on by default for Swift, unlike TypeScript, where it waits on parity with the existing gate. For Swift that parity is measured: swift-format 604.0.0 as a library is byte-identical to `xcrun swift-format` on 952 of our files. The flag is accepted and changes nothing. |
| `--format-all` | Format every file rather than changed files. |
| `--changed`, `[paths]` | Narrow fix, format and lint to the scope. Types still reports the whole package (see `types`). |
| `--lint-config <file>` | The `CohereSettings.json` whose `swift` block configures rules. |
| `--fix-passes <n>`, `--single-threaded` | As for TypeScript. |
| `--rules`, `--rules-enabled`, `--version` | `rule` and `provenance` records. |
| `--unused`, `--unused-all`, `--unused-deep` | Not implemented for Swift until phase 2. The engine records `unused` as `skipped (not implemented for Swift yet)`. It neither errors, because the phase is opt-in and its absence withholds nothing from the gate, nor stays silent. |
| `--timing`, `--explain <file>` | Not implemented for Swift yet. The engine refuses with exit 2 and names the flag. A flag that is accepted and ignored reads as a run that did what was asked. |
| `--tsconfig` | Meaningless for Swift. The front door refuses it against a Swift root. |
| `--directory` | Resolved by the front door into `--root`. |

## Config

Rules are configured in the `swift` block of `CohereSettings.json`. Keys are `cohere-swift/<rule>`,
and values are severities and strictness options, never allow lists or ignore names. The formatter
is configured by `.swift-format`, found at or above each file, so `Format.sh` and cohere read the
same file and cannot disagree.

**Where the config lives.** The engine reads `CohereSettings.json` from beside `Package.swift`, or
the file `--lint-config` names. When there is none, every house rule runs at `error`, and every run
says so in one line naming the defaults in force and where a config would go:
`  config: no CohereSettings.json beside Package.swift, so every house rule ran at error — put one at <root>/CohereSettings.json to change that`.
That is not the permissive default the TypeScript loader refuses. The TypeScript loader refuses to
lint with nothing configured because the output would look like a clean run. Here, every rule runs,
and the line says why. Approved by @system_cohere, 2026-10-01.

The line travels in the `lint` record as `"configNote"`. It is empty when a config was read, and the
front door prints it under the lint line whenever it is not empty.
