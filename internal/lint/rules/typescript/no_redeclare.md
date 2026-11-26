# `@typescript-eslint/no-redeclare`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | **1**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | no                   |

## What it checks

Disallow variable redeclaration

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.
