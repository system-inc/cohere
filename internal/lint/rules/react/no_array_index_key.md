# `react/no-array-index-key`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | **84**                |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow usage of Array index in keys

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
