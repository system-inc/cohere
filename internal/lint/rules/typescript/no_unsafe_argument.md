# `@typescript-eslint/no-unsafe-argument`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **90**               |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow calling a function with a value with type `any`

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.
