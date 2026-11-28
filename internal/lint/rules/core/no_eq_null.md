# `no-eq-null`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Maybe**     |
| Violations in ahra     | **57**        |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## What it checks

Disallow `null` comparisons without type-checking operators

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
