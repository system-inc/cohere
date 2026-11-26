# `@typescript-eslint/consistent-return`

|                        |                      |
| ---------------------- | -------------------- |
| **Recommendation**     | **Yes**              |
| Violations in ahra     | **121**              |
| Plugin                 | `@typescript-eslint` |
| Auto-fixable           | no                   |
| Needs type information | yes                  |

## What it checks

Require `return` statements to either always or never specify values

## Why this recommendation

Catches a defect rather than a preference. The cleanup is real but bounded.

## Status: ported and registered, NOT enabled

`consistent_return.go` is beside this file. `--rules` lists the name; `--rules-enabled` does not.

Enabling it is a decision rather than a wiring step, because the bare `consistent-return` is already
enabled at `CohereSettings.json:506` and the two are not interchangeable. Driving upstream's
extension and upstream's core over the extension's own 30-case corpus on the installed 8.67.0 /
10.8.1 builds:

    total 30   identical 17   divergent 13

Every one of the thirteen is the extension going SILENT where the core reports, and every one needs
the type checker. Twelve are the `void` / `Promise<void>` suppression and one is a `return` of a
value typed exactly `undefined`.

Measured on the ahra tree through the real config layer:

    consistent-return                       141 findings   (enabled today)
    @typescript-eslint/consistent-return    133 findings

Ten of the 141 are suppressed by the extension. Two findings appear under the extension that the bare
rule does not report, and that is correct upstream behaviour rather than a defect: suppressing a
function's FIRST bare return lets a later value-carrying return set the expectation instead, which
moves the finding from the return to the function head. Both verdicts were confirmed against the
installed builds and the shape is pinned by a fixture.

So adopting this means choosing which of the two rules the tree wants, and turning the other off.
Adding a key without removing the other leaves both enabled, which reports the thirteen the extension
exists to suppress.

## What fixing looks like

Not auto-fixable. Each site needs a human decision, which is what makes the count in the table above the real cost.
