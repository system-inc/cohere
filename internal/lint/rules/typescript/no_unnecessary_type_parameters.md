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

## Where cohere is deliberately quieter than ESLint

The exact type-equality idiom reports in ESLint and is silent here:

```ts
type IsExactlyType<TLeft, TRight> =
    (<T>() => T extends TLeft ? 1 : 2) extends <T>() => T extends TRight ? 1 : 2 ? true : false;
```

`T` used once is the mechanism, not decoration. The checker defers a conditional type whose check
type is an unresolved type parameter and relates two deferred conditionals only when their extends
types are identical, which is how this asks "is L exactly R" where assignability would let `any`
through. The rule's suggestion, replacing `T` with its constraint, resolves both sides eagerly and
destroys the comparison, so both findings are false.

The condition (`isExactEqualityWitnessParameter`): the owner's return type is a conditional type
whose check type is this parameter (by symbol, through parentheses), and the owner is itself the
check or extends operand of an enclosing conditional type (through parentheses). Only a function type
or a constructor type can stand there, so a declaration, a `declare function`, a method or a call
signature returning the same conditional, any of which a caller can invoke as a disguised cast,
still reports.

The real site is `libraries/structure/libraries/nexus/source/types/UnionFromClasses.test.ts:23`, two
findings (columns 7 and 49) before and none after: 28 findings before, 26 after. Upstream's own corpus
pins the idiom as reporting (invalid57, `Equal<X, Y>` over `Compute`); that case moved out of the
reporting table and is asserted silent.

Fixtures: `TestNoUnnecessaryTypeParametersRecognizesTheExactEqualityIdiom` in
`no_unnecessary_type_parameters_test.go`, four silent rows (the real site verbatim, invalid57
verbatim, a parenthesized check type, the constructor-type spelling) and six reporting controls. The
differential harness records both columns of the real site in `internal/differential/acknowledged.go`
as gate-only findings.

`ObjectTypes.ts:93` `typeOnly<Shape>(): Shape` is still reported, and correctly: it is a return-only
type parameter on a callable function, a disguised cast by construction, and its one sanctioned use is
a decision for Kirk rather than something the rule can tell from types.

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

