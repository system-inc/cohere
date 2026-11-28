# `react/hook-use-state`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Strong No**         |
| Violations in ahra     | **35**                |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Ensure destructuring and symmetric naming of useState hook value and setter variables

## Why this recommendation

It requires a destructured value+setter pair, which fights react-hook-no-destructuring and flags deliberate single-element reads like `const [animateInitial] = React.useState(...)` in TaskDetailPanel.tsx and useKingdomLive.tsx.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **No** to **Strong No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
