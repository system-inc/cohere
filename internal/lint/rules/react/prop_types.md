# `react/prop-types`

|                        |                       |
| ---------------------- | --------------------- |
| **Recommendation**     | **Strong No**         |
| Violations in ahra     | **5**                 |
| Plugin                 | `eslint-plugin-react` |
| Auto-fixable           | no                    |
| Needs type information | no                    |

## What it checks

Disallow missing props validation in a React component definition

## Why this recommendation

It is not an overlap but a category error in a TypeScript codebase: the 5 hits ask for runtime propTypes on components whose props are already typed, such as AhraIcon.tsx's `width`/`height`.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **No** to **Strong No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
