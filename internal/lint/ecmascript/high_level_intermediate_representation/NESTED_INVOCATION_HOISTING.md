# Nested invocation and hoistable reads

`CollectHoistablePropertyLoads` already gates callback-derived non-null facts on
`CollectAssumedInvokedFunctions`. The dependency collector's additional
`nestedHoistable` path bypassed that gate: it treated an unconditional read inside
any closure as proof that reading it at closure creation was safe. The collector
now uses the same invocation set. Dependencies themselves are still collected
from every closure; only the extra non-null evidence is gated.

## Reproduction

```tsx
import React from 'react';
function Component(properties) {
  const callback = React.useCallback(
    () => properties.inputReference.current.value,
    [properties.inputReference],
  );
  React.useEffect(() => callback(), [callback]);
  return null;
}
```

React Compiler 1.0.0 compiles this successfully. The permanent fixture reports
one preservation error before the repair and none after. Returning the callback
as a JSX attribute instead produces a reference preservation CompileError and
still reports here: JSX arguments are assumed callable, permitting a deeper
inferred path through `current` that disagrees with the written dependency.
The ordinary-property twin remains clean, as does passing the callback to an
unknown consumer; a separate missing-reactive-dependency control still reports.

The reference's invocation collector considers CallExpression hook arguments,
not MethodCall arguments. This repair neither adds a namespace exception nor
changes that collector: it removes an independent route around its existing
answer. An early attempted reduction using a JSX return was not a false positive
at all; compiling both exact variants was necessary to retain the trigger.

On the full FormUncontrolledInputSynchronizer source, the reference already has
`properties.inputReference` and `properties.fieldContext` as the memo scope's
dependencies at BuildReactiveFunction. The problem therefore precedes pruning
and merging. Reordering those passes in a private control moved none of the four
confirmed false positives; gating the extra hoistable facts clears this one.

## Corpus attribution

Only the hoistable distribution pin changes: 791 deep / 3371 flat becomes
777 deep / 3380 flat, with maximum iterations still two. The complete multiset
delta replaces these fourteen deep accesses with nine root accesses:

| Function | Removed paths | Added roots |
| --- | --- | --- |
| useEventSourceStream | channelsKey.length | channelsKey |
| AnimatedText | tokens.length | tokens |
| TimeSeriesTip | appearance.appearanceClassName | appearance |
| ChartTypeInputSelect | translations.ChartType | translations |
| useZoomBehavior | options.currentTimeInterval; options.currentTimeRange | options |
| useZoomControls | options.currentTimeRange.startTime x2; options.currentTimeRange.endTime x2; options.currentTimeInterval x2 | options x2 |
| useColorPickerState | colorState.rgb x2 | colorState x2 |

The golden preservation scores and scope/dependency oracles otherwise retain
their existing expectations. This corpus movement is loss of unsupported
non-null assumptions, not permission to suppress validation on callbacks.
