# `prefer-exponentiation-operator`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Maybe**     |
| Violations in ahra     | **29**        |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Disallow the use of `Math.pow` in favor of the `**` operator

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.
