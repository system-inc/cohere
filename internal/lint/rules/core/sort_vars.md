# `sort-vars`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **No**        |
| Violations in ahra     | **2**         |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Require variables within the same declaration block to be sorted

## Why this recommendation

Overlaps a rule we already enforce in-house, so it would report the same defect under a second name.
