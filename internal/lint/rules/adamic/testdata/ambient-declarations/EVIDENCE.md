# Verification for #4emavvv

Base: `06af2e399c6c1fb93fd570b774027942747400b4` (`origin/main` at checkout).
All commands run from cohere, after sourcing `/workspace/adamic-tools/env.sh`.

## Before the implementation

`go build ./command/cohere`: exit 0.

`go test ./internal/lint/rules/adamic -run TestNoAmbientDeclarations -v`,
with the new fixtures and registration lookup but no implementation: exit 1,
35 failing subtests. All 12 refusal subtests expected one finding and received
zero: const, let, var, function, class, global-value, global-method, module,
reference, reference-types, reference-lib, reference-transitive. The remaining
23 subtests failed at their positive controls (13 neighbours and 10 file-scope
cases). The original, smaller run failed all 27 subtests too.

The additional `reference-library-name` fixture was introduced while reviewing the
initial implementation. `go test ./internal/lint/rules/adamic -run
TestNoAmbientDeclarationsRefused/reference-library-name -v` failed with zero
findings before the compiler-library-identity guard was added. Its strict tsc
probe also passed and Node threw ReferenceError for customLimit.

## Runtime witnesses

The command in README.md uses tsc 6.0.3, `--strict --lib es2024`, and Node
v24.19.0. On the final fixtures: 14 accepted main.ts files, 13 expected Node
failures, one successful field control. Eleven failures are ReferenceError;
global-method and module are TypeError. The inherited field control exits 0
and prints `2`.

## Scope decisions

Only .ts, .tsx and .a source files are judged. Declaration files are identified
with SourceFile.IsDeclarationFile, not just a filename suffix. Ordinary imports,
exports, defined values, narrowed writes and value-returning functions assigned
to void function types stay quiet in fixtures. Each quiet fixture also plants an
ambient const, or moves the same declaration into an in-scope file, to show that
its positive assertion can fail.

An unused reference is erased and supplies no runtime operation to fail; unused
path, types and lib directives stay quiet. Type-query-only uses, shadowed names
and configured standard-library declarations stay quiet too. Used path, types,
lib and transitive references have separate Node-failing probes. Two reads through
one directive produce one finding.

The tested `declare readonly x: number` annotates a field that Base defines. It
stays quiet, supported by its successful Node probe. Field type refinements and
field initialization promises are outside this ambient-binding rule.

No ESLint core rule changed, so no known-gaps.json row was removed. No new
package was introduced. Base and structure settings were not edited.

## Mutation controls

24 of 24 explicit source mutants were caught by failing tests, not build failures.
Each command was `go test <package> -run <test> -v`; the package was
`./internal/lint/rules/adamic` except drop-reference-read-permission, which used
`./internal/lint/rule`. Sources were restored after each run.

| Mutant | Failing test(s) |
| --- | --- |
| drop-const | `TestNoAmbientDeclarationsRefused/const` |
| drop-let | `TestNoAmbientDeclarationsRefused/let` |
| drop-var | `TestNoAmbientDeclarationsRefused/var` |
| drop-function | `TestNoAmbientDeclarationsRefused/function` |
| drop-class | `TestNoAmbientDeclarationsRefused/class` |
| drop-global | `TestNoAmbientDeclarationsRefused/global-value`, `TestNoAmbientDeclarationsRefused/global-method` |
| drop-module | `TestNoAmbientDeclarationsRefused/module` |
| include-declaration-files | `TestNoAmbientDeclarationsFileScope/main.d.ts` |
| include-other-extensions | `TestNoAmbientDeclarationsFileScope/main.js`, `TestNoAmbientDeclarationsFileScope/main.jsx` |
| include-field-annotations | `TestNoAmbientDeclarationsNeighbours/field` |
| include-defined-variables | `TestNoAmbientDeclarationsNeighbours/defined` |
| include-defined-functions | `TestNoAmbientDeclarationsNeighbours/defined_function` |
| include-unused-references | `TestNoAmbientDeclarationsNeighbours/unused_type_reference`, `TestNoAmbientDeclarationsNeighbours/unused_library_reference`, `TestNoAmbientDeclarationsNeighbours/unused_reference` |
| repeat-reference-reports | `TestNoAmbientDeclarationsRefused/reference` |
| drop-reference-reads | `TestNoAmbientDeclarationsRefused/reference`, `TestNoAmbientDeclarationsRefused/reference-types`, `TestNoAmbientDeclarationsRefused/reference-transitive`, `TestNoAmbientDeclarationsRefused/reference-lib` |
| drop-reference-transitivity | `TestNoAmbientDeclarationsRefused/reference-transitive` |
| include-type-queries | `TestNoAmbientDeclarationsNeighbours/type_query_reference` |
| include-configured-libraries | `TestNoAmbientDeclarationsNeighbours/unused_library_reference` |
| drop-path-resolution | `TestNoAmbientDeclarationsRefused/reference`, `TestNoAmbientDeclarationsRefused/reference-transitive` |
| drop-types-resolution | `TestNoAmbientDeclarationsRefused/reference-types` |
| drop-lib-resolution | `TestNoAmbientDeclarationsRefused/reference-lib` |
| drop-reference-read-permission | `TestAProgramViewAdmitsExactlyTheDeclaredReads/ReferenceTargets` |
| basename-only-library-filter | `TestNoAmbientDeclarationsRefused/reference-library-name` |
| go-whitespace-for-declaration | `TestNoAmbientDeclarationsRefused/const` |

## Package checks

`go test ./internal/lint/rules/adamic -json`: 46 top-level tests and 395
subtests passed, zero failures. This includes the 36 new fixture subtests.

`go test ./internal/lint/rule -json`: 35 top-level tests and 27 subtests
passed, zero failures. Both read-permission and expired-view guards include
ReferenceTargets.

`go run ./internal/settingsschema/tools/generate`: exit 0; wrote
CohereSettings.md, CohereSettings.schema.json and NexusCohereSettings.schema.json
under schema/.

`go vet ./internal/lint/rules/adamic ./internal/lint/rule
./internal/lint/configuration ./policy`: exit 0, zero diagnostics in four
packages. `gofmt -l` over the five touched Go files: zero files.

The first required docs capture exposed a repository whitespace-guard failure
in this rule and a 600-second timeout in the unchanged regexp package's
TestUnicodePropertiesNode. It was stopped after those failures. A U+FEFF in the
const fixture then failed the diagnostic-name assertion before replacing
strings.TrimSpace with text.TrimWhitespace. The full Adamic and text packages
passed afterward. The correction also has the explicit whitespace mutant above.

`go test ./internal/lint/ecmascript/text -json`: 17 top-level tests, 12 subtests,
zero failures. `go test ./internal/lint/ecmascript/regexp -timeout 30m -json`:
13 top-level tests, 1,923 subtests, zero failures; elapsed 353.605 seconds. No
regexp implementation or test was changed.

## Final documentation capture

`go run ./internal/docsdata/tools/generate -capture`: exit 0; 44 tested packages
passed, zero package failures, 43,009 captured records, 15,411 distinct optioned
cases. The command inherited setup's `-buildvcs=false -trimpath` and added:

```
GOFLAGS: -p=1 -gcflags=github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored=-trimpath=
```

Serial package scheduling removed contention from the existing Unicode oracle.
The package-specific compiler flag retains absolute runtime.Caller paths for the
Tailwind test-fixture helper: all six snapshot files were present, but setup's
-trimpath otherwise made the unchanged helper search an import-relative path.
Its unchanged snapshot test passed with this flag. No baseline implementation,
assertion or fixture was changed or skipped. A prior serial capture exposed this
path issue; the final capture passed Tailwind and TypeScript too.

Generated files retained are the rule's example, rules/sets catalogs and schema
copies. An unrelated pre-existing Tailwind example wording refresh was reverted
as required by the task's generated-file scope rule.

## Static program-read integration

The first final gate caught the missing ReferenceTargets entry in its static
method/read table. The new fixture
TestReadProgramFactsSeesCodeNotComments/compiler-resolved_reference_targets
failed before that entry was added: reads=0, declared=ReadsOtherFiles and an
unseen Program.ReferenceTargets call. The method is now mapped to ReadsOtherFiles.
This is an additional failing-before control beside the 24 explicit rule/runtime
mutants above.

## Final repository gates

`go test ./internal/testpolicy ./internal/lint/registry ./internal/guard -json`:
all three packages passed, zero failures, using the same serial/fixture-helper
GOFLAGS as the successful capture. Counts:

- guard: 19 top-level tests, 15 subtests passed; 0 top-level skips.
- registry: 36 top-level tests, 26095 subtests passed; 2 top-level skips.
- testpolicy: 4 top-level tests, 0 subtests passed; 0 top-level skips.

Registry's existing skips require COHERE_CORPUS_AHRA (private consumer checkout)
and COHERE_CHECK_SCRATCH=1 (opt-in scratch guard during live work). Neither was
enabled. No private consumer census was performed; the task assigns that to the
owner at landing.

`go vet ./internal/guard`: exit 0, zero diagnostics, in addition to the four
previously vetted packages. `gofmt -l` over all six touched Go files: zero files.
`git diff --cached --check`: clean.
