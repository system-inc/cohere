# `react/no-unstable-nested-components`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Yes**               |
| Violations in ahra     | **18**                |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow creating unstable components inside components

## Why this recommendation

Components defined during render remount their whole subtree and lose state every render, which is a real bug; the 18 hits include concrete cases like Sidebar.tsx:294 and ArtCard.tsx:82.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Maybe** to **Yes**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
