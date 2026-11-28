# `@typescript-eslint/require-array-sort-compare`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **4**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Require `Array#sort` and `Array#toSorted` calls to always provide a `compareFunction`

## Why this recommendation

Already disabled in VerifySettings.json, and 3 of 4 sites are `results.sort()` on small number arrays inside PromiseGroup.test.ts assertions where the default lexicographic sort is harmless.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Strong Yes** to **Maybe**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
