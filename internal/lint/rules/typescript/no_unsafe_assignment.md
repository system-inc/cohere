# `@typescript-eslint/no-unsafe-assignment`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **387**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow assigning a value with type `any` to variables and properties

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.
