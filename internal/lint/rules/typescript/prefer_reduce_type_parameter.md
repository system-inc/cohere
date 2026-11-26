# `@typescript-eslint/prefer-reduce-type-parameter`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **2**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | yes                  |
| Needs type information | yes                  |

## What it checks

Enforce using type parameter when calling `Array#reduce` instead of using a type assertion

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.
