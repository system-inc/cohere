# The engine contract

What an external engine prints and how the Go front door renders it. cohere-swift is the first
external engine; the TypeScript engine stays in process and never speaks this. The contract exists
so a Swift run reads exactly like a TypeScript run: same finding lines, same phase line, same
coverage notes, same accounting, same exit code. Someone who knows how to read one already knows
how to read the other.

The Go engine speaks it too. It is the cohere binary itself in a hidden mode, `cohere go-engine
--contract 3 --root <dir>`, which the front door runs as a child exactly as it runs cohere-swift, so a
crash or an out-of-memory in a Go load ends that child and prints as nothing checked rather than taking
the front door down with it. One renderer serves both engines; only its words differ (`engineLanguage`
in command/cohere: "the Go engine", "Go files", a `go` label on a compiler finding with no rule).

The fixtures in `Contract/` are the agreement made executable. The Go renderer's tests and the Swift
encoder's tests both read them, so neither side can drift without the other's test failing.

| Fixture                     | What it pins                                                                                                                                                                                                                                                                                                   |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Clean.jsonl`               | Every phase ran or was reused. One file is excluded with its reason. Exit 0.                                                                                                                                                                                                                                   |
| `Findings.jsonl`            | A compiler warning with its group, and a rule finding whose message has a newline. Lint still runs after a warning. The binary was built from a modified tree. Exit 1.                                                                                                                                         |
| `TypesBail.jsonl`           | Under `--no-fix`, a compiler error cuts lint off as `notReached`. The run is incomplete. Exit 1.                                                                                                                                                                                                               |
| `CrashWithoutSummary.jsonl` | The stream stops after the fix phase. The front door must say nothing was checked and exit 1.                                                                                                                                                                                                                  |
| `Unreadable.jsonl`          | One file the engine could not read: named beside the excluded files, every phase ran, nothing found, and still incomplete with exit 1.                                                                                                                                                                         |
| `LintOnly.jsonl`            | `--lint`, a real run: fix and types skipped as `not requested`, lint ran and found two, and the summary calls the run complete, which it is. Exit 1, for the findings.                                                                                                                                         |
| `FormatOnly.jsonl`          | `--no-fix --format-only`, a real run: the fix phase ran with no fix proposed and found one file the formatter would change, which is the one finding; types, lint and unused skipped as `not requested`, and the summary calls the run complete. Exit 1, for the finding.                                      |
| `GoClean.jsonl`             | A clean run as the Go engine writes it: its provenance names a go toolchain and no swift-syntax or swift-format, and the front door renders it in Go's words, with nothing in the output saying Swift. Exit 0.                                                                                                 |
| `NotWritten.jsonl`          | A bare run whose fix phase reached one file through a link to outside the root: `notWritten` names it with its real path, its formatting is a `format` finding and the fix it was not given a `rule` finding, the file inside beside it is written, and the run is incomplete. Exit 1.                         |
| `Unused.jsonl`              | `--unused`: after lint, each rule's `unused` records then its `unusedCoverage`, `unused-import` first and `unused-declaration` second (the declaration section is from a real run on ahraos-presence), the unused phase ran, and the summary counts no findings and exits 0, because the report is not a gate. |

**Version 2** (2026-10-02) added the `unreadable` record. Before it, a file the engine could not read was a
note on stderr: the summary said the run fell short and the front door believed it, but could not say
which files. Both sides moved to 2 in one commit, and each refuses the other version.

**Version 3** (2026-10-03) removed the summary's `nothingToCheck`. It named why a run had nothing to look
at, which only `--changed` over an unchanged tree ever produced, and `--changed` is gone from both
engines (ff50e94, 68e37a8). This engine had written it empty since, so the field, its fixture
(`NothingChanged.jsonl`) and the front door's `0 files checked` rendering went together. A summary over
a run that skipped every phase is now the gap it is, and one that calls itself complete is refused.

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
  to stderr (a submodule it could not read, a file lost from scope) go here. A file the engine could
  not read is not one of them: it is an `unreadable` record, so the front door can name it.
- **Records stream in pipeline order.** The front door prints each one as it arrives, so a slow
  types phase shows its fix line first, the way the TypeScript engine does.

## The invocation

The front door resolves the project root (the nearest directory holding `Package.swift` or
`tsconfig.json`, nearest wins), then runs:

```
cohere-swift --contract 2 --root <absolute package root> [flags] [paths]
```

The root may also be an Xcode project's directory, one holding `project.yml` (an xcodegen spec) or
exactly one `*.xcodeproj`, and no `Package.swift` (#ybspptb). A `Package.swift` beside them wins. The
engine generates a spec's project into its own scratch, never beside the spec, and reads the targets and
their Swift files from the project file. A root holding two projects, or a workspace and no project, is
refused with exit 2 and a sentence, as is a spec xcodegen refuses and a machine without xcodegen.

A root holding no project at all is a scripts-only root (#wpd36zj): discovery gives one to the Swift files no
project claims, at the discovery root, inside a repository or not, as ahra's four scripts under `modules/` are. It has no target, so no
package or Xcode check runs, and its files are its standalone scripts (see `project`), each checked alone. Its
walk skips what discovery never enters (`node_modules`, `.build`, `.cache`, `.git`, `testdata`), and a file
below a project of its own is left out entirely, since that project runs or is covered by one that does. Its
inputs record names no walk of the root, only the scripts, the files it read and the file set's listings and
ignore files. Outside a repository it walks by the tree's own ignore files, as a package root does there
(#13nbb1s), so no Swift file is left to the gap the front door once named, `Swift files no package checks`, whose
`gaps.unclaimedSwift` its `--json` summary no longer writes.

`--contract` is the version the front door speaks. An engine speaking a different version refuses
the run, prints a sentence on stderr naming both versions, and exits 2. The first record the engine
writes is `provenance`, which carries its own `contract` field. The front door refuses a mismatch
there too, because the record and the flag are two independent ways for the versions to disagree.

The front door asks the toolchain once per process tree and hands the answers down, so an engine need not ask
again (#nkk91tv). Each is optional, and an engine that finds one missing, or unusable, asks for itself:

- `COHERE_SWIFT_VERSION_OUTPUT` is what `swift --version` printed, unchanged. The engine reads its
  `swiftlang-` token exactly as from its own call, and ignores a value without one.
- `COHERE_SWIFT_EXECUTABLE` is the `swift` that `xcrun --find swift` names. The engine uses it only when it
  is an absolute path to an executable file.

Both feed the engine's cache keys, so the front door never passes on a value it did not ask for or receive
from the cohere process that started it: a cohere run from a shell drops any it inherits and asks fresh
(command/cohere/swift_toolchain.go).

## Records

Every record has `kind`. Fields not listed are not part of the contract and the front door ignores
them. Durations are integer milliseconds. Paths are absolute, as `sourceFile.FileName()` is for
TypeScript.

### `provenance`, always first

```json
{
    "kind": "provenance",
    "contract": 3,
    "engine": "cohere-swift",
    "version": "0.1.0",
    "commit": "<40 hex or dev>",
    "sourceTreeModified": false,
    "toolchain": "swiftlang-6.4.0.34.1",
    "swiftSyntax": "604.0.0",
    "swiftFormat": "604.0.0"
}
```

`sourceTreeModified` drives the same warning the TypeScript run prints: no commit reproduces these
findings. For this engine it covers the engine's own inputs, `swift/Package.swift`, `swift/Package.resolved`
and `swift/Sources/`, the files the launcher's cache key reads: true when any of them differs from the
commit, tracked or untracked, and never for a change elsewhere in the repository. `--version` prints
exactly this record and nothing else.

### `project`, the Swift counterpart of `graph built in`

```json
{
    "kind": "project",
    "root": "/…/ahraos-macos",
    "package": "AhraOs",
    "elapsedMilliseconds": 412,
    "filesInPackage": 152,
    "filesOurs": 149,
    "filesInScope": 149,
    "scopeDescription": "",
    "targets": [{ "name": "AhraOsCore", "kind": "library", "files": 14, "languageMode": "6" }],
    "excluded": [{ "file": "/…/Scratch.swift", "reason": "ignored by git", "source": ".gitignore:3" }]
}
```

- `filesInPackage` is every `.swift` file in a root target, and every standalone script the run checks.
  `filesOurs` is the subset that is not ignored by git and not excluded. Dependencies, including local ones
  like `Vendor/SwiftTerm`, are never counted, because they are not root targets. Ignored by git means by the
  repository's rules alone, every `.gitignore` and `info/exclude`, matched case sensitively, never the machine's
  global excludes file or `core.ignorecase` (#jgk8ppa), so one tree counts the same files on every machine.
- A standalone script (#13nbb1s) is a file of ours: a `.swift` file git would list that no target compiles,
  with a `#!` line or a statement at its top level, the code `swift <file>` runs, whose nearest project (a
  `Package.swift`, a `project.yml` or one `.xcodeproj`) is the root or a local package the run covers. It is
  in `targets` as a target of its own, kind `script`, one file, named for the module `swift <file>` compiles it
  into (its file's stem, or `main`, numbered past the first when two scripts share one). This is additive within
  contract 3. Every rule and the formatter judge it as they judge a `main.swift`. A file below a nested project is left
  to that project's run, as `no target of this package compiles it`, which the front door leaves out under a
  project it runs. Any other file no target compiles is excluded with a reason the front door counts as a gap:
  `no target of this package compiles it, and it holds only declarations, so it is no script to check alone`, or,
  for a script whose imports name both macOS (AppKit, Cocoa) and iOS (UIKit),
  `a script whose imports do not settle its platform: it imports AppKit for macOS and UIKit for iOS, so neither SDK was guessed at`.
  Outside a git repository, a `git archive` copy say, "git would list" reads the package's own ignore files as git
  would were its root a repository's, so a script there is checked and every other such file named, as in a checkout.
- An excluded entry's `source` names the rule that excluded the file, as `path:line` with the line counted from 1: the
  ignore file's line that decided, or the line that excluded a directory above the file. The path is relative to the
  project's root (`.gitignore:3`, `Sources/.gitignore:1`, `../.gitignore:2` for a repository whose root is above the
  package's), or absolute for an ignore file outside the work tree, as a worktree's shared `info/exclude` is. It is
  absent where no rule is the cause: a package manifest, a vendored file, a file no target compiles, a file inside
  another repository. The Swift engine has no `ignorePatterns`, so no Swift source names `CohereSettings.json`. A
  narrowing excludes no file here; the front door counts what it left out from `filesInScope`. Additive within contract
  3 (#08cv2mp): the front door quotes it when every file was excluded,
  `no files checked: 1 file excluded, ignored by git (.gitignore:3)`, and a reader before it ignores it.
- `filesInScope` equals `filesOurs` on a whole-package run. A run narrowed by named paths sets it
  lower and describes the request in `scopeDescription`, which the front door prints in the
  parenthesis of `N in scope (…)`.
- `elapsedMilliseconds` is the time spent describing the package. The accounting line bills it as
  the graph, because it is the step nothing can skip.
- Written on every run that checks anything, since every such run describes the package first.
- `projectKind` says what `package` was read from: `SwiftPackage`, `XcodeGen` (a spec, generated into the
  engine's scratch), `XcodeProject` (a committed project, read where it stands) or `Scripts` (a scripts-only
  root, whose `package` is its directory's name and which reads nothing). It is additive within
  contract 3, and an engine from before it writes none. For an Xcode project, `package` is the project's
  name, and each native target's kind is Xcode's product type in SwiftPM's words: an app or an extension
  is `application`, a unit-test or interface-test bundle `test`, a framework or library `library`, a
  command-line tool `executable`.
- An Xcode project's types are built with xcodebuild (see `types`). Its index is not read yet, so `--unused`
  is `notReached` there (#9ktapz2). The package rules, which judge settings an Xcode project keeps elsewhere,
  are offered no package there and listed as silent.

Rendered (whole package, then scoped):

```
package described in 412ms: 152 Swift files in the package, 149 of them ours
package described in 412ms: 152 Swift files in the package, 149 of them ours, 3 in scope (Sources/AhraOsIpc)
```

Excluded files print as coverage notes under the lint line, grouped by reason. Up to five files of a
reason are named, `  not checked: <file> (<reason>)`; more are counted, `  not checked: <n> files
(<reason>)`, because 107 vendored files named one per line buried the verdict on the first real run.

The Go engine writes an excluded entry's `source` as the Swift engine does (see the bullet above): for a file git
ignores, the ignore file's line that decided, relative to `root`, `../.gitignore:4` for one above it. A file the
build constraints leave out has none, since no line of a file decided it.

```json
{
    "kind": "project",
    "excluded": [{ "file": "/…/scratch.go", "reason": "ignored by git", "source": ".gitignore:1" }]
}
```

### `unreadable`, between `project` and the first `phase`

```json
{ "kind": "unreadable", "file": "/…/Latin1.swift", "error": "it is not valid UTF-8 or could not be opened (…)" }
```

One per file the engine could not read, so nothing in it was checked. `error` is a sentence. The front
door prints each under the lint line with the excluded files, grouped and counted past five the same
way, as `  not checked: <file> (could not be read: <error>)`, and counts it as a gap: a summary calling
the run complete with any `unreadable` record is a protocol error. An `unreadable` record before
`project` or after any `phase` is refused, so every file a run could not read is known before a phase
reports on the files it could.

### `finding`

```json
{
    "kind": "finding",
    "source": "rule",
    "file": "/…/ByteRing.swift",
    "line": 159,
    "column": 61,
    "endLine": 159,
    "endColumn": 62,
    "severity": "error",
    "rule": "cohere-swift/force-unwrapping",
    "messageId": "forceUnwrap",
    "message": "…why, not only what…",
    "fixes": [],
    "suggestions": []
}
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
/…/ByteRing.swift:159:61 - force unwrapping crashes on nil … [cohere-swift/force-unwrapping/forceUnwrap]
/…/Broken.swift:3:23 - error: cannot convert value of type 'String' to specified type 'Int'
/…/Warning.swift:2:9 - warning: variable 'neverMutated' was never mutated; consider changing to 'let' constant [#VariableNeverMutated]
/…/Unformatted.swift:12:1 - not formatted the house way; a run without --no-fix rewrites it [cohere-swift/consistency-require-formatting/notFormatted]
```

The compiler line is the TypeScript shape `file:line:col - error TS2322: message`. Swift
diagnostics have no number, so the compiler's warning group takes the place of the code, written
the way swiftc writes it, `[#Group]`. A compiler finding with no group has no bracket. For compiler
findings, `rule` holds the group name without the `#`, or `""`.

### `unused` and `unusedCoverage`, only under `--unused`

```json
{"kind":"unused","file":"/…/Main.swift","line":1,"column":1,"endLine":1,"endColumn":18,
 "rule":"cohere-swift/correctness-no-unused-import","messageId":"unusedImport","message":"…why…","subject":"import Foundation",
 "suggestions":[{"message":"Remove `import Foundation`","fixes":[{"start":0,"end":18,"text":""}]}]}
{"kind":"unusedCoverage","rule":"cohere-swift/correctness-no-unused-import","filesChecked":217,
 "filesNotChecked":{"it has #if, and the index describes only the configuration the build compiled":1},
 "checked":471,"skipped":{"re-exported with @_exported, which is API":0},"found":15,"elapsedMilliseconds":2525}
```

The `--unused` report: code that was written and is never used. It is the Swift counterpart of the
TypeScript unused report and keeps its rule, a report and not a gate. An `unused` record carries a
finding's place and words, but it is not a `finding`: the summary does not count it and it never
moves the exit code. Failing a build over code that is safe to remove and never breaks one would make
the report something people route around rather than read.

- Each `unused` record names one thing to remove. `subject` is that code as written and short
  (`import Foundation`), for the report's one line per item. `suggestions` hold the removal, offered
  and never applied: the report does not rewrite.
- Exactly one `unusedCoverage` record follows a rule's `unused` records, and its `found` equals how
  many came. It states the population beside the result, so a report that checked nothing never reads
  like a report that found nothing: `filesChecked`, `filesNotChecked` by reason (a file the index
  cannot vouch for), `checked` (the items judged), and `skipped` by reason (items never reported by
  design).
- Both come after the `lint` phase record and before the `unused` one. An `unused` phase that `ran`
  without an `unusedCoverage` record is refused, as is a count that disagrees.

Rendered after the lint line, in the TypeScript report's shape:

```
unused: a report, not a gate — nothing here fails a build

  imports nothing uses — 2
    /…/Main.swift:1:1 — import Foundation
    /…/Tally.swift:2:1 — import Combine
  looked at 2 files and 5 imports (cohere-swift/correctness-no-unused-import)
  not checked for unused imports: 1 files (it has #if, and the index describes only the configuration the build compiled)
  not checked for unused imports: 1 files (the build has not compiled it as it stands)
  never reported: 1 imports (re-exported with @_exported, which is API)
```

Files not checked and items skipped are counted by reason, sorted by reason. The section header and its
lines are left out when nothing was found.

`unused-import` reads the index store the types phase's check, or an Xcode root's build, writes. An import is used when something written in the file
resolves into its module, or into a module it re-exports, or when a declaration the file refers to
names that module in its signature (the index leaves out some member references, so a value of the
module's type reached through a closure parameter is caught by the signature that hands it over). An
import that is used is still reported, with `messageId` `redundantImport`, when the file's other imports
already bring everything it brought: `import AppKit` beside `import SwiftUI`, which re-exports AppKit
whole. That check believes only re-exports read from each module's own text: a `.swiftinterface`'s
`@_exported import` lines, and for a Clang module the headers its map re-exports and what they import on
lines the build certainly compiled, a whole module or one header at a time. It tries the widest import
first so the narrower one stays, and judges each removal against what the ones before it left, so every
import reported can go together. A file the index cannot vouch for is not checked: one the build has
not compiled as it stands, one with `#if` (the index describes the configuration the build compiled),
one with a reference no module claims. `@_exported` imports are API and never reported.

`unused-declaration` reads the same index, after `unused-import`, and writes its own `unusedCoverage`
record. A declaration `private` or `fileprivate`, or inside a type or extension that is, is judged
against its own file's record, since nothing outside the file can name it. Any other declaration is
judged against every record of the package, its tests included; one declared public in a library
product's target, or in a target a library product re-exports, is API and never reported. A declaration
is used when a record holds a reference to it from outside its own text (a function that only calls
itself is not used), when another declaration overrides or witnesses it, when a call of its struct's
synthesized memberwise initializer passes it, or, for a property wrapped by an attribute, when its
`$name` or `_name` is referenced. `subject` is the keyword and the name (`func after(_:_:)`,
`var stopping`), and the suggested removal takes the declaration's lines with the comments written above
and trailing it. Only the outermost of nested unused declarations is reported, so every one reported can
go together. What the index cannot see, or what removing would change without breaking the build, is
counted under `skipped` by reason and never reported: overrides and witnesses, what the Objective-C
runtime reaches or can find by name, XCTest's test methods, entry points and previews, members the
compiler calls by name, declarations an attribute may register and the types that hold them, Codable's
coding keys, initializers, a stored property of a type whose conformances reach `Encodable` or
`Decodable` or what the index cannot see into, of a SwiftUI view (SwiftUI compares them to decide when to
draw), or of a type descending from another package's (which may read it by reflection), an instance's
stored property whose initializer runs code, a field of a struct of plain numbers (its bytes may be a
shader's constants), cases an enum's conformances or raw values may reach, and a name that appears inside
`#if` or in a file the build has not compiled as it stands. Files are left unchecked for the reasons
`unused-import` gives.

### `fix`, the fix and format phase's summary

```json
{
    "kind": "fix",
    "filesConsidered": 149,
    "filesRewritten": 2,
    "fixesApplied": 5,
    "fixesRefused": 1,
    "refusalsByReason": { "overlaps another fix": 1 },
    "filesReformatted": 2,
    "filesNotFormatted": 0,
    "notFormattedReasons": {},
    "formatScope": "Sources/AhraOsCore",
    "changedFiles": [
        { "file": "/…/Pane.swift", "fixedBy": { "cohere-swift/private-over-fileprivate": 3 }, "formatted": true },
        { "file": "/…/Ring.swift", "fixedBy": { "cohere-swift/legacy-constructors": 2 }, "formatted": true }
    ]
}
```

The front door fills an `edit.Summary` from it and prints that summary's `String()`, then
`format scope: <formatScope>`. Fixes and formatting are separate counts, as they are for
TypeScript. Every file in scope is formatted the house way. A file the formatter could not format
(the formatter failed on it, or a file in scope does not parse, so nothing was rewritten) counts in
`filesNotFormatted` with its reason. A formatter that formatted nothing therefore never reads like
a formatter that found everything already correct.

`changedFiles` names every file the phase wrote, once each. Each entry carries `fixedBy`, the fixes that
landed in it counted by the rule that proposed them (empty when only the formatter changed it), and
`formatted`, whether the formatter changed it. Its length is `filesRewritten`. A run that writes nothing,
under `--no-fix` or with nothing to change, writes it empty. The front door reads it with #ytqqv8v's
output: it lists these files and counts them as cohered, and refuses a stream whose list length differs
from `filesRewritten`. Until then it ignores the field, as below.

The field is additive within contract 3 (@system_cohere_build's ruling on #s4k5yb9). A field this
document does not list is ignored by the front door, so an older front door loses only the list, never a
count or a verdict. The counts stay what the verdict reads, and the list never changes an exit code. An
engine from before the field writes no `changedFiles` at all. The front door reads that as _not listed_,
never as _no files changed_, and renders the counts alone. `Clean.jsonl` is such a stream.

The Go engine's fix record carries `notWritten`, additive within contract 3 as `changedFiles` is: every file the
phase would have written and did not, since it is reached through a symbolic link to a real path outside the root
(#hwz114g), as `{"file":"<root>/shared/sum.go","realPath":"<elsewhere>/shared/sum.go"}`, sorted by file, and `[]`
when there were none. A fix never writes such a file: it is checked, its findings stand, and the run is incomplete.
The front door names each file with its real path and counts them in the footer, and reads a stream without the
field, from an engine before it, as naming none.

This engine writes `notWritten` too. It names every file the phase would have written and did not, since its real
path, every link resolved, lies outside the run's root (#723rzxm, the rule #hwz114g gives all three engines), sorted
by file:
`"notWritten":[{"file":"/…/Linked/Outside.swift","realPath":"/elsewhere/Outside.swift"}]`, `file` as the run reached
it and `realPath` where it is. Such a file is checked, read only, as `--no-fix` checks it: none of its fixes lands, lint
walks the text on disk, so what they would have fixed is reported, and when the formatter would change that text it
is one `notFormatted` `format` finding at the first line that moves. It is never in `changedFiles`, and its fixes and
reformat are not counted in `fixesApplied` or `filesReformatted`. Each is a gap: the run is incomplete, exit 1, and a
summary calling it complete is refused. The front door names each in the account as
`  not written: <file> → <realPath>` and counts them on the footer, `⚠ not written: 1 file reached through a link
outside the root`, and `--json` lists them in the summary's `gaps.notWritten`. A link inside the root is written
through: a temporary file in the target's own directory, given the target's permissions, then renamed onto the target,
so the rename never crosses a filesystem and the link stays a link. The field is additive within contract 3, in the Go
engine's shape: this engine always writes it, `[]` when there were none, and a stream from an engine before it has
none, which the front door reads as nothing withheld.

### `types`

```json
{
    "kind": "types",
    "diagnostics": 1,
    "files": 149,
    "elapsedMilliseconds": 2187,
    "filesWithoutRecord": [],
    "build": "checked without building: the build server prepared the dependencies in 4.1s, and 12 frontend jobs checked 149 files of 8 modules with -emit-sib in 6.2s, scratch <root>/.cache/cohere/swift; prepare built no plugin host tool"
}
```

Rendered as `types: 1 diagnostics over 149 files in 2.187s`. Each entry in `filesWithoutRecord`
prints `  types: no compiler record for <file>, so its diagnostics are unknown`, and any such file
makes the run incomplete. The types phase reads each file's serialized diagnostics (`.dia`), not
the compiler's stdout, so a file with no `.dia` is a file the engine cannot vouch for.

`build` says in words how the records were had: a check into the scratch, the last check's records
reused, or edited files checked in process. A run whose checks all succeeded may end it with what the
scratch prune removed, as `; the scratch prune removed 6.9 MB: 226 index records no unit names`. The
prune runs after such a check, at most once an hour per scratch. It removes the index store's records
no unit names and the scratch of a package that is no longer local, each unused for an hour, and never
touches a check in flight or the build server's build descriptions, whose cache the build system bounds itself.

At an Xcode root (#gg0x95n) the build is xcodebuild, one per native target that compiles a file of ours,
through the scheme Xcode makes for it, for the generic device of the platform its `SDKROOT` names: `build`, or
`build-for-testing` for a test bundle. It writes only into the scratch (`xcode-build` as derived data,
`xcode-packages` for packages), never into `~/Library/Developer/Xcode/DerivedData`, signs nothing, and holds
run-script phases to their declared files (`ENABLE_USER_SCRIPT_SANDBOXING=YES`). Beside the build, the one write a
check leaves in the developer's tree is the empty `.swiftpm/xcode` and `.swiftpm/configuration` xcodebuild makes in
each local package the project links, which no flag stops (#8f8vy1n): removed after each build, they moved the
package directory's times, which the inputs record names, so every build cost each project naming that package its
next replay. Left there, they move it once. Any other empty folder a build made in a package's `.swiftpm` is
removed, a folder holding a file stays, and no file is ever deleted. Packages resolve into the scratch,
under `--no-fix` too (#s8p20fe): a generated bundle is in the scratch already, and under `--no-fix` a committed
bundle is built through a workspace in the scratch that names it, so its pins land there and the bundle is left as
it was found. A resolution the repository commits (a committed bundle's own, or one kept in the bundle beside a
spec that git does not ignore) is preferred, and under `--no-fix` the build is held to it as it stands. A resolve
that fails is a finding at the spec or the committed project file that says so in xcodebuild's words. Its records
are judged by the same rules as a package's, and `build` reads
`xcodebuild of <n> targets for <platforms>, derived data <path>; built in <seconds>, the build an Xcode root is allowed, since xcodebuild has no action that checks without building`, then, when a package it links holds those folders, `; it leaves the empty .swiftpm/xcode and .swiftpm/configuration xcodebuild makes in each local package the project links, in <n> packages here, the one write into the tree a check allows`, or that the last build's records were reused
because no input changed, then names each build that failed and the step it failed at, and each target not built and why (an `SDKROOT` naming no platform the engine knows). A failure no file of
ours carries an error for is a finding at the spec or the committed project file, and a build that left no
record leaves every file it was for in `filesWithoutRecord`, never an empty report.

A package is checked, never built (#11fs5xp): no object, no code generation, no link. The check has three
steps, all into the scratch:

- `swift package experimental-build-server`, SwiftPM's Build Server Protocol server, gives each target's compiler
  arguments (`sourceKitOptions`) and prepares the dependencies' modules (`buildTarget/prepare`, Swift Build's
  prepare-for-indexing), bodies skipped and no object written, under `<scratch>/out`;
- each owned module's frontend jobs, planned by the driver as `-typecheck`, run as `-emit-sib` with their output
  sent to /dev/null, so the diagnostics a build reports after type checking (missing returns, constant overflow,
  infinite recursion, use before initialization, exclusivity violations, Swift 6's sending data races) are
  reported too. Each file has its own record under `<scratch>/check/<Target>.build` and its own index unit in
  `<scratch>/index-store`, which the typed rules and `--unused` read. A batch that fails is checked again one file
  at a time, since a failed batch leaves its other files' records unreadable, and a compiler a signal ends is a
  finding at the file it crashed on, `messageId` `compilerCrashed`;
- each owned C, C++ and Objective-C file is checked with `clang -fsyntax-only` and its target's arguments, and each
  error in it is a finding at its place. Its warnings are not reported, as a build never reported them;
- the dependencies' declarations, which a build left in its index beside the project's own, are indexed for
  `--unused` and the typed rules: each dependency module's files with `-typecheck`, every function body skipped, and
  every C-family file of the graph in the same `clang -fsyntax-only` pass. A file a unit newer than it already
  describes is not indexed again.

A build server that fails before the compiler runs, a package `--no-fix` cannot resolve among them (SwiftPM keeps a
package's pins only beside its Package.swift, so a package that names a remote dependency and has no
Package.resolved cannot be resolved without writing into the tree), is a named gap in a run that finishes
(#4txsasr): one `buildFailed` finding at the package's Package.swift in the check's words, `build` reading
`not checked: <those words>`, and every file of ours in `filesWithoutRecord`. The finding is no type error in any
file, so lint still runs over every file, and each file the typed rules cannot judge says the failed check is why.

After an edit, when only files changed since the last successful check, the check looks again only at what the
edit could change: the targets holding an edited file, every owned target that imports one of them, directly or
through another, and the owned C-family files when one of them or a header changed. Every other target's records
are the last check's, and `build` counts them, as `, 3 modules no edit could change read from the last check's records`.
A scoped module that imports no other scoped module needs nothing prepare emits again, so it is checked while prepare
runs, counted as `, 1 module of them alongside prepare, since they import no edited module`.
When the edits are all to Swift files, prepare covers only the edited modules' own dependencies, and the check emits
each interface a scoped module imports itself: one `-emit-module` job with the testable variant's arguments and bodies
skipped, as prepare's, into `<scratch>/check/interfaces/<Target>`, which every module checked after searches first.
Prepare emitted an owned module twice, its testable variant and its product's, and the check reads only the first.
`build` says so after prepare's time, as `, only the edited modules' own, and the check emitted the interface of Core
itself in 6.1s`. The scratch lists those interfaces, whose prepared copies are older than their sources, until a check
that prepares everything, after a C-family edit or any other, empties the list.
An edit to anything else, a manifest or a resource, checks everything.

A local package that holds test files of ours is checked in a session of its own, under `<scratch>/local/<package>`,
since the root's build server lists a local package's libraries and not its tests. Under `--no-fix` the server is
held to the package's own `Package.resolved` (`--disable-automatic-resolution`). A server that fails is the types
phase failing by name, never a build in its place. The one build a check allows is a build-tool plugin's host tool,
which prepare compiles and links because the plugin runs it: `build` counts it on every run, as
`; prepare built 1 plugin host tool, the build a check allows, a tool a build-tool plugin runs and not the project's code: encuda in 9.8s`,
or `; prepare built no plugin host tool`, then, when SwiftPM compiled a build-tool plugin itself so it could run it,
`; SwiftPM compiled 1 build-tool plugin to run them: CudaBuild`. A failed job no file of ours carries an error for is a finding at the
manifest, `messageId` `buildFailed`.

A package is checked for the macOS host when its manifest names macOS among its `platforms`, or names no platform.
One whose `platforms` leave out macOS (#ee9ct1r) is never judged against the host's SDK, which it does not target:
it is checked the same way for the first platform it names among iOS, tvOS, watchOS and visionOS, the build server
given `--triple arm64-apple-<system>` and that platform's SDK, and `build` begins
`for <platform>, the first platform the package names, since it names no macOS: `. A package whose platforms name
none of those (Mac Catalyst alone), or whose platform's SDK is not installed, is not checked: `build` begins
`not checked:` and says why, and every file is in `filesWithoutRecord`, so the run is incomplete.

Each standalone script (see `project`) is checked after the package or the Xcode project, alone, as `swift <file>`
runs it, in the same record: its findings among the rest, `files` counting it. The check is a package's frontend
job, `-emit-sib` to /dev/null with a record and an index unit for the script, into `<scratch>/scripts`, whose index
and compile arguments the typed rules and `--unused` read beside the package's. Its arguments are the house's, in
one shape for every script: `-swift-version 6`, each upcoming feature `toolchain-require-upcoming-features`
requires, and `-strict-memory-safety` with `-Werror StrictMemorySafety`. Its imports choose the SDK: UIKit is iOS,
checked with `-target arm64-apple-ios<version>` at the iOS SDK's `DefaultDeploymentTarget`, and anything else the
macOS host. After the compiler's own words, each error in a script says in a parenthesis that it was checked alone,
as `swift <name>` runs it, and that a script needing other files to compile belongs in a package with them, since a
script needing companion files fails exactly so. `build` ends by counting the scripts checked alone with `-emit-sib`,
by platform, and names each script kept unchecked because this Xcode lacks its SDK, which is in `filesWithoutRecord`.

The scripts' check indexes the SDK modules they import (`-index-system-modules`), which `--unused` and the typed rules
read. Into an empty store, every `--no-cache` run and every first run, it seeds them first from the toolchain's SDK
index seed (#f6pqraj): a store of the SDK's units alone, never a project's, under
`~/Library/Caches/cohere/sdk-index/<key>`, outside every project, cloned in whole. The key is everything the units
depend on, each part compared: the seed's format, the contract, `swift --version`'s whole output, the SDK's path and
build version, the target, the index flags, the job's whole argument list without its own file and outputs, and the
modules the scripts import, since Clang writes an SDK header's record differently in another import context. A
unit the seed cannot vouch for (its bytes, the bytes of a record it names, or the module file it was indexed from
not as recorded) is taken out with every unit that imports it, and indexed afresh; a seed that cannot be read, or none, is a miss, and
the check indexes everything, then makes the key's seed from what it wrote, in a folder of its own renamed into
place, never changed after. A hit touches its key's folder, and a publish removes every key's folder untouched for
fourteen days that no reader holds. The types record says so, additive within contract 3, as
`"sdkIndexSeed": {"key": "<the key's SHA-256>", "hit": true}`, absent when the store was not empty or no seed
applied; the front door names it under `--verbose` and in `--json`'s summary as `swift.sdkIndexSeed`, never in
the default line. `COHERE_SDK_INDEX_SEED_ROOT` points the seeds elsewhere, for a bench or a proof.

The Go engine's types record carries two more fields, additive within contract 3, so a front door that does
not read them yet loses only what they count. The Go engine type-checks every package from source and runs no
cgo tool, so a dependency's package `"C"` is faked, and every C name is of an invalid type.
`withheldInDependencyCgo` names each file of a dependency outside the root, whose package has cgo files, where a
type error was withheld because the expression it is about uses package `"C"` or something the package's cgo
files declare: not the project's to fix. Each entry gives the file's package and how many errors it held, as
`{"package":"net","file":"<goroot>/src/net/cgo_unix.go","errors":1}`, sorted by file, and the list is `[]` when
nothing was withheld. A parse or list error is never withheld, nor a type error that uses nothing of the cgo
files, nor anything in the project's own packages. `withheldDependencyCgoDeclarations` counts the declarations
of such dependencies that use the same, or what it left without a valid type: functions, which every rule was
given without their bodies, as an assembly function is given, and package-level variables, given without their
initial values. It is `0` when there were none. For a project with cgo files of its own, go list
-compiled has the cgo tool translate every package with cgo files instead, and those are type-checked from
its outputs, so both are empty then, and the record's `build` says so.

The Go engine type-checks and never builds, with one exception: cgo's preprocessing, which go list
-compiled has the cgo tool run for a project with cgo of its own. So the Go engine's types record
carries one more additive field, `cgoTranslation`, that counts the exception on every run:
`"cgoTranslation":{"packages":3,"milliseconds":2140}` is how many packages the cgo tool translated,
the project's and its dependencies' with cgo files alike, and how long that took. On a load that
translates nothing, which runs no cgo tool, it is `{"packages":0,"milliseconds":0}`, written and never
left out, so a front door can always say whether a run built anything.

The Go engine's types record also carries `"censusMissedPackages":673`, additive within contract 3 (#yyv7nr2), only
when its census counted no Go file while its load reached packages in the workspace: the packages it reached. The
census and the load read one tree, so this is a census that never walked the tree, as a root reached through a
symbolic link once was, and the record's other counts read 0 over work never done. Such a run is never complete: the
engine notes why and writes `complete` false, and a front door refuses a summary that calls it complete. A run whose
census and load agree never writes it.

The Go engine's types record also carries `memory`, additive within contract 3: the memory limit its process
held its heap under and where it came from, as
`{"limitBytes":8589934592,"capBytes":8589934592,"source":"Cap","availableBytes":44023414784}`. The limit is the
smaller of the engine's cap and half the memory available when the run started. `source` is `Cap` or
`Available`, whichever was smaller; `Environment` when the user's `GOGC` or `GOMEMLIMIT` decided, with
`environment` naming them and `limitBytes` the limit they left, `0` for none; or `Unreadable` when available
memory could not be read, and the cap was set as a bound alone. The front door prints it in the account
`--verbose` prints. It is left out of a run the engine was not started for as a process of its own.

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

### `duplicates`, the Go engine's, only under `--duplicates`

```json
{
    "kind": "duplicates",
    "ran": true,
    "notRun": "why, written only when ran is false",
    "floor": 11,
    "files": 412,
    "filesIgnored": 0,
    "functions": 19643,
    "hashed": 19640,
    "groups": [
        {
            "digest": "…",
            "size": 136,
            "members": [
                { "file": "/…/a.go", "line": 12, "name": "decompress", "size": 136, "digest": "…" },
                { "file": "/…/b.go", "line": 30, "name": "decompress", "size": 136, "digest": "…", "kept": "port of …" }
            ]
        }
    ],
    "withheld": 0,
    "belowFloor": 210,
    "refused": [
        { "file": "/…/c.go", "line": 4, "name": "run", "reason": "unsupportedInstruction", "detail": "*ir.Select" }
    ],
    "notRead": { "generated": 4 },
    "crashes": [{ "file": "/…/d.go", "error": "…" }],
    "deadSuppressions": [{ "file": "/…/b.go", "line": 9 }],
    "elapsedMilliseconds": 9,
    "cache": { "unit": "packages", "read": 430, "hashed": 10 }
}
```

The duplicates report (#jfvhbj7): functions whose canonical IR is one graph, at size 11 and up. It is the Go
engine's alone and additive within contract 3; a Swift engine is never given `--duplicates`, which the front door
answers for it as a report that did not run.
It is not a `finding`: the summary does not count it and it never moves the exit code. Its fields are
`internal/duplicate_functions`' `Report`, which the front door prints as it prints a TypeScript run's report.
Exactly one comes, after the `unused` phase record and before `inputs`, when `--duplicates` was named, and never
otherwise; a run that asked and ends without one is refused. When the lint phase did not run, `ran` is false and
`notRun` says why, since the report reads the lint phase's pass.

### `timing`, after `lint`, only under `--timing`

```json
{
    "kind": "timing",
    "processCpuMicroseconds": 80000,
    "load": {
        "listCpuMicroseconds": 2000,
        "listChildrenCpuMicroseconds": 150000,
        "parseCpuMicroseconds": 4000,
        "parsedFiles": 24,
        "typeCheckCpuMicroseconds": 12000,
        "typeCheckedPackages": 3,
        "exportDataCpuMicroseconds": 6000,
        "exportDataPackages": 37
    },
    "rules": [{ "name": "cohere-go/consistency-no-shouting", "cpuMicroseconds": 2500, "packages": 3, "calls": 640 }],
    "shared": [
        {
            "name": "build_intermediate_representation",
            "kind": "intermediateRepresentation",
            "cpuMicroseconds": 8000,
            "packages": 40
        },
        { "name": "syntax_dispatcher", "kind": "dispatcher", "cpuMicroseconds": 1500, "packages": 3 }
    ],
    "measuredCalls": 2300,
    "measuringCpuMicroseconds": 230
}
```

Additive within contract 3, and the Go engine's alone: under `--timing` it says what the run's CPU went to, so a Go
rule is timed before it lands as a TypeScript one is (#3apgjer). It is written once, after the lint record and before
`inputs` and the summary, and never in a run that did not ask, which the front door refuses as a broken stream, as it
does a second one. The Swift engine refuses `--timing` and writes none.

Every CPU figure is microseconds of the kernel's thread clock, read on a thread held for the work, so a rule that
waits, sleeps or is descheduled costs nothing and none is wall time. Each figure has the measuring's own cost taken
out, said once in `measuringCpuMicroseconds` beside `measuredCalls`, the pieces of work measured.

- `rules` holds one entry for each rule that ran, most expensive first, summed over every package and build:
  `packages` is how many its analyzer ran on, dependencies analyzed for facts included, and `calls` how many times a
  dispatcher called its listener, 0 for a rule that walks on its own.
- `shared` holds the analyzers no single rule owns, most expensive first, each with its `kind`:
  `intermediateRepresentation` for the engine's intermediate representation build; `dispatcher` for a dispatcher's
  walk less its calls into the listeners, which are billed to their rules; and `analyzer` for any other several rules
  read, such as `inspect`. A kind the front door does not know is refused.
- `load` is the load's phases, each summed across the goroutines that ran it, so each runs past the load's wall.
  `listChildrenCpuMicroseconds` is the CPU of the go commands the listing ran, which no thread of this process spent.
- `processCpuMicroseconds` is every CPU microsecond the process spent up to the record. The rows (rules, shared, the
  load's own phases and measuring) are said against it, and what they leave is printed as its own line, what no row
  measures, never spread over the rows.

`rules` and `shared` are `[]` when empty, never left out, and the front door refuses a record missing either. When
the thread clock cannot be read, `unavailable` says why, every CPU figure is 0 and means nothing, and the counts are
still real; the front door then leaves the CPU columns out rather than print zeros:

```json
{
    "kind": "timing",
    "unavailable": "this platform has no per-thread CPU clock",
    "processCpuMicroseconds": 0,
    "load": {
        "listCpuMicroseconds": 0,
        "listChildrenCpuMicroseconds": 0,
        "parseCpuMicroseconds": 0,
        "parsedFiles": 24,
        "typeCheckCpuMicroseconds": 0,
        "typeCheckedPackages": 3,
        "exportDataCpuMicroseconds": 0,
        "exportDataPackages": 37
    },
    "rules": [{ "name": "cohere-go/consistency-no-shouting", "cpuMicroseconds": 0, "packages": 3, "calls": 640 }],
    "shared": [],
    "measuredCalls": 0,
    "measuringCpuMicroseconds": 0
}
```

The front door prints the record as TypeScript's timing table under the lint line, and under `--json` writes it
through as a `timing` line before the summary (schema/CohereOutput.schema.json). A run under `--timing` is a
measurement, so the run cache never replays it and never records it.

### `memory`, the Go engine's, just before `inputs`

```json
{ "kind": "memory", "raisedToBytes": [9663676416] }
```

Additive within contract 3, and the Go engine's alone (#5shepvt): the memory limits the run raised its heap to.
Every load runs under the limit the types record's `memory` names, and when that limit is the engine's cap, once a
load ends with no collection in flight the engine may raise it once, by at most 1 GiB, until the next load starts
(#44n0vap). A fixing run loads up to six times. The types record is written after the first load, before any raise
lands, so its `memory` stays the limit the run started under, and this record says each limit the run went above it
to.

`raisedToBytes` holds one entry for each raise that landed, the limit it set, in the order raised. It is `[]` when
nothing raised, never left out and never `null`. The record is written once, in every check run whose load ran, as
`inputs` is: after the last phase record, and after `duplicates` and `timing` when they were asked for, and just
before `inputs`. A check run whose load failed, or that asked for no load, writes none. The Swift engine writes none.

The front door accepts one, in that place, and refuses a second, one before a phase's record or after `inputs`, and
one with no `raisedToBytes`, as a broken stream. A raise is said under `--verbose`, below the types phase's `memory:`
line, as `memory: raised to 9.7 GB after the load` (each raise in order, joined by `then`), and under `--json` as the
summary's `memoryRaisedToBytes`. Neither is in the footer, and a replay says neither, since it raised nothing.

### `inputs`, just before `summary`

```json
{
    "kind": "inputs",
    "files": [
        "/…/go.mod",
        "/…/go.sum",
        "/…/p/p.go",
        "<goroot>/VERSION",
        "<goroot>/bin/go",
        "<goroot>/src/fmt/print.go"
    ],
    "directories": ["/…", "/…/p"],
    "absent": ["/…/CohereSettings.json", "/…/staticcheck.conf", "/go.work"],
    "trackedIgnored": [{ "index": "/…/.git/index", "root": "/…", "scope": "" }]
}
```

Additive within contract 3, and written once, just before the summary: by the Go engine in a check run whose load
ran, and by the Swift engine in a check run that described its project (#nkk91tv). It declares what the run's
verdict was decided from, so the front door can replay that verdict without starting the engine when nothing it
names has moved: `go list`, SwiftPM and the engines read in subprocesses and in their own processes, where the front
door cannot observe them. The Go engine's record names:

- `files` are the files whose bytes the verdict can depend on: every file of every package in the loaded graph, the
  standard library's and the module cache's too, the project's census, every module's go.mod, each module root's
  go.sum and `vendor/modules.txt`, the go.work in use and its go.work.sum, PATH's `go`, the `bin/go`, `VERSION` and
  `go.env` of the toolchain that ran (one in the module cache when `GOTOOLCHAIN` switched), the go env file, the
  settings file, a staticcheck.conf at or above an analyzed package, the ignore files the census reads, and at a
  go.work root the git index its gitlinks come from. A file in the build cache is never one: go writes it during
  the run, and its bytes follow from what is named.
- `directories` are the directories whose listing decides which packages or files exist: every directory under a
  module root, or a local replacement's, that `./...` could reach, and the package and embedded directories among
  them. None is above a module root, in the build cache, or below a directory whose name starts with `.`, such as
  `.cache`, since a listing the run writes into would never let one replay. The standard library's and the module
  cache's directories are the toolchain's and pinned versions', which the files above stand for.
- `absent` are paths whose absence the run depends on: a go.work in each directory above a module root while none
  is in use, and each of the files above that is not there. An ancestor is only ever named this way, as a path.

The Swift engine's record (`RunInputs.swift` says why each is enough) names:

- `files`: every file below each root a build reads, the project's root, each local and path package's, and at an
  Xcode root each package its project links, walked as the build-input snapshot walks them: no directory whose name
  starts with `.` is entered, so neither `.git`, `.build` nor cohere's `.cache` is, and everything else is, ignored by
  git or not, with the hidden files in each walked directory but `.DS_Store`. Beside them, every target's sources,
  each package's `Package.swift` and `Package.resolved`, the ignore files, the repository's git config and the git
  index the file set consulted, a `.git` file a worktree points through, the settings file, and the toolchain's
  files: the `swift`, `xcrun` and `xcodebuild` PATH finds (and `xcodegen` at a spec's root), the `swift` xcrun names
  with `swift-frontend`, `swift-build` and `swift-package` beside it, the toolchain's `ToolchainInfo.plist`, its
  Xcode's `version.plist`, and each SDK's `SDKSettings.json`. A remote dependency's checkout lives in the engine's
  scratch and is pinned by the resolved file.
- `directories`: every directory those walks listed, every directory the file set's walk listed (a hidden one it
  entered among them), and the directory holding each target source. None is in cohere's cache directory, which
  the file set's walk no longer enters, since the front door writes its table there after every run.
- `absent`: each of the files above that is not there, a `.git` in each directory from a package up to its
  repository, and a `.swift-format` in each directory from a file of ours up to the repository.

The front door adds what only it knows: the settings file it handed the engine, the engine binary it ran, the `swift`
its own `swift --version` went to, and for an engine built from the working tree, that engine's sources. A Swift
record lists every directory whose listing it read, so the front door records no directory holding a file beyond
those: a git config in `.git`, or a toolchain's binary, is decided by its own signature, never by a directory every
git command or install moves. Which toolchain xcrun would pick, and how the engine was chosen, are keyed rather than
named (command/cohere/swift_run_cache.go), since a replay asks neither.

`trackedIgnored` (#m1daxxk) names each git index among `files` that the run read only for what it decides of a walk:
which paths its ignore rules exclude are tracked, and which are gitlinks. Each entry is the index, the root of the
repository it belongs to, and the directory the run walked, relative to that root and slash separated, `""` for the
root. The Go engine names its census's index, scoped to the root below the repository. The Swift engine names the
index its file set read whole (#jem2gsa), scoped to the nearest directory holding every path the file set asked it
about, the package's root and each target source, local and path packages' among them, so `""` when they lie in
different subtrees of the repository: a scope too wide only costs a miss, and one too narrow is a stale replay. Both
read the repository's rules alone, case sensitively (#jgk8ppa), a worktree's shared `info/exclude` among them, as
the digest reads them. The front door keys such an index on `gitignore.TrackedIgnoredDigest` over that scope rather
than on its bytes, which git rewrites on a `git status` and on every `git add`, so a replay survives what decides
nothing. An index the reader cannot read whole, a split or sparse one among them, is keyed on its bytes as an index
not named here is. A reader before it ignores the field, and keys every index on its bytes.

Every path is absolute and cleaned, each list is sorted, without duplicates, and `[]` when empty, and two runs over
an unchanged tree write the same record. A superset only costs a miss, and a path left out is a replay that goes
stale, so whatever the load, or a later cache of the engine's, reads to decide a verdict must be named here. A
check run whose load failed, or that asked for no load, writes none, nor does a Swift run that could not describe its
project, and the front door records nothing for either.

### `lint`

```json
{
    "kind": "lint",
    "findings": 3,
    "rulesRun": 15,
    "filesWalked": 149,
    "nodesVisited": 412233,
    "elapsedMilliseconds": 96,
    "reusedFrom": "",
    "rulesSilent": ["cohere-swift/consistency-no-print"],
    "rulesWatchedAndQuiet": 9,
    "crashes": [{ "file": "/…/X.swift", "error": "…" }],
    "rulesScopedOff": { "cohere-swift/consistency-no-print": 12 },
    "rulesNotConfigured": [],
    "configNote": "",
    "filesReplayed": 140
}
```

Rendered by the existing lint line and coverage printers:
`lint: 3 findings — 15 rules over 149 files, 412233 nodes visited, in 96ms`, followed by the rule
coverage, crash, and config notes. `reusedFrom` names the phase whose walk was reused (`fix`), and
the line then says so instead of printing a duration.

`filesReplayed` counts the files of `filesWalked` whose every rule's verdict was read from the
engine's lint verdict store, `<scratch>/lint-verdicts.json`, rather than computed this run
(#da5fx6d). It is additive within contract 3, as `changedFiles` is: this engine always writes it, a
stream from an engine before it has none, and a front door that does not read it loses only the
count. Every other field reads as it would without the store: a replayed file is still in
`filesWalked` and `nodesVisited`, its findings are written, and it counts toward each rule's
coverage, so a run that replayed and a run under `--no-cache` write the same stream but for this
field and the elapsed times. A verdict is replayed only under the key it was kept under. The store
is keyed as a whole by the engine (the executable's sha256) and the enabled rules with their
options. Each file is keyed by its bytes, its path and its package root, both with links resolved,
its target and the target's kind, and a standalone script's platform. Its typed rules' verdict is
also keyed by the index record that describes the file as it stands, by store and record name (a
content address of what the file's names resolve to), and by the modules the package owns. Never
kept: the package rules, a typed verdict sourcekitd answered, a typed verdict for a file no source
described, a verdict holding a rule's failure, and a file whose text moved during the run. Under
`--no-cache` nothing is read or kept, and the count is 0.

The Go engine's lint record carries `offReasons` (#v11k0g2), additive within contract 3 and the Go
engine's alone: for each rule the settings' go block turns off and explains under its `reasons`, the
reason, as `"offReasons":{"cohere-go/consistency-no-utils-file-name":"Off until ..."}`. It is always
written, `{}` when no off is explained, so a front door tells an engine that explains its offs from one
that never does: it prints a reason beside its rule in the coverage, as `off because ...`, and marks a
rule off with no reason given, which only a project outside our tiers can have. A Swift engine writes
none, and its offs are marked neither way.

The Go engine's lint record carries one more field, additive within contract 3 as the types record's
are, so a front door that does not read it yet loses only what it counts:
`"cache":{"on":true,"hit":412,"missed":3,"written":3,"unstorable":0,"factsDropped":1,"factsSkipped":0,"waves":1}`.
It is what the engine's per-package findings cache did, by package. Every package the load reached,
each analyzed package and test variant and each dependency, was either `hit` or `missed`, or
restored from the binary (see `standardLibraryFromBinary` below). A hit is
restored from its entry, and no analyzer runs on it. A package's key holds its dependencies' keys, so
an edit misses every package above the one edited, though most come out of an analysis as before.
So the packages whose own inputs moved are analyzed first, and each package above them is then
restored by a second key, its own inputs and what the analysis of everything below it came out as
(its types without positions and the facts it exported), when that key holds an entry and every
answer of a fact computed on demand that the entry read of another package still holds; what still
misses is analyzed together, so `waves`, the times the lint phase analyzed packages, is at most 2,
and 0 when everything hit. A package one of whose findings names a file outside it, as vet's
structtag names the line of a field embedded from another package, never takes the second key. On
the load from source a hit by its first key is neither parsed nor type-checked, and its types are
read from the entry's export data only when a package that missed imports it; one restored by its
second key was type-checked, since only the first wave could say it would hit. A miss is checked
and analyzed from source. Of the misses, `written` got an entry, and `unstorable`
held what no entry can hold, a fact gob cannot encode or types export data cannot carry, so it got
none and is checked again next run; a miss that is neither had a load error, had a rule fail on it,
or its write failed, which the engine says on stderr. `factsDropped` counts the facts the written
entries left out because nothing outside their package can name their object, such as a wrapper
closure printf marks: no importer can look such a fact up, so leaving it out changes no finding.
`factsSkipped` counts the restored facts whose object the package's export data does not hold, which
no importer can name either. The entries live in `<root>/.cache/cohere/go/`, beside the front door's
own cache. The cache changes no finding and no other field of any record: a run with it and a run
under `--no-cache` write the same stream but for this field and the elapsed times. Under `--no-cache`
the engine reads and writes no entry, and every count is 0 with `"on":false`. A project with cgo of
its own is cached the same way, its packages with cgo files keyed on the cgo tool's outputs.

A release binary also carries the Go standard library's entries, computed when the release was
built and keyed on everything they read: the release, the Go version, the target, cgo, the go flags,
the analyzers that learn facts with their options, and each file's bytes by its path below `GOROOT`.
A standard-library package whose key matches is restored from the binary, in a cached run and under
`--no-cache` alike, since those entries are part of cohere and nothing of the project's tree; any
mismatch computes it. `"standardLibraryFromBinary":258` counts those packages, which are neither
`hit` nor `missed`, and is written only when it is not 0, so a build that carries none, as every
build but a release does, writes the field as it always has. Under `--no-cache` it, and
`factsSkipped` of what it restored, are the only counts that can be other than 0, with `reloads`.

A few facts are computed only when a rule asks of a function (#w7zmvat), from the function's
package's syntax, which a restored package does not have. Every entry a run writes holds every
answer of its package, computed whole under its key, as the binary's entries do, so a question of
a restored package reads its answer from the entry and reloads nothing. An entry can still hold
only some, as one a run writes when computing them all would need a package it restored and
nothing answers. A question of a restored package that nothing holds an answer to is never
answered "no fact": the pass is handed a placeholder and runs on, so it names every package it
needs, and the run loads those again from source, never restoring them, and runs again; the pass
that ran on placeholders is dropped unwritten, until a round needs no package it did not load
already. `"reloads":2` counts the packages so loaded and `"reloadRounds":1` the rounds, each written only when it is not 0.
Beside them, `"replays":35` counts the questions answered by replaying the order the push walked a
package in, which a cold run makes too; it is written only when it is not 0. A package loaded again that still cannot answer is a crash naming it, never a quiet answer.

`"problems":2` counts what kept the findings cache from being read or written in the lint phase,
the host's build's and every other build's alike (#27r8r24): keys that could not be computed, an
entry that could not be encoded, a write that failed, or an entry an extra build could not write
through. The engine says each one on stderr, as `the findings cache: ...`, and the count carries
them into the record, so a run read only through `--json` still shows its cache could not be
written. A problem never changes a finding. Of the misses whose write failed, only the first is a
problem, so `"writeFailed":412` counts every one, every build's, and a store that refused 412
writes says 412. Each is written only when it is not 0.

The Go engine checks a file under every build our own files constrain on, the host's and the fewest others under
which every file compiles at least once (#72n9mq0), and when it checks more than one it writes `builds`, additive
within contract 3, the host's first:

```
{
    "builds": [
        { "target": "darwin/arm64 cgo", "host": true, "compiledFiles": 412, "analyzedPackages": 349,
          "findingsAdded": 0, "findingsDropped": 0 },
        { "target": "windows/arm64", "host": false, "compiledFiles": 398, "analyzedPackages": 351,
          "findingsAdded": 6, "findingsDropped": 1 }
    ]
}
```

`target` names the build as `GOOS/GOARCH`, with ` cgo` when cgo is on and its tags after `tags`. Every build
analyzes every package its load holds, through a findings cache of its own keyed on its target, cgo and flags, and
`analyzedPackages` counts them; a build whose load found a type error analyzes none. The findings, the withheld
counts and the crashes above are the merge of every build's: a finding stands if any build that compiled its file
and analyzed its package reports it, but for a rule whose findings stand only where every such build reports them
(U1000, and each staticcheck check whose documentation says so), and a `//cohere:ignore` is unused only if it is
unused in every such build. `findingsAdded` counts the findings that stand which this build reported and the host's
did not, and `findingsDropped` those it reported that the merge left out. A run of one build writes no `builds`, so
its record reads as it always has. A file no build compiles stays a named gap, in the summary's `gaps.unchecked`,
and its exclusion reason in the project record says why no build compiles it rather than which constraint the
host's build fails: `no target platform compiles it: !linux && !darwin` for a file whose constraint only rules out
the platforms the project's files name, which a target in the settings' go block can check (#ntamp9e), `only
another architecture than the host's ...`, or `a program of its own, ...` (#rkw4xcn).

A type error only another build finds is a finding of that build, so the lint phase writes it as a compiler finding
record, after the types phase has ended, and the lint record counts it in `typeErrors` (#rw6tg3a), additive within
contract 3 and written only when it is not 0. The phase's finding records are `findings` plus `typeErrors`, and the
front door refuses a lint record whose two counts do not add up to the records that came with it.

Each build's entry says what it covered that no other build did, additive within contract 3 (#ytnvt8k), each field
absent when there is nothing to count. `filesAlone` counts the files of ours in scope that only this build compiles
and that a rule ran on. `notLinted` lists the files of ours in scope that the host's build leaves out and no build
analyzed, absolute and sorted, each under the first build that compiles it, and `notLintedReason` says why: a build
whose load found a type error analyzes no package (`"the windows/arm64 build compiles it, and its load found a type
error, so it analyzed no package"`). The project record names such a file `checked by` its build, since it is written
before any build runs, so the front door reads `notLinted` and names those files as not checked, `not linted: ` and
the reason, in the summary's `gaps.unchecked` and the footer's count, rather than as checked by the build.

An extra build restores from the host's run each package its own findings cache does not hold whose own files,
module and import map are the host's and whose every read of other packages reads the same in the build: the
objects its type check reached, the rules' lookups, the facts its analyzers read and the answers it asked, each
compared by what an importer can observe of it, never by bytes. Its findings and facts are then the host's, so the
merge is the one checking it would give. Each count below is written only when it is not 0: the host's
`"entriesKept":349` counts the packages it kept an entry of for the other builds, and an extra build's
`"untouched":280` the packages the host kept that its walk left untouched, restored unjudged and never analyzed,
since nothing they read changed in the build, their findings the host's and their files the host's checked files,
never a gap, `"visited":21` those its walk visited, each judged,
`"restoredFromHost":301` the packages it restored, `"declinedAtLoad":12` those its load checked after all, since
what they read of types did not hold, and `"declinedAtCheck":3` those its check declined, since a fact or answer they
read did not. `"declinedBy"` counts each decline, and each package never restored, by why: `ownInputs` (its own files differ), `noHostEntry`, `neverRestorable` (what it read cannot be recorded, as with cgo), `readsUnknown`, `exportData` (its export data did not read), `typeReads` (a type it read), `lookups` (a rule's lookup), `factReads` (a fact), `deprecations` (a deprecation) or `answers` (an answer).
`"reloadRounds":2` counts every time the build was loaded again, and `"reloadRoundsBy"` each of those under every
reason it had: `hostDecline` (a package its check declined, at most 2 such rounds) or `missingAnswer` (a question
reached a restored package with no answer of it kept). `"writtenThrough":301` counts the packages it restored whose
entry it then wrote to its own findings cache, once their restore held, so its next run hits them there rather than
check them from source, since the host, hitting its own store then, keeps nothing to restore them from; under
`--no-cache` nothing is written, and it is absent. Under `--timing` nothing is restored from the host, so every
figure is of work that ran.

Each build's record also says where its time went, each figure in wall milliseconds and written only when it is not
0: `"loadMilliseconds":1200` loading the workspace and `"lintMilliseconds":2400` running the rules, every reload
included, an extra build's `"decideMilliseconds":310` deciding which packages restore from the host's run, summed
across the goroutines that decided, so it can exceed the load and lint it ran within, and the host's
`"keepMilliseconds":180` keeping its packages for the other builds, within its lint: their keys, which a run without
a store computes only for this, and what each entry holds of the run's answers, since the entries themselves are
encoded when a build first asks for them, and the host's `"waitMilliseconds":40`, how long the other builds
waited after its lint for its walks of what each package read of others to end, before any could start.

An extra build's record also says what weighed its lint (#b2t9xmf): `"checkedFromSource":120` the packages of its
whole graph its loads checked from source rather than restored, summed over every load, and `"packagesLoaded":1400`
every package its first load loaded, dependencies and test variants included. A build loaded again after a decline
also writes `"checkedFromSourceRounds":[20,100]`, each load's count in order (#k8gty9e); the first load's count over
`packagesLoaded` is the share that weighs its lint. The extra builds' lints
share a budget of one whole build: each takes that share of it once its load has ended, or all of it at or above one
half, so no two near-full builds lint at once, and they are admitted in the order they became ready, a light build
never passing a heavy one already waiting. `"lintWaitMilliseconds":850` is how long its lint waited for that weight,
written only when it is not 0, and `"lintWaited":true` whether it waited at all, so a wait under a millisecond still
reads as one. The host's record writes none of the four, and none reaches a key or the verdict.

Beside `builds` the Go engine writes `"buildsAtOnce":3`, additive within contract 3: how many of the builds beside the
host's ran side by side, decided once (#b2t9xmf, #rmzehfg) by what the heaviest build holds. The first build on another
platform starts first, then every other in build order, the host's own before the other platforms, so heavy builds
never start side by side ahead of the light ones (#awbzy9y), and the first runs alone until its load has ended; there,
with nothing else running, a collection measures the live heap, and the build's holding is that heap less the host's,
measured the same way as the builds started. A build is taken to hold `"buildsWidthPeakFactor":3.5` times that at its
lint's peak in a run with a findings store, and 2.5 times under `--no-cache` (#hv090f4), each the largest ratio
measured in its mode across 14 builds of two trees rounded up to the next half, and the width is
`floor((limit - live) / (holding * factor))`, no more than the builds and at least one, and one at a time under
`--single-threaded` or a limit other than the cap. Beside it, `"buildsWidthLiveBytes":2147483648` is the host's live
heap and `"buildsWidthBuildBytes":536870912` the first build's holding, so a reader redoes the arithmetic with the
memory record's limit, and two runs of one workspace are given the same width; all three are absent when the decision
needed none, because nothing could make the builds more than one at a time. `"buildsPoolSize":16` is how many slots the
run's one pool holds, which every load and check of the run shares, the host's and every extra build's, so however many
builds run side by side the run never runs more of that work at once (#ckm0hsp): one per core, one under
`--single-threaded`. Each build notes into its own buffer, and once every build has ended their results are taken in
build order and their notes in the order they started, up to and including the first that failed in that order, so the
records and stderr are those of a run one build at a time, byte for byte, but these five fields, which no key and no
verdict reads. All five are absent when `builds` is.

Under `--timing`, whose builds run one at a time, each extra build's record writes `"heldAtLoadEndBytes":1170372464`,
what it held beyond the host's live heap as its load ended, measured by a forced collection, and
`"heldAtLintPeakBytes":3555149280`, the most it held while its rules ran, sampled by one every quarter of a second. Their
ratio is what `buildsWidthPeakFactor` stands for, so a tree whose ratio passes it shows in one run. A build under
`--timing` restores nothing from the host's run, so it holds more as its load ends than one that restores, and its ratio
reads lower than the width's case.

### `phase`

```json
{
    "kind": "phase",
    "name": "types",
    "outcome": "notReached",
    "elapsedMilliseconds": 0,
    "detail": "1 type diagnostics — lint findings against wrong semantics are noise"
}
```

`name` is `fix`, `types`, `lint`, or `unused`. `outcome` is `ran`, `skipped`, `notReached` or
`reused`; these are the phases.go outcomes, spelled in camelCase. The engine writes exactly one
`phase` record per phase, every run, in pipeline order, including phases that did not run. A
missing phase record is a protocol error, for the reason `markRemainingNotReached` gives: a phase
absent from the report reads the same as one the reporter forgot.

A phase the caller did not ask for is `skipped` with the detail `not requested`, exactly, or for
unused `not requested — ` and how to ask for it. That detail is the only one that means nobody
asked, and it is not a gap: `--lint` asked for lint and got all of it. Any other `skipped` detail on
a phase that reports findings is a gap. The front door reads the command line too, the way the
TypeScript run does (no phase flag asks for fix, types and lint; `--fix`, `--types` or `--lint`
only for those named; any `--unused` flag for unused as well). So a phase it asked for that comes
back `not requested` is a protocol error, not a smaller run.

### `rule`, only under `--rules` and `--rules-enabled`

```json
{ "kind": "rule", "name": "cohere-swift/force-unwrapping", "severity": "error" }
```

`--rules` prints names, sorted. `--rules-enabled` prints `name<TAB>severity`. The development-build
note goes to stderr, as it does for TypeScript.

### `summary`, always last

```json
{ "kind": "summary", "findings": 4, "complete": true, "exitCode": 1 }
```

- `findings` is the sum of every `finding` record. The front door counts the findings it received
  and refuses a summary that disagrees. Two counts that should agree and do not are a defect.
- `complete` is false when any file, or any phase the caller asked for, that could have produced a
  finding did not run. A phase skipped as `not requested` is not one of them. The
  front door prints `this run did not check everything` from its own phase records anyway. A summary
  calling the run complete over a gap its records show (a requested phase not run, a file without a compiler
  record, a rule crash, an `unreadable` file) is a protocol error. The other direction is believed: an
  engine that says it fell short where no record shows it is taken at its word.
- `exitCode` is what the engine will exit with.

## Exit codes

| Engine exit      | Meaning                                                                                                                                           | Front door                                                                                               |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| 0                | summary written, `findings` 0, `complete` true                                                                                                    | exits 0                                                                                                  |
| 1                | summary written, findings or an incomplete run                                                                                                    | exits 1                                                                                                  |
| 2                | the engine could not run: bad flag, contract mismatch, no package, toolchain missing, refused config (a leftover `.swift-format`, a `format` key) | prints the engine's stderr, then `cohere: the Swift engine did not run, so nothing was checked`, exits 1 |
| other, or killed | crash                                                                                                                                             | `cohere: the Swift engine exited <code> without a summary, so nothing was checked`, exits 1              |

An exit of 0 or 1 without a `summary` record is a crash. An exit code that disagrees with the
summary is a protocol error. The front door owns the final exit code, and there is no path through
it where a run that did not finish prints green.

## Flags

| Flag                                        | Swift meaning in phase 1                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--no-fix`                                  | Writes nothing to the project: no source, no `.build`, no `Package.resolved`. Builds go to the engine's cache outside the project. A package is built from its own `Package.resolved` with resolution disabled, since SwiftPM keeps pins nowhere else, so one with remote dependencies and none fails by name; an Xcode project resolves into the engine's scratch, held to a resolution the repository commits when there is one. The fix phase runs in check mode, recorded as `ran`: each file the formatter would rewrite becomes one `format` finding at its first changed line.                                                                |
| `--fix`                                     | Fix and format only.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `--types`, `--lint`                         | That phase alone, as for TypeScript. A `--lint` run whose typed rules apply to a file still checks, skipped when no input changed, because those rules read the check's index and compile commands, the way TypeScript's lint still loads the checker (#q1574nq). Nothing the compiler said is reported, and types stays `skipped` as `not requested`.                                                                                                                                                                                                                                                                                               |
| `--format`                                  | Formatting is on by default for Swift, unlike TypeScript, where it waits on parity with the existing gate. For Swift that parity is measured: swift-format 604.0.0 as a library is byte-identical to `xcrun swift-format` on 952 of our files. The flag is accepted and changes nothing.                                                                                                                                                                                                                                                                                                                                                             |
| `--format-all`                              | Accepted and changes nothing. Swift formats every file in scope by default, because formatting all of Presence costs a few seconds, not the minutes that made TypeScript format only changed files. Named paths still narrow the scope.                                                                                                                                                                                                                                                                                                                                                                                                              |
| `--format-only`                             | The fix phase narrowed to formatting, as TypeScript's `--format-only`: no rule is asked for a fix, and types, lint and unused are skipped as not requested, so nothing is compiled. With `--no-fix` it is the format gate: each file the formatter would change is a `format` finding, and nothing is written. `--format-all` widens it to every file. Named with `--fix`, `--types`, `--lint`, `--unused` or `--no-format`, the run is refused with exit 2.                                                                                                                                                                                         |
| `--no-format`                               | Formatting left out of a bare run, `--fix` or `--no-fix`, as TypeScript's `--no-format`: the fixers run, types and lint run, and no file is formatted or reported unformatted. The `fix` record counts nothing formatted, and its `formatScope` says formatting was left out. Named with `--format`, `--format-all` or `--format-only`, the run is refused with exit 2.                                                                                                                                                                                                                                                                              |
| `--no-cache`                                | Every phase from source, as TypeScript's `--no-cache`. The run uses a scratch of its own in the system's temporary directory and removes it when it ends, so it reads no package description, build, compiler record or index a run before it left, and writes nothing to the project's `.cache`. Every cache the engine keeps lives under its scratch, so this is all of them. It costs a cold build.                                                                                                                                                                                                                                               |
| `--covered-packages`                        | Prints one `coveredPackages` record after `provenance` and exits 0, building nothing: `checked` is the root and each local package inside it whose files a run at this root checks, and `vendored` each local package under `Vendor/`, whose files the run lists as not checked. It comes from the run's own traversal (manifests only), so the front door can skip a nested package a run already covers (#sxs26s3). At an Xcode root `checked` is the root alone: a local package the project links is outside it and runs as its own. Paths are absolute, standardized as Foundation spells them; compare after resolving symlinks on both sides. |
| `[paths]`                                   | Narrow fix, format and lint to the scope. Types still reports the whole package (see `types`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `--lint-config <file>`                      | The `CohereSettings.json` whose `swift` block configures rules.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `--fix-passes <n>`, `--single-threaded`     | As for TypeScript.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `--rules`, `--rules-enabled`, `--version`   | `rule` and `provenance` records.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `--unused`, `--unused-all`, `--unused-deep` | The `--unused` report: `unused` and `unusedCoverage` records after lint, never counted as findings (see those records). Today it holds `unused-import` and `unused-declaration`, each with its own `unusedCoverage` record. It reads the index the types phase writes, so on a run without types it reads the last check's and counts every file that check did not see as it stands as not checked. A bail before it (a file that does not parse, a type error) records `unused` as `notReached`. `--unused-all` and `--unused-deep` ask for nothing more yet and run the same report.                                                              |
| `--duplicates`                              | The front door's for Swift, never given to the engine, which has no duplicates report: the run checks as it would and prints a duplicates report that did not run, saying why. The Go engine answers it with a `duplicates` record (see that record).                                                                                                                                                                                                                                                                                                                                                                                                |
| `--timing`                                  | Go takes it: the Go engine writes one `timing` record after lint (see that record), which the front door prints as TypeScript's timing table. Not implemented for Swift yet: the Swift engine refuses it with exit 2 and names the flag.                                                                                                                                                                                                                                                                                                                                                                                                             |
| `--explain <file>`                          | Not implemented for Swift yet. The engine refuses with exit 2 and names the flag. A flag that is accepted and ignored reads as a run that did what was asked.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `--tsconfig`                                | Meaningless for Swift. The front door refuses it against a Swift root.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `--directory`                               | Resolved by the front door into `--root`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |

## Config

Rules are configured in the `swift` block of `CohereSettings.json`. Keys are `cohere-swift/<rule>`,
and values are severities and strictness options, never allow lists or ignore names.

A value is a severity, or an array of a severity and one object of options. Only a rule that reads an
option may be given it: today that is `cohere-swift/file-length`'s `max`, a whole number at or below the
house's 2,000, which the rule then judges by. An option no rule reads, a value of the wrong type, a `max`
above the house's (an allowance) and more than one object are each refused by name with exit 2, never
dropped (#4x9h976). The block holds only `rules`, and a rule name cohere-swift does not have is refused the
same way, as is any other key in the block (#naazhm0).

**Formatting is not configured.** Swift has one house format, built into the engine
(`Sources/CohereSwift/Format/HouseSwiftFormat.swift`, ruled 2026-10-03 in #mg4dgjm), the way
TypeScript's Prettier options live in one block of the Nexus tier. Every file the engine owns is
formatted with it, and nothing a repository carries changes it. Two things are refused by name, each
with exit 2 before anything is checked, as a bad flag is:

- A `.swift-format` at or above any file the engine owns, up to the repository root (the package
  itself outside git). The refusal names each file and says to delete it, and to bring any setting it
  needs to the house format instead. A `.swift-format` above the repository root belongs to someone
  else and is not looked at.
- A `format` key in the settings file, at the top level or in the `swift` block. The refusal names
  the file and the key and says to remove it, and to bring any setting it needs to the house format.

**Where the config lives.** The engine reads `CohereSettings.json` from beside `Package.swift`, or
the file `--lint-config` names. When there is none, every house rule runs at `error`, and every run
says so in one line naming the defaults in force and where a config would go:
`  config: no CohereSettings.json beside Package.swift, so every house rule ran at error — put one at <root>/CohereSettings.json to change that`.
That is not the permissive default the TypeScript loader refuses. The TypeScript loader refuses to
lint with nothing configured because the output would look like a clean run. Here, every rule runs,
and the line says why. Approved by @system_cohere, 2026-10-01.

The line travels in the `lint` record as `"configNote"`. It is empty when a config was read, and the
front door prints it under the lint line whenever it is not empty.
