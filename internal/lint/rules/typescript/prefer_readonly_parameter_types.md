# `@typescript-eslint/prefer-readonly-parameter-types`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Strong No**        |
| Violations in ahra     | **12744**            |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Require function parameters to be typed as `readonly` to prevent accidental mutation of inputs

## Why this recommendation

Stylistic and enormous: the volume says the codebase has a different convention, not that it is wrong.
