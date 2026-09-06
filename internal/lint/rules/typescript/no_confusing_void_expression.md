# `@typescript-eslint/no-confusing-void-expression`

| | |
|---|---|
| **Recommendation** | **Yes** |
| Violations in ahra | **226** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | yes |
| Needs type information | yes |

## What it checks

Require expressions of type void to appear in statement position

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.

## Violations

226 in the tree. Showing the first few.

**`app/(os-layout)/_components/kingdom/OsKingdomGraphCanvas.tsx:213`**

```
return () => cancelAnimationFrame(frame);
```

> Returning a void expression from an arrow function shorthand is forbidden. Please add braces to the arrow function

**`app/(os-layout)/_components/kingdom/OsKingdomGraphCanvas.tsx:301`**

```
return () => cancelAnimationFrame(frame);
```

> Returning a void expression from an arrow function shorthand is forbidden. Please add braces to the arrow function

**`app/(os-layout)/contacts/_components/ContactsListView.tsx:123`**

```
onChange={(event) => setQuery(event.target.value)}
```

> Returning a void expression from an arrow function shorthand is forbidden. Please add braces to the arrow function

**`app/(os-layout)/contacts/_components/ContactsListView.tsx:133`**

```
onClick={() => setActiveType(null)}
```

> Returning a void expression from an arrow function shorthand is forbidden. Please add braces to the arrow function

**`app/(os-layout)/contacts/_components/ContactsListView.tsx:144`**

```
onClick={() => setActiveType(activeType === 'Person' ? null : 'Person')}
```

> Returning a void expression from an arrow function shorthand is forbidden. Please add braces to the arrow function

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

