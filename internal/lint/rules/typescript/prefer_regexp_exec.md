# `@typescript-eslint/prefer-regexp-exec`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **170** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | yes |
| Needs type information | yes |

## What it checks

Enforce `RegExp#exec` over `String#match` if no global flag is provided

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## Violations

170 in the tree. Showing the first few.

**`app/(os-layout)/contacts/_components/ContactsListView.tsx:27`**

```
const match = birthDate.match(/(\d{4})?-?(\d{2})-(\d{2})/);
```

> Use the `RegExp#exec()` method instead

**`libraries/structure/code-quality/lint/rules/ReactComponentRequirePropertiesTypeSuffixRule.ts:54`**

```
const match = name.match(/[A-Z][a-z]+$/);
```

> Use the `RegExp#exec()` method instead

**`libraries/structure/command-line/StructureAnalyzer.ts:610`**

```
const [, minimumLinesDigits] = argument.match(/^--min-lines=(\d+)$/) ?? [];
```

> Use the `RegExp#exec()` method instead

**`libraries/structure/command-line/StructureAnalyzer.ts:616`**

```
const [, windowDigits] = argument.match(/^--window=(\d+)$/) ?? [];
```

> Use the `RegExp#exec()` method instead

**`libraries/structure/command-line/StructureDoctor.ts:48`**

```
const [, matchedIdentifier] = content.match(/identifier:\s*['"]([^'"]+)['"]/) ?? [];
```

> Use the `RegExp#exec()` method instead

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

