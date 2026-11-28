# `require-atomic-updates`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **31**         |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow assignments that can lead to race conditions due to usage of `await` or `yield`

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
