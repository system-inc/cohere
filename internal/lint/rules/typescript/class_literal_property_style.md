# `@typescript-eslint/class-literal-property-style`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **8** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Enforce that literals on classes are exposed in a consistent style

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## Violations

8 in the tree. Showing the first few.

**`libraries/structure/libraries/nexus/source/validation/schema/ArraySchema.ts:23`**

```
get typeName(): string {
```

> Literals should be exposed using readonly fields

**`libraries/structure/libraries/nexus/source/validation/schema/BooleanSchema.ts:16`**

```
get typeName(): string {
```

> Literals should be exposed using readonly fields

**`libraries/structure/libraries/nexus/source/validation/schema/DateSchema.ts:23`**

```
get typeName(): string {
```

> Literals should be exposed using readonly fields

**`libraries/structure/libraries/nexus/source/validation/schema/FileSchema.ts:20`**

```
get typeName(): string {
```

> Literals should be exposed using readonly fields

**`libraries/structure/libraries/nexus/source/validation/schema/NumberSchema.ts:16`**

```
get typeName(): string {
```

> Literals should be exposed using readonly fields

### What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count above the real cost.

