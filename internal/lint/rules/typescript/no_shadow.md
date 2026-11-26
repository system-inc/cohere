# `@typescript-eslint/no-shadow`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **No**               |
| Violations in ahra     | **174**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | no                   |

## What it checks

Disallow variable declarations from shadowing variables declared in the outer scope

## Why this recommendation

Every sampled violation is the deliberate `React.useCallback(function handleX() {...})` named-function-expression idiom used 44 times for DevTools stack names, where the inner name is the same binding, not a shadow.

> Reviewed against real violation sites and the house conventions in `CLAUDE.md`,
> which moved this from **Yes** to **No**. The first pass classified rules by
> correctness-versus-style crossed with violation count, and that is blind to
> whether a rule fights a convention we chose on purpose.
