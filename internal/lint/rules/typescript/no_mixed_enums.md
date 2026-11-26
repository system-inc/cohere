# `@typescript-eslint/no-mixed-enums`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | **1**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow enums from having both number and string members

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
