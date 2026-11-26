# `@typescript-eslint/array-type`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **711**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | no                   |

## What it checks

Require consistently using either `T[]` or `Array<T>` for arrays

## Why this recommendation

Not a house-style conflict: the codebase already writes `readonly T[]` 213 times against only 46 `ReadonlyArray<>`, so the rule's default matches existing practice and is fully autofixable.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **No** to **Maybe**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
