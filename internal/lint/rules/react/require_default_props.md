# `react/require-default-props`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Strong No**         |
| Violations in ahra     | **337**               |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Enforce a defaultProps definition for every prop that is not a required prop

## Why this recommendation

defaultProps is deprecated and removed for function components in React 19, so this rule pushes the codebase toward a dead API rather than merely duplicating in-house coverage.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **No** to **Strong No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
