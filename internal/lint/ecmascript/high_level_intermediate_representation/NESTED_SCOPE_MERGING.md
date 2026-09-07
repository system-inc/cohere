# Nested scope merging

React Compiler 1.0.0's tree-level merge has two operations. Adjacent scopes can
be absorbed into a survivor and recorded in its merged-scope set. Separately,
`Transform.transformScope` replaces a nested scope with its body when its
dependency set equals the enclosing live scope's set. The latter operation was
missing entirely; recursively merging adjacent children is not equivalent.

Nested replacement preserves instructions and does not add the removed scope
to the parent's merged set. Pruned scopes do not establish a new enclosing
dependency set and are not themselves removed by this operation. Conditional
and other terminal bodies retain the enclosing context. A separate
`NestedScopesRemoved` counter keeps this operation distinguishable from
adjacent merges and their declaration-pruning counts.

The production pruning chain also now runs in reference order: non-escaping,
non-reactive dependencies, unused scopes, merge, always-invalidating. Pruning
dependencies after merging misses equal sets created by that pruning.

## A diagnostic the old pipeline missed

```tsx
import {useCallback} from 'react';
function Component() {
  const theme = mergeTheme();
  const enabled = theme.enabled === true;
  const callback = useCallback(value => [value, enabled], [enabled]);
  mutate(theme.colors);
  return <div onClick={callback}/>;
}
```

Compiler 1.0.0 reports one preservation CompileError: the nested callback scope
is removed after its non-reactive dependency is pruned. The previous cohere
pipeline incorrectly reports nothing. Passing `props.theme` to mergeTheme
instead makes the dependency reactive and both compilers accept the callback.
Removing the dependency in that reactive counterpart makes both report.

All three exact sources were run through the reference harness, with its
independent missing-dependency control still active. The permanent fixture is
red on the original implementation. Restoring only the old merge or only the old
pass order also makes the constant-input case fail; neither repair alone is
sufficient. The two controls stay green in all three counterfactual runs.

Eight structural cases separately cover empty and named dependency sets,
different inputs and optionality, pruned scopes, conditional bodies and a
pruned wrapper. They assert instruction conservation and distinguish nested
replacement from recording an adjacent merge.

## Why this precedes primitive inference

A private arithmetic-type experiment clears the two remaining confirmed chart
false positives, but originally also silenced a reference-positive control.
Tracing that control found this missing operation. With nested replacement and
the correct pass order, the control reports again while the reactive chart
variant remains clean. The arithmetic experiment is still private; this repair
does not depend on it and adds a reproduction that uses an already-primitive
strict comparison.

The complete lint suite and build pass without repinning any corpus count.
The existing preservation, dependency, scope and hoistable expectations remain
unchanged. The exact-source residue probe still reports the two chart findings;
the population must be remeasured from this commit rather than inferred from
those selected sources. This repair restores a missing diagnostic rather than
weakening validation to reduce a count.
