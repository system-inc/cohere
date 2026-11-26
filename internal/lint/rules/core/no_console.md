# `no-console`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Strong No** |
| Violations in ahra     | **9565**      |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow the use of `console`

## Why this recommendation

Formatting, which Prettier already decides. Turning it on creates a second authority that can disagree.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
