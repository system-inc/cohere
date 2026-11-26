# `no-implicit-coercion`

|                        |               |
| ---------------------- | ------------- |
| **Recommendation**     | **Maybe**     |
| Violations in ahra     | **52**        |
| Plugin                 | `eslint core` |
| Auto-fixable           | yes           |
| Needs type information | no            |

## What it checks

Disallow shorthand type conversions

## Where cohere is deliberately quieter than ESLint

Upstream's `isNumeric` is syntactic (a numeric literal, or a call to `Number`, `parseInt` or
`parseFloat`), so `1 * zoom` reports and recommends `Number(zoom)` even when `zoom` is already a
`number`. Nothing is coerced, and the advice converts nothing. cohere asks the checker
(`noImplicitCoercionIsAlready`) and stays silent on the four number arms (`+x`, `-(-x)`, `1 * x`,
`x - 0`) when every union constituent of the operand's type, read through a type parameter's
constraint, is number-like (an intersection counts when any part is, so branded numbers qualify). On
`-(-x)` a bigint qualifies too, since that is an identity on a bigint; `+x`, `1 * x` and `x - 0` on a
bigint are compile errors and runtime TypeErrors and keep reporting. `any`, `unknown`,
`number | undefined` and strings keep reporting.

The string and boolean arms ask the same question (#vsy2eym). `"" + s`, `s + ""` and `s += ""` are
silent when `s` is already string-like (a template literal type counts), and `!!b` is silent when `b`
is already boolean-like: `String(s)` and `Boolean(b)` convert nothing, so ESLint's advice is wrong
there. Measured on ESLint 10.8.1, which reports all five shapes. `string | undefined`,
`boolean | null`, a number, `any` and `unknown` keep reporting.

The real sites that went silent, all four of the rule's findings in ahra:
`libraries/structure/source/components/maps/Map.tsx:747`, `:765`, `:784` (`context.lineWidth = 1 * zoom`)
and `libraries/structure/source/components/maps/MapDrawing.ts:220` (`const dotRadius = 1.0 * zoom`).
4 findings before, 0 after. No case in ESLint's 141-case corpus moved.

The string and boolean arms moved one site in the four trees measured (ahra, www-phi-health,
api-phi-health cohere-zero, connected, with the rule alone): www-phi-health's
`app/ops/marketing/social-media/_components/ManageSocialMediaSettingsDialog.tsx:94`,
`!!next.responseBotEnabled` on a boolean schema field. ahra has no site of either shape, so the
differential harness, which runs on ahra, has nothing new to acknowledge.

Fixtures: `TestNoImplicitCoercionIsSilentWhenNothingIsCoerced` in `no_implicit_coercion_test.go`, twelve
silent rows (both real shapes, each arm on a number, literal unions, a branded number, a numeric enum,
`-(-big)`, a constrained type parameter, a non-null-asserted number) and twelve reporting controls,
then seven string and boolean silent rows (DialogRoot.tsx:175's boolean expression among them) and
seven reporting controls. The four sites were then written as `zoom` (Structure c62c077e), which says
the same thing and which neither linter reports (#cn8sthd).

## Why this recommendation

Stylistic. Small enough to adopt if we want the convention, not urgent if we do not.
