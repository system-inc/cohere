# `@typescript-eslint/prefer-nullish-coalescing`

| | |
|---|---|
| **Recommendation** | **No** |
| Violations in ahra | **774** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | yes |

## What it checks

Enforce using the nullish coalescing operator instead of logical assignments or chaining

## Why this recommendation

Stylistic with a cleanup cost that is not obviously worth the convention it buys.

## Violations

774 in the tree. Showing the first few.

**`app/(os-layout)/_components/TasksCenter.tsx:332`**

```
const activeProjectTitle = projectId !== null ? activeProject?.title || projectId : undefined;
```

> Prefer using nullish coalescing operator (`??`) instead of a logical or (`||`), as it is a safer operator

**`app/(os-layout)/_components/kingdom/OsKingdomLayout.ts:118`**

```
if(firstChildX === undefined) {
```

> Prefer using nullish coalescing operator (`??=`) instead of an assignment expression, as it is simpler to read

**`app/(os-layout)/_components/row/TaskRowTitleEditor.tsx:204`**

```
const fallback = !visibleText ? properties.fallbackText || '(no title)' : null;
```

> Prefer using nullish coalescing operator (`??`) instead of a logical or (`||`), as it is a safer operator

**`app/(os-layout)/art/_components/ArtCard.tsx:42`**

```
const [comment, setComment] = React.useState(properties.item.ratingComment || '');
```

> Prefer using nullish coalescing operator (`??`) instead of a logical or (`||`), as it is a safer operator

**`app/(os-layout)/art/_components/ArtGallery.tsx:115`**

```
return { ...item, rating, ratingComment: comment || undefined };
```

> Prefer using nullish coalescing operator (`??`) instead of a logical or (`||`), as it is a safer operator

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

