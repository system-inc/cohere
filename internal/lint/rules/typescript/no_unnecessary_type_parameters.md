# `@typescript-eslint/no-unnecessary-type-parameters`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **27** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | yes |

## What it checks

Disallow type parameters that aren't used multiple times

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## Violations

27 in the tree. Showing the first few.

**`libraries/structure/libraries/nexus/source/command-line/CommandArguments.ts:354`**

```
export function jsonFlag<ParsedType>(flags: CommandArgumentFlagsType, key: string): ParsedType | undefined {
```

> Type parameter ParsedType is used only once in the function signature

**`libraries/structure/libraries/nexus/source/structured-text/json/Json.ts:42`**

```
export function convertRowsToJson<RowType extends Record<string, unknown>>(
```

> Type parameter RowType is used only once in the function signature

**`libraries/structure/libraries/nexus/source/structured-text/tables/Csv.ts:21`**

```
export function convertRowsToCsv<TRow extends Record<string, unknown>>(
```

> Type parameter TRow is used only once in the function signature

**`libraries/structure/libraries/nexus/source/time/TimeSeriesProcessors.ts:434`**

```
export function getTopBarDataKey<T extends Record<string, number | string>>(
```

> Type parameter T is used only once in the function signature

**`libraries/structure/libraries/nexus/source/types/Constructor.test.ts:441`**

```
function createInstance<T>(classType: unknown, ...constructorArguments: unknown[]): T | null {
```

> Type parameter T is used only once in the function signature

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

