# `no-implicit-coercion`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **52** |
| Plugin | `eslint core` |
| Auto-fixable | yes |
| Needs type information | no |

## What it checks

Disallow shorthand type conversions

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## Violations

52 in the tree. Showing the first few.

**`app/(os-layout)/art/_components/ArtCard.tsx:43`**

```
const [showComment, setShowComment] = React.useState(!!properties.item.rating);
```

> Unexpected implicit coercion encountered. Use `Boolean(properties.item.rating)` instead

**`app/(os-layout)/os/wisdom/_components/WisdomShapingInput.tsx:127`**

```
const canShape = shapeText.trim().length > 0 && !isShaping && !!propertiesTaskId && propertiesTaskId.length > 0;
```

> Unexpected implicit coercion encountered. Use `Boolean(propertiesTaskId)` instead

**`app/(os-layout)/phi/social/_components/PhiSocialCard.tsx:73`**

```
const [showComment, setShowComment] = React.useState(!!properties.post.rating);
```

> Unexpected implicit coercion encountered. Use `Boolean(properties.post.rating)` instead

**`app/api/finance/statements/route.ts:120`**

```
const isIsoDate = (value: string | null): value is string => !!value && /^\d{4}-\d{2}-\d{2}$/.test(value);
```

> Unexpected implicit coercion encountered. Use `Boolean(value)` instead

**`libraries/structure/libraries/nexus/code-quality/lint/rules/ConsistencyNoAmbiguousIdentifierRule.ts:80`**

```
return !!name && /^on[A-Z]/.test(name);
```

> Unexpected implicit coercion encountered. Use `Boolean(name)` instead

### What fixing looks like

Auto-fixable. `eslint --fix` rewrites these, so the cleanup is a command plus a review of the diff.

