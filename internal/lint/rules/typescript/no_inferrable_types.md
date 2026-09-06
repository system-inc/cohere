# `@typescript-eslint/no-inferrable-types`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **203** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | yes |
| Needs type information | no |

## What it checks

Disallow explicit type declarations for variables or parameters initialized to a number, string, or boolean

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## Violations

203 in the tree. Showing the first few.

**`libraries/structure/libraries/nexus/source/collections/Array.ts:69`**

```
function getRandom<T>(array: ReadonlyArray<T>, strict: boolean = true): T | undefined {
```

> Type boolean trivially inferred from a boolean literal, remove type annotation

**`libraries/structure/libraries/nexus/source/coordination/BackoffTask.ts:34`**

```
private isRunning: boolean = false;
```

> Type boolean trivially inferred from a boolean literal, remove type annotation

**`libraries/structure/libraries/nexus/source/coordination/BackoffTask.ts:35`**

```
private isInterrupted: boolean = false;
```

> Type boolean trivially inferred from a boolean literal, remove type annotation

**`libraries/structure/libraries/nexus/source/coordination/BackoffTask.ts:36`**

```
private attemptCount: number = 0;
```

> Type number trivially inferred from a number literal, remove type annotation

**`libraries/structure/libraries/nexus/source/coordination/Delay.ts:34`**

```
jitter: boolean = true,
```

> Type boolean trivially inferred from a boolean literal, remove type annotation

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

