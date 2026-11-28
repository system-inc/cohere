# `@typescript-eslint/prefer-promise-reject-errors`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong Yes**       |
| Violations in ahra     | **7**                |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Require using Error objects as Promise rejection reasons

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
