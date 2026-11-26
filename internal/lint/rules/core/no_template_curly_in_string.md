# `no-template-curly-in-string`

|                        |                |
| ---------------------- | -------------- |
| **Recommendation**     | **Strong Yes** |
| Violations in ahra     | **2**          |
| Plugin                 | `eslint core`  |
| Auto-fixable           | no             |
| Needs type information | no             |

## What it checks

Disallow template literal placeholder syntax in regular strings

## Why this recommendation

Catches a defect rather than a preference, and the cleanup is small enough to do in one pass.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
