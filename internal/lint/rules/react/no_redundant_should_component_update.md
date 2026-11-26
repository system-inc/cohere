# `react/no-redundant-should-component-update`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | none                  |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow usage of shouldComponentUpdate when extending React.PureComponent

## Why this recommendation

Stylistic, but the tree already satisfies it, so enabling costs nothing and prevents drift.
