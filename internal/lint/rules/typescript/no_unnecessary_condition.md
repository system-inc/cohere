# `@typescript-eslint/no-unnecessary-condition`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Maybe**            |
| Violations in ahra     | **1243**             |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Disallow conditionals where the type is always truthy or always falsy

## Why this recommendation

Catches a defect rather than a preference, but the volume means it needs a plan rather than a switch.
