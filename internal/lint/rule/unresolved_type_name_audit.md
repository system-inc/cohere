# Unresolved type names (#qy4x7h6)

Base: `29631237ac1e741036c2a0ec4a27fcca393f0e59` (`origin/main`).

A missing generated Next contract does not establish ownership. `IsResolvedSymbol`
rejects missing checkers, nil symbols, declaration-free symbols, and unknown alias
targets. Alias declarations alone do not prove that a target exists. The reader
returns only a bool and is registered with the type-reach guard.

Property ownership sometimes receives only the checker's recovery type, after
symbol identity has been erased. `IsUnresolvedType` recognizes that intrinsic
`any` named `error`; explicit `any`, `unknown`, and no contextual type are different.
Both predicates live beside the dependency readers in `declarations.go`.

## Rule audit

Searches: `rg -n 'IsDeclaredByADependency' internal/lint`,
`rg --files internal/lint/rules` filtered for suffix/naming/identifier rules,
and `rg -n 'ReplaceNode.*(name|Name|identifier|Identifier)|Rename' internal/lint/rules --glob '*.go' --glob '!**/*_test.go'`.
The wider `ctx.ReplaceNode|rule.ReplaceRange` search returned 220 source lines;
most replace expressions, statements, tokens, or module specifiers rather than
rename a type or property binding.

| Rule | Finding and disposition |
| --- | --- |
| `structure/react-component-require-properties-type-suffix` | Changed. The parameter listener and collection pre-pass both treated unresolved names as ours. Both now require a resolved declaration before dependency classification. No checker also means no ownership. Local declarations still report and fix; `.d.ts` and node_modules declarations remain silent. |
| `nexus/consistency-no-abbreviated-identifier` | Changed. Type-reference identifiers already return through `binding.IsForeignName`, but property ownership treated an unresolved contextual, inherited, or combined contract as permission to judge its members. Each of those three paths now declines the error recovery type. This rule has no rename fixer; its existing no-fix behavior remains. |
| `nexus/consistency-require-type-suffix` | Already safe for unresolved references: listeners inspect only alias, interface, and const-shaped variable declarations. No type-reference listener, no fixer. |
| `nexus/consistency-no-ambiguous-identifier` | Already skips type references and qualified type members through `binding.IsForeignName`. No fixer. |
| `nexus/consistency-require-constant-casing` | Judges variable declarations. Its identifier-renaming fixer, `fileLocalRenameFix`, requires a checker, exactly one declaration, and matching symbol identity for every reference; it refuses merged type/namespace symbols. It does not rename unresolved type references. |
| `structure/react-component-require-properties-parameter` | Operates on a declared parameter name. The rename is a suggestion, not an unattended fix, and does not rename its annotation. |
| `structure/consistency-require-matching-file-name` | Reports component declarations; no type-reference rename or fixer. |
| `structure/network-require-hook-request-suffix` | Reports hook declarations/file names, not type-reference names. No fixer. |
| `structure/react-hook-require-result-naming` | Reports names introduced by variable declarations, not annotation references. No fixer. |
| `structure/next-require-api-parameter-name` | Reports declared parameters, not their types. No fixer. |
| `base/consistency-require-pagination-argument-name` | Reports decorated argument declarations, not unresolved type references. No fixer. |
| `react/boolean-prop-naming` | Named prop types resolve to declarations before their property signatures are visited. A nil symbol or empty declarations yields no members; an unknown symbol likewise has no declarations. No identifier-renaming fixer. |
| `structure/network-require-hook-variables-type` | Judges inline/destructured annotation shape, not a type's spelling. No identifier rename. |
| `nexus/consistency-require-matching-return-type` | Judges return expressions; fixes `return;`/`return undefined;`, not type names. |
| `nexus/import-require-node-namespace` | Reports import bindings. Its automatic replacement adds `node:` to the module specifier; it does not rename an identifier. |
| `structure/import-require-react-namespace` | Enforces import/qualification syntax for explicitly imported React bindings. Reports but does not rename type identifiers. |
| `no-useless-rename` | Removes redundant same-spelling import/export/destructuring syntax while preserving the binding's name. Does not choose a replacement type/property name. |

### Boundary of the ownership change

The literal sentence “a rule that judges ... a type's name must resolve it first”
is broader than ownership-based naming rules. The following rules intentionally
operate on configured spelling or type-use syntax. They were inspected and left
unchanged; they are not claimed to satisfy a universal unresolved-name exemption.
Applying the sentence to them would be a separate behavior/parity decision:

| Rule | Why no change |
| --- | --- |
| `@typescript-eslint/no-wrapper-object-types` | Converts uses of boxed built-in types to primitive types, rather than renaming a declaration. A dependency's built-in declaration is precisely what it prohibits. |
| `@typescript-eslint/no-unsafe-function-type` | Prohibits use of the built-in `Function` type; it does not judge a name we own. No fixer. |
| `@typescript-eslint/no-restricted-types` | Explicit configuration bans type-use spellings and can replace whole type expressions, including keywords and literals. A configured ban may intentionally prohibit dependency types. Making every dependency silent would defeat that policy. |
| `id-match` | Configured lexical identifier policy, including references and optionally property writes. No fixer. Its upstream contract can report unresolved identifiers. |
| `id-denylist` | Explicit lexical ban; its documented upstream contract deliberately reports unresolved names (`scope.through`). No fixer. |
| `nexus/consistency-no-stuttering-name` | Reports literal repetition in `result.result`, independently of type ownership. No fixer or proposed replacement name. |

There is only one `require-*-type-suffix` rule in this tree: the React properties
rule above. The other type-suffix rule is `consistency-require-type-suffix`.

## Evidence

Counts below are Go JSON `pass`/`skip` events including parent tests and subtests,
except the focused before/after counts, which count fixture cases only.

| Command | Result |
| --- | --- |
| `bash cloud/setup.sh` | Exit 0; Go 1.27.1; prescribed build ready. TypeScript initialization took 929.951 seconds. |
| `go build ./command/cohere` from cohere | Exit 0 before source changes. |
| `go test ./internal/lint/rules/structure -run '^TestReactComponentRequirePropertiesTypeSuffixRequiresADeclaration$' -v` on unchanged rule | 3 failing / 3 passing cases: missing global, missing import, and collection of a different nested declaration failed. |
| `go test ./internal/lint/rules/nexus -run '^TestConsistencyNoAbbreviatedIdentifierUnresolvedTypeOwnership$' -json` on unchanged rule | 3 failing / 5 passing cases: contextual, inherited, and combined unresolved contracts failed. |
| Focused commands above after changes | 6/6 and 8/8 fixture cases pass. Own declarations still report; dependency declarations and unresolved references are silent. |
| `go test ./internal/lint/rule -run '^TestIsResolvedSymbolRequiresADeclaration$' -json` | 6/6 symbol cases pass. The parent also verifies exactly 2 expected compiler diagnostics and distinguishes missing-type recovery from an own type, explicit any, and absent context. |
| `go test ./internal/lint/rule -json` | 89 pass, 0 skip, 0 fail. |
| `go test ./internal/lint/rules/structure -json` | 1,224 pass, 0 skip, 0 fail. |
| `go test ./internal/lint/rules/nexus -json` | 2,507 pass, 2 skip, 0 fail. The skips are the existing shouting-gate measurement tests. |
| `go test ./internal/testpolicy ./internal/lint/registry ./internal/guard -json` | testpolicy: 4 pass; registry: 26,180 pass, 2 skip; guard: 34 pass; 0 failures. Registry skips are the opt-in scratch scan and live-config reachability check. The type-reach caller scan passes. |
| `go vet ./internal/lint/rule ./internal/lint/rules/structure ./internal/lint/rules/nexus ./internal/guard` | Exit 0; 0 diagnostics. |
| `gofmt -l` on all 7 changed Go files | 0 files listed. |
| `git diff --check` | Exit 0; 0 whitespace issues. |

### Controls

`python3 /tmp/qy4x7h6-controls.py` drove reversible source mutants with the focused
`go test <package> -run <test> -json` commands above, restoring each source in a
`finally` block. Logs are `/tmp/qy4x7h6-control-00.json` through `-12.json`.
One initial silencer variant failed compilation due to an unused import; it was
corrected and rerun, and is not counted as a killed mutant.

| Mutant | Failing fixture cases |
| --- | ---: |
| Remove parameter ownership guard | 4 |
| Remove collection ownership guard | 1 |
| Restore report-without-checker behavior | 1 |
| Remove unresolved contextual contract guard | 1 |
| Remove unresolved inherited contract guard | 1 |
| Remove unresolved combined contract guard | 1 |
| Make resolved-symbol predicate always true | 5 symbol cases |
| Make resolved-symbol predicate always false | 1 symbol case |
| Make unresolved-type predicate always false | Parent assertion |
| Make unresolved-type predicate always true | Parent assertion |
| Treat every suffix type as unowned | 1 |
| Plant a locally declared, wrongly suffixed component type into each suffix fixture | 6/6 |
| Plant an own `durationMs` property into each abbreviation fixture | 8/8 |

All 11 compiling mutants fail assertions. All 14 planted rule-fixture cases fail.
The no-checker regression is separately caught by its guard mutant. Existing
positive import fixtures now have real source declarations; missing modules are
reserved for the explicitly unresolved fixtures.

No rule was added or renamed, so schema/docs rule-list generation was not needed.
No top-level package was added. No ESLint core corpus rule changed, so no
known-gap row moved. No whole-module test run was performed.
