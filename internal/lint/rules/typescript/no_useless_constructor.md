# `@typescript-eslint/no-useless-constructor`

| | |
|---|---|
| **Recommendation** | **Maybe** |
| Violations in ahra | **1** |
| Plugin | `@typescript-eslint` |
| Auto-fixable | no |
| Needs type information | no |

## What it checks

Disallow unnecessary constructors

## Status: declined as a port, because it would register a second NAME rather than a second check

There is no `.go` beside this file, and that is the finding rather than an omission.

This rule is `getESLintCoreRule('no-useless-constructor')` plus two filters: an accessibility test
and a parameter test. `core/no_useless_constructor.go` already carries both, and its doc comment says
so with a measurement taken when it was ported.

**That claim was re-measured independently for this batch rather than inherited.** Upstream's
extension and upstream's core were driven over identical inputs on the installed 8.67.0 / 10.8.1
builds, across the extension's own 41-case corpus:

    total 41   identical 41   divergent 0   config problems 0

Zero divergence, so the extension narrows nothing the core does not already narrow here. Our core
rule reproduces that corpus exactly, and the whole core package is green.

The remaining work a port would represent is the NAME, and nothing asks for it: the config enables
`no-useless-constructor` at `CohereSettings.json:520` and never mentions the namespaced spelling. A
wrapper would also need this rule's 427-line body lifted out of the rule package, because a rule
package may not import another rule package, for no behavioural change.

## If this is revisited

Re-run the differential before assuming the zero still holds; it is a fact about two installed
versions, not a property of the rules. The harness used is an ESLint `Linter` driven with the
typescript-eslint parser and a project tsconfig, refusing any message with no `messageId` so a
configuration complaint cannot be counted as a finding.
