# `init-declarations`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **476** |
| Plugin | `eslint core` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Require or disallow initialization in variable declarations

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## Violations

476 in the tree. Showing the first few.

**`app/(os-layout)/_components/TasksCenter.tsx:109`**

```
let lowerBound: number;
```

> Variable 'lowerBound' should be initialized on declaration

**`app/(os-layout)/_components/TasksCenter.tsx:110`**

```
let upperBound: number | null;
```

> Variable 'upperBound' should be initialized on declaration

**`app/(os-layout)/_components/drag/TasksDragContext.tsx:376`**

```
let reparentToTaskId: string | undefined;
```

> Variable 'reparentToTaskId' should be initialized on declaration

**`app/(os-layout)/_components/kingdom/OsKingdomLayout.ts:114`**

```
let firstChildX: number | undefined;
```

> Variable 'firstChildX' should be initialized on declaration

**`app/(os-layout)/_components/kingdom/OsKingdomLayout.ts:125`**

```
let x: number;
```

> Variable 'x' should be initialized on declaration

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

