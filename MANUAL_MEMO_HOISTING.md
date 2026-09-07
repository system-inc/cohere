# Manual memo dependencies as hoisting evidence

React compiler 1.0.0's collectNonNullsInBlocks has a StartMemoize arm guarded by
enablePreserveExistingMemoizationGuarantees. For each immutable, non-global root,
it records the prefixes before each non-optional property access, stopping at the
first optional access. Cohere already defaults this option on for marker freezes
but omitted its hoisting arm entirely.

The repair uses that same option predicate and the existing immutability test.
It does not revive dead dependency-array instructions or treat an optional access
as proof of non-nullness. The validator and dependency comparator are unchanged.

## Fixtures and reference

A hook with state conditionally returns a row from options.rows inside useMemo,
with [options.rows, selected] as dependencies. Cohere inferred the shallower
options and reported a mismatch. The exact reduced source gets CompileSuccess
from React compiler 1.0.0; the optional [options?.rows, selected] variant gets a
preservation CompileError. Controls prove the compiler harness can report first.

The new table pairs those sources with an unconditional read and a genuine
unpreserved method receiver. Before the repair only the non-optional conditional
case fails. All four pass afterward.

An existing dominating-scope test asserted the wrong answer under the default
configuration. React accepts that exact source with preservation enabled and
reports with it disabled. The test now runs both configurations: two default
cases must be silent; the disabled cases retain the prior expectations, including
the two harness findings from the dominating-scope case. Only that default case
fails before the repair. The disabled control remains green throughout.

This fixes useAssetPreview in the private exact-source probe. It does not establish
that all conditional-access discrepancies are repaired; the disabled version of
the minimized preview hook remains a separate precision discrepancy.

## Golden attribution

The matcher, its inputs and dependency multiplicities are unchanged. Four fixture
rows become more precise, with the raw total holding at 125:

| Fixture | Changed dependency | Replacement |
| --- | --- | --- |
| optional-member-expression-inverted-optionals-parallel-paths | props.a | props.a.b.c.d.e |
| propagate-scope-deps-hir-fork copy of that fixture | props.a | props.a.b.c.d.e |
| preserve-memo-deps-conditional-property-chain-less-precise-deps | x | x.y |
| preserve-memo-deps-conditional-property-chain | x | x.y.z |

The optional fixtures already have another matching full-depth dependency, and
the precise-chain fixture already has another x.y.z. Only the less-precise-chain
fixture gains a new golden match: 82/88 to 83/88. No matcher change accounts for
the gain. Corpus hoisting counts, scope counts, alignment, merge bounds and
conservation do not change.

The `dc82ac8` archive binary measures 44 findings on the frozen snapshot: one
removed (useAssetPreview), none added and 44 unchanged relative to `f693b4f`.
The remaining population has 28 covering CompileSuccess occurrences and 16
bailout-covered occurrences (15 Todo, one Suppression). Zero false positives is
not yet achieved.
