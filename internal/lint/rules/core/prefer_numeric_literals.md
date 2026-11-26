# `prefer-numeric-literals`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Yes**       |
| Violations in ahra     | none          |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Disallow `parseInt()` and `Number.parseInt()` in favor of binary, octal, and hexadecimal literals

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
