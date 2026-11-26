# `react/jsx-curly-brace-presence`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Maybe**             |
| Violations in ahra     | **11**                |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | yes                   |
| Needs type information | no                    |

## What it checks

Disallow unnecessary JSX expressions when literals alone are sufficient or enforce JSX expressions on literals in JSX children or attributes

## Why this recommendation

Prettier does not add or strip JSX curly braces around string literals, so there is no competing authority, and at only 11 auto-fixable sites it is a cheap consistency win.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Strong No** to **Maybe**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
