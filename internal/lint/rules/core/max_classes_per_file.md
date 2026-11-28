# `max-classes-per-file`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Maybe**     |
| Violations in ahra     | **18**        |
| Plugin                 | `eslint core` |
| Auto-fixable           | no            |
| Needs type information | no            |

## Where cohere differs from ESLint

A class declared inside a function, a method's body included, does not count. ESLint counts every class in the file. Kirk ruled it on #mbbg6js: one class per file names the design unit a reader greps for, and a class declared inside a test's `describe` or `it` is a fixture, not a unit. A class in a namespace or a static block still counts. Class expressions are left out through upstream's own `ignoreExpressions`, which ahra and the ESLint configuration both set. ESLint runs the same ruling through `nexus/max-classes-per-file` (Nexus `MaxClassesPerFileRule.ts`), the core rule with a nested declaration left out of its count, and the linter parity check pairs it with this rule, so the two linters agree (#cn8sthd). Its fixture shares this rule's rows verbatim.

## What it checks

Enforce a maximum number of classes per file

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
