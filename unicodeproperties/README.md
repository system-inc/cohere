# ECMAScript Unicode character properties

Cohere owns these tables. Consumers, including Adamic, can import
`github.com/system-inc/cohere/unicodeproperties` and use `Lookup`, `Names`, and
`Version` instead of maintaining another copy. `Lookup` returns sorted, disjoint,
inclusive code point intervals. Returned slices are shared and must not be changed.

The tables cover Unicode **17.0.0**, matching Node v24.19.0's
`process.versions.unicode === "17.0"`: 38 general categories, 175 scripts,
175 script extensions, and 53 ECMAScript binary properties. There are 1,715
accepted spellings, including the spec's property-name and property-value aliases.
Names are exact and case sensitive. The UCD aggregate Hrkt/Katakana_Or_Hiragana
is excluded because ECMAScript does not accept it. String properties and the
`v` flag are outside this character-property API.

## Generation

From cohere, run:

```sh
go generate ./unicodeproperties
go test ./internal/lint/ecmascript/regexp -run '^TestUnicodePropertiesFresh$'
```

Generation is offline. `internal/ucd/17.0.0.zip` holds unmodified files extracted
from https://www.unicode.org/Public/17.0.0/ucd/UCD.zip:

- PropertyAliases.txt and PropertyValueAliases.txt
- extracted/DerivedGeneralCategory.txt
- Scripts.txt and ScriptExtensions.txt
- PropList.txt and DerivedCoreProperties.txt
- extracted/DerivedBinaryProperties.txt
- emoji/emoji-data.txt
- DerivedNormalizationProps.txt

Upstream UCD.zip SHA-256:
`2066d1909b2ea93916ce092da1c0ee4808ea3ef8407c94b4f14f5b7eb263d28e`.
Vendored subset SHA-256:
`090a55128dd7891e77dc9c0f7975b8db06bdd20ec1c47a7c0f4ba08b1a9498e2`.
The subset uses ZIP DEFLATE and a fixed entry timestamp of 2025-01-01 00:00:00;
files appear in the order above. Unicode's data license is in LICENSE.unicode.

The generator builds general-category aggregates, computes the Unknown script
from the complement of explicitly assigned scripts, and applies Script_Extensions
overrides to Script defaults. ASCII, Any and Assigned are derived explicitly.
Binary properties come from the ECMAScript allowlist, rather than every UCD
binary property. Changes_When_NFKC_Casefolded comes from normalization data.
The generator's `-inventory` flag emits its canonical intervals and aliases as JSON.

## Task #7zm085y inventory and failure controls

Baseline compiler: `839b0cdf364baa36372fa893a4edeb54410524ca`.
Provisioned Go: go1.27.1, `/workspace/adamic-tools/go/bin/go`.
Node: v24.19.0, Unicode 17.0.

Before changing the compiler, a temporary `TestUnicodePropertyInventory` read
`go run ./unicodeproperties/internal/generate -inventory` output, asked Node to
accept each candidate, and compared accepted patterns with esregexp. The command
was `go test ./internal/lint/ecmascript/regexp -run '^TestUnicodePropertyInventory$' -v`.
It made 4,327,167 membership comparisons; early rejection or mismatch stops the
remaining comparisons for that spelling. A spelling was rejected if its basic
`\p` pattern did not compile; otherwise any disagreement among the 12 profiles
made it wrong, and passing all comparisons made it working.

| Family | Working | Rejected | Wrong |
|---|---:|---:|---:|
| General_Category | 65 | 160 | 15 |
| Script | 0 | 688 | 0 |
| Script_Extensions | 0 | 688 | 0 |
| Binary | 32 | 61 | 6 |
| Total | 97 | 1597 | 21 |

Node rejected eight other UCD candidates: Hrkt and Katakana_Or_Hiragana under
each of sc, Script, scx, and Script_Extensions. The accepted-name fixture was
recorded from this inventory independently of the generated tables, so deleting
an alias cannot silently reduce differential coverage.

### Old compiler runs with the new fixtures

With class.go, escape.go and rewrite.go restored from the baseline commit and the
new regexp/properties.go helper temporarily removed, these commands were run:

- `go test ./internal/lint/ecmascript/regexp -run '^TestUnicodePropertiesNode$' -v`:
  exit 1, 97 passing and 1,618 failing property subtests.
- `go test ./internal/lint/ecmascript/regexp -run '^TestUnicodePropertySyntaxNode$' -v`:
  exit 1, 14 passing and 10 failing syntax fixtures.

Representative old failures: Script=Greek, Script_Extensions=Latin and
General_Category=Letter were rejected; Emoji compiled but disagreed with Node
at U+24DC under iu. All compiler files were then restored to the new implementation.

### Compile-checked family mutants

Each mutant was applied separately to tables_generated.go, compiled with
`go test ./internal/lint/ecmascript/regexp -run '^$'` (exit 0, zero tests), and then
run only against its Node differential subtest. Each extended the first interval's
upper endpoint by one, retaining a valid compiled set.

| Canonical table | Targeted command suffix after `go test ./internal/lint/ecmascript/regexp` | Node caught |
|---|---|---|
| sc=Greek | `-run '^TestUnicodePropertiesNode$/^Script=Greek$' -v` | extra U+0374, 1 failing subtest |
| scx=Latin | `-run '^TestUnicodePropertiesNode$/^Script_Extensions=Latin$' -v` | extra U+005B, 1 failing subtest |
| gc=L | `-run '^TestUnicodePropertiesNode$/^General_Category=Letter$' -v` | extra U+005B, 1 failing subtest |
| Emoji | `-run '^TestUnicodePropertiesNode$/^Emoji$' -v` | extra U+0024, 1 failing subtest |

Freshness and other local checks were excluded from these targeted mutant runs;
only Node supplied the expected membership. The tables were restored after every run.

### Additional plants

- Generator Version 17.0.0 -> 17.0.1, leaving tables unchanged:
  `go test ./internal/lint/ecmascript/regexp -run '^TestUnicodePropertiesFresh$' -v`
  exited 1, one failing freshness test.
- propertyAtom returned classRune instead of classSet: compilation with `-run '^$'`
  passed; `go test ./internal/lint/ecmascript/regexp -run '^TestUnicodePropertySyntaxNode$' -v`
  exited 1 with eight failing and 16 passing fixtures, catching range-endpoint
  treatment as well as lost set membership.
- Removed the Script=Greek alias: the full Node test exited 1 at the independently
  pinned accepted-name guard.
- Changed the Node oracle's expected Unicode version to 16.0: the full Node test
  exited 1 at the version guard on Node Unicode 17.0.

All eight mutants/plants were removed before final verification.

### Differential coverage

Node alone decides membership. For every accepted spelling the test probes every
interval's start, end and immediate neighbors, all case-fold group members, both
ends of the code point domain, surrogate boundaries, Greek mu and the micro sign.
Each runs with p/P, outside/inside/negated classes, and u/iu: 20,580 patterns and
64,403,724 membership comparisons. MatchRunes also permits lone surrogate probes;
the 24 syntax fixtures use the public Compile/Test API on 18 subjects.

The syntax fixtures cover strict aliases, invalid range endpoints, trailing dashes,
mixed built-in/property classes, modifier groups, quantifiers, escaped backslashes,
and Annex B identity escapes. The final syntax run makes 216 membership comparisons
and checks acceptance for all 24 fixtures. The freshness test compares regenerated
Go source byte for byte. Node and Go Unicode versions must match the pinned tables.

One failure found while implementing this: regexp2 canonicalizes a class after
each inserted range, and can invert a nearly-full class before later fold members
are added. This made P{Lt} under iu depend on the order of case-fold groups.
Property classes now normalize the complete union and its case closure before
writing ranges, preserving set-atom identity until range syntax has been checked.

## Final package checks

- `go generate ./unicodeproperties`: exit 0, regenerated one table file.
- `go test ./internal/lint/ecmascript/regexp -v`: exit 0, 13 top-level tests;
  1,715 passing property subtests, 24 passing syntax fixtures, and one passing
  freshness test. Membership counts: 64,403,724 property comparisons + 216 syntax comparisons.
- `go vet ./internal/lint/ecmascript/regexp ./unicodeproperties ./unicodeproperties/internal/ucd ./unicodeproperties/internal/generate`:
  exit 0, four packages, zero diagnostics.
- `gofmt -l internal/lint/ecmascript/regexp/*.go unicodeproperties/*.go unicodeproperties/internal/ucd/*.go unicodeproperties/internal/generate/*.go`:
  exit 0, 14 files, zero listed files.
- `git diff --check`: exit 0, zero whitespace diagnostics.

No whole-module tests, landing, or Adamic consumer changes were performed. The
branch goes to cohere's origin for @system_cohere_lint's review. After landing,
Adamic's library should consume this package at the landed SHA and remove its copy.
