# `@typescript-eslint/consistent-type-definitions`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **872** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | yes |
| Needs type information | no |

## What it checks

Enforce type definitions to consistently use either `interface` or `type`

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## Violations

872 in the tree. Showing the first few.

**`baselines/DumpRuleInventory.ts:40`**

```
type ConfigurationBlockType = {
```

> Use an `interface` instead of a `type`

**`libraries/structure/command-line/Structure.ts:1384`**

```
type EditType = { start: number; end: number; replacement: string };
```

> Use an `interface` instead of a `type`

**`libraries/structure/libraries/nexus/source/command-line/CommandArguments.ts:60`**

```
export type PendingCommandArgumentFlagValueType = {
```

> Use an `interface` instead of a `type`

**`libraries/structure/libraries/nexus/source/command-line/CommandArguments.ts:234`**

```
type CommandArgumentClaimsType = {
```

> Use an `interface` instead of a `type`

**`libraries/structure/libraries/nexus/source/coordination/TrackedPromise.ts:8`**

```
export type TrackedPromiseType<T> = {
```

> Use an `interface` instead of a `type`

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

