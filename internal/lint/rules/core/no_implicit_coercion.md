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

## Where cohere is deliberately quieter than ESLint

Upstream's `isNumeric` is syntactic (a numeric literal, or a call to `Number`, `parseInt` or
`parseFloat`), so `1 * zoom` reports and recommends `Number(zoom)` even when `zoom` is already a
`number`. Nothing is coerced, and the advice converts nothing. cohere asks the checker
(`noImplicitCoercionIsAlready`) and stays silent on the four number arms (`+x`, `-(-x)`, `1 * x`,
`x - 0`) when every union constituent of the operand's type, read through a type parameter's
constraint, is number-like (an intersection counts when any part is, so branded numbers qualify). On
`-(-x)` a bigint qualifies too, since that is an identity on a bigint; `+x`, `1 * x` and `x - 0` on a
bigint are compile errors and runtime TypeErrors and keep reporting. `any`, `unknown`,
`number | undefined` and strings keep reporting. The string and boolean arms are unchanged.

The real sites that went silent, all four of the rule's findings in ahra:
`libraries/structure/source/components/maps/Map.tsx:747`, `:765`, `:784` (`context.lineWidth = 1 * zoom`)
and `libraries/structure/source/components/maps/MapDrawing.ts:220` (`const dotRadius = 1.0 * zoom`).
4 findings before, 0 after. No case in ESLint's 141-case corpus moved.

Fixtures: `TestNoImplicitCoercionIsSilentWhenNothingIsCoerced` in `no_implicit_coercion_test.go`, twelve
silent rows (both real shapes, each arm on a number, literal unions, a branded number, a numeric enum,
`-(-big)`, a constrained type parameter, a non-null-asserted number) and twelve reporting controls. The
differential harness records the four sites in `internal/differential/acknowledged.go` as gate-only
findings.

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

