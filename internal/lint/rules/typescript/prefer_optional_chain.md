# `@typescript-eslint/prefer-optional-chain`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **238** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | yes |
| Needs type information | yes |

## What it checks

Enforce using concise optional chain expressions instead of chained logical ands, negated logical ors, or empty objects

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## Violations

238 in the tree. Showing the first few.

**`app/(os-layout)/_components/TasksCenter.tsx:876`**

```
lastCommentWakeOutcome && lastCommentWakeOutcome.taskId === selectedTaskId
```

> Prefer using an optional chain expression instead, as it's more concise and easier to read

**`app/(os-layout)/_components/kingdom/OsKingdomView.tsx:245`**

```
selectedMember && selectedMember.parent && selectedMemberLookup
```

> Prefer using an optional chain expression instead, as it's more concise and easier to read

**`app/(os-layout)/_components/kingdom/OsKingdomView.tsx:256`**

```
if(!snapshot || !snapshot.team) {
```

> Prefer using an optional chain expression instead, as it's more concise and easier to read

**`app/(os-layout)/_components/profile/OsProfileDateRangeResolve.ts:35`**

```
if(!match || match.days === null) return null;
```

> Prefer using an optional chain expression instead, as it's more concise and easier to read

**`app/(os-layout)/_components/select/TaskTitleFocusContext.tsx:61`**

```
if(!pending || pending.taskId !== taskId) return null;
```

> Prefer using an optional chain expression instead, as it's more concise and easier to read

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

