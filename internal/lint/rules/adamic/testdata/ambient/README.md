# Ambient declaration probes (#4emavvv)

The rule reports ambient variable, function and class bindings, string-named
module augmentations, and global augmentations in `.ts`, `.tsx` and `.a` sources.
It uses `SourceFile.IsDeclarationFile`, rather than guessing `.d.ts` from a suffix.
A reference reports only when a runtime expression resolves to a declaration in
the referenced declaration file/package/library. Repeated uses report once.

## Scope decisions

- An unused directive stays quiet: the accepted program references `phantom.d.ts`
  but never uses its declaration. Strict tsc accepts it and Node completes. A
  type-query-only use also stays quiet; it adds no runtime read.
- Backed class-field `declare readonly` annotations stay quiet. The accepted
  program checks both a constructor-assigned field and an inherited defined
  field; strict tsc accepts both and Node completes. Property annotations are
  outside this ambient-binding rule; this rule does not prove initialization of
  arbitrary declared fields.
- Declaration files and source extensions other than `.ts`, `.tsx`, `.a` are
  outside the requested scope. Ordinary import/export, defined values, narrowed
  writes and value-returning callbacks assigned to void types stay quiet.
- No base-tier or Structure settings changed. This house rule does not change an
  ESLint core corpus row, so no known-gap entry was removed.

## Strict TypeScript / Node controls

Go 1.27.1, TypeScript 6.0.3, Node 24.19.0. From the cohere root, with the pinned
TypeScript package installed locally:

```sh
python3 internal/lint/rules/adamic/testdata/ambient/probe.py /absolute/path/to/typescript/bin/tsc
```

12 programs: 12 tsc exits 0, 11 Node exits 1 (the expected runtime error),
1 Node exit 0 (all legal runtime controls). The runner checks the exits and error
classes; it does not merely print them. Options include strict,
noUncheckedIndexedAccess, exactOptionalPropertyTypes and erasableSyntaxOnly.

```text
accepted: tsc=0, node=0, completed
class: tsc=0, node=1, ReferenceError: Missing is not defined
const: tsc=0, node=1, ReferenceError: limit is not defined
function: tsc=0, node=1, ReferenceError: missing is not defined
global-method: tsc=0, node=1, TypeError: [1].missing is not a function
global-value: tsc=0, node=1, ReferenceError: missing is not defined
let: tsc=0, node=1, ReferenceError: limit is not defined
module: tsc=0, node=1, TypeError: (intermediate value).missing is not a function
reference: tsc=0, node=1, ReferenceError: missing is not defined
reference-lib: tsc=0, node=1, ReferenceError: window is not defined
reference-types: tsc=0, node=1, ReferenceError: missing is not defined
var: tsc=0, node=1, ReferenceError: limit is not defined
```

## Before / after and tests that can fail

Base: `febe188a940ba395ed5ff71062f9167f5ebb4ec0` (`origin/main`).
The first run, before either implementation file existed, failed all 11 refused
form subtests (expected one `ambientDeclaration`, received zero).
The final fixtures were also run with both new implementation files physically
removed, using the registration-resolving fallback to the old inert engine:

```sh
go test ./internal/lint/rules/adamic -run '^TestNoAmbientDeclarations' -v
```

Exit 1: 34 subtests failed, including all 11 refused forms and `.ts`, `.tsx`,
`.a` source cases. The clean neighbours' planted declarations and out-of-scope
files' `.ts` controls also failed, showing those tests can detect an inert rule.
With the implementation restored, the full package command
`go test -json ./internal/lint/rules/adamic` passed 49 test functions and
446 subtests, with zero failures or skips.

The new suite has 3 parallel test functions, 34 parallel subtests: 11 refused
forms, 14 clean neighbours with planted ambient bindings, and 9 source/declaration
file cases (each excluded file has a reporting `.ts` control). All 11 refused
programs and both versions of all 14 neighbours have zero syntactic and zero
semantic diagnostics under Adamic's strict options. Each refusal also checks the
finding span and that its message names the declaration/reference and runtime
reason.

18 mutants were applied individually to the implementation, run with the same
command, and restored in `finally` blocks. Every mutant exited 1 with an assertion
failure (none counted a build error as a caught mutant):

| Mutant | Failing subtests | Example failing test |
| --- | ---: | --- |
| `drop-ambient-modifier-guard` | 13 | `TestNoAmbientDeclarationsNeighbours/unused_reference` |
| `drop-class` | 1 | `TestNoAmbientDeclarations/class` |
| `drop-declaration-file-guard` | 1 | `TestNoAmbientDeclarationsFileScope/.d.ts` |
| `drop-function` | 1 | `TestNoAmbientDeclarations/function` |
| `drop-global` | 2 | `TestNoAmbientDeclarations/global-value` |
| `drop-module` | 3 | `TestNoAmbientDeclarations/module` |
| `drop-reference-association` | 5 | `TestNoAmbientDeclarationsNeighbours/unrelated_runtime_value` |
| `drop-reference-declaration-file-guard` | 1 | `TestNoAmbientDeclarationsNeighbours/reference_to_defined_import` |
| `drop-reference-deduplication` | 2 | `TestNoAmbientDeclarations/reference` |
| `drop-reference-lib` | 1 | `TestNoAmbientDeclarations/reference-lib` |
| `drop-reference-path` | 1 | `TestNoAmbientDeclarations/reference` |
| `drop-reference-types` | 1 | `TestNoAmbientDeclarations/reference-types` |
| `drop-reference-use` | 3 | `TestNoAmbientDeclarations/reference` |
| `drop-runtime-use-guard` | 1 | `TestNoAmbientDeclarationsNeighbours/type_reference_only` |
| `drop-source-extension-guard` | 3 | `TestNoAmbientDeclarationsFileScope/.js` |
| `drop-string-module` | 1 | `TestNoAmbientDeclarations/module` |
| `drop-variable` | 26 | `TestNoAmbientDeclarations/const` |
| `report-field-annotations` | 3 | `TestNoAmbientDeclarationsNeighbours/backed_field_annotation` |

## Repository checks

- `go test -json ./internal/testpolicy ./internal/lint/registry ./internal/guard`:
  59 test functions and 26,124 subtests passed; zero failures, two registry tests
  skipped. `TestEveryRegisteredRuleIsReachableFromTheLiveConfig` requires the
  private ahra checkout (`COHERE_CORPUS_AHRA`); `TestNoScratchFilesRemain` is
  opt-in (`COHERE_CHECK_SCRATCH=1`) during active probe work.
- `gofmt -l internal/lint/rules/adamic/no_ambient_declarations*.go`: zero paths.
- `go vet ./internal/lint/rules/adamic ./policy`: exit 0, zero diagnostics. The
  settings set is JSON only, so it is not a Go package to vet.
- `go run ./internal/settingsschema/tools/generate`: exit 0, three schema files
  changed to include the new rule.

The first docs capture failed in existing Tailwind tests: inherited
`GOFLAGS=-buildvcs=false -trimpath` made `runtime.Caller` return a module-relative
path, while `vendored.TailwindPackageRoot` expects a real source path. All six
snapshot files exist. With real source paths,
`GOFLAGS='-buildvcs=false -trimpath=false' go test -v ./internal/lint/rules/tailwind/vendored`
passed its one snapshot test. Documentation capture is run with the same
untrimmed-source setting; no tests or assertions are disabled and no Tailwind
source changes are included.

An additional pre-commit check,
`COHERE_CHECK_SCRATCH=1 go test -v ./internal/lint/registry -run '^TestNoScratchFilesRemain$'`,
ran one test and failed it on `internal/corpus/probe_test.go`. This is an existing
tracked helper file: `git ls-files --error-unmatch internal/corpus/probe_test.go`
returns one path, and `git diff febe188a940ba395ed5ff71062f9167f5ebb4ec0 -- internal/corpus/probe_test.go`
returns zero lines. Its `runProbe` helper is unrelated to this rule. It is left
unchanged; the failure is reported for the owner rather than deleting or renaming
an existing corpus helper in this task.

The completed command
`GOFLAGS='-buildvcs=false -trimpath=false' go run ./internal/docsdata/tools/generate -capture`
exited 0: 44 package passes, zero failure lines, 43,082 captured records,
15,413 distinct cases with options. It wrote six documentation data files; the
unrelated pre-existing `--open` addition to `docs/data/cli.json` is left unstaged
and excluded from this commit. The five retained docs/data files list the new
rule, its inherited set membership, schema properties, and captured fixture pair.
The three schema/ files and five docs/data files are the only generated changes
retained in the commit.
