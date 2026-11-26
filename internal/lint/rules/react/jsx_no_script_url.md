# `react/jsx-no-script-url`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow usage of `javascript:` URLs

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
