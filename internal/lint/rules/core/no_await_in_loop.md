# `no-await-in-loop`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | **363**       |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow `await` inside of loops

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
