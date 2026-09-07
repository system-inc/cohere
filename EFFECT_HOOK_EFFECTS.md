# Effect-hook signatures and preservation

After the state repair, four newly introduced false positives occur in
`NotificationsContainer` and `useVariableVirtualScroll`. A private intervention
restricted to React's `useEffect`, `useLayoutEffect` and `useInsertionEffect`
clears those four. Extending the same intervention to custom hooks is unnecessary
for these cases; the fifth addition, `SidebarNewProjectDialog`, remains unexplained.

## Reproduction and repair

`TestEffectHookDoesNotPromoteStableSetter` reduces the notifications case to a
component that obtains a service, initializes state, memoizes a derived array,
passes a callback and dependencies to an effect, then memoizes a callback using
the stable setter. The unknown-call effect previously lets that effect invocation
widen captured values' scopes and promote the setter to a reactive dependency.

Direct React compiler 1.0.0 successfully compiles the exact minimized source with
each of the three effect hooks. Preservation validation is enabled and the
bad-dependency controls run first. The implementation freezes effect arguments
instead of treating their callback and dependency array as mutable call inputs.
No validator exemption is added.

Recognition uses the import-origin metadata introduced for state, not callee
spelling. Fixtures cover named, renamed, namespace, default and barrel imports,
with local shadows and unrelated modules as controls. The minimized table includes
the no-effect variant and an actual dependency mismatch. These 26 cases pass with
the repair; the previous implementation fails all 15 genuine-origin signature
cases and all three effect-bearing reproductions while the controls remain green.

React's bundled `REACT_APIS` table gives layout and insertion effects Frozen
results. `useEffect` has an aliasing signature that takes precedence over its
legacy fields: it freezes its inputs, stores them in a private Frozen effect
object and creates a Primitive return. Cohere models the observable argument and
return effects; it does not create React's private effect-object temporary. The
Primitive return is load-bearing: using the legacy Frozen return instead leaves
unnecessary allocation scopes and a different corpus distribution.

## Corpus attribution

The same corpus and harnesses were compared using private Go overlays. Temporary
directory prefixes were removed from function keys; dependency multiplicity was
preserved. The changes are in the inputs to existing passes, not edits to those
passes.

| Instrument | Before | After | Attribution |
| --- | ---: | ---: | --- |
| Matched golden dependencies | 81/88 | 82/88 | `ref-like-name-in-effect` gains exactly the golden `ref` dependency |
| Raw golden dependencies | 124 | 125 | The same single row; no other fixture's dependency list changes |
| Functions requiring method alignment | 61/680 | 109/680 | 48 added, none removed; 79 `useEffect` property/result mismatches in those functions |
| Function-operand skip upper bound | 7 | 8 | One additional callback-operand union in `AnimatedButton` |
| Surviving scopes in the guard sample | 133 | 140 | Four added in `allow-global-mutation-in-effect-indirect-usecallback`, three in `ref-like-name-in-effect` |
| Deep scope inputs | 631 | 649 | 3 removed, 21 added |
| Flat scope inputs | 3043 | 3118 | 15 removed, 90 added |

`useEffect` now has an unscoped Primitive result while its property load can still
carry a scope. The existing method-alignment pass removes that property's scope.
Every mismatch in the 48 newly changed functions names `useEffect`.

The merge probe skips every function-expression operand, not just primitive
operands, so eight remains an upper bound rather than an asserted reference gap.
Its total unions move from 136/129 to 135/127 without/with the probe. Besides the
extra `AnimatedButton` union, `ColorPickerAxisPanel` and `ColorPickerAxisSlider`
each lose one union under both modes.
The added callback capture is `processingAnimationTimeoutReference` at order 191,
with range `[1,452)` and active scopes `[1,9]`; it adds exactly one union.

### The scope oracle's limitation

Its regex counts only `!==` guards and misses constant-cache sentinel guards.
The two changed goldens each have one changing-input guard, but the full generated
code has three cache guards in `allow-global-mutation-in-effect-indirect-usecallback`
and four across the functions in `ref-like-name-in-effect`. Both explicitly cache
effect callbacks and dependency arrays. With proper freezing, those values gain
independent scopes here too. Surviving counts change 2 to 6 and 2 to 5, respectively.

This does not establish exact scope parity: some callback/array scopes remain
separate where React merges them, and the total-count oracle is not an output-level
proof. The bound is updated only for these seven attributed scopes. Its sample,
changing-input guard count, under-production checks and exact-fixture floor remain
unchanged. The dependency oracle independently recovers the reference's `ref`
input, and preservation's positive and negative goldens are still required to pass.

### Complete raw dependency delta

All 18 removals and 111 additions occur in these 17 functions; other dependency
multisets are unchanged. These are pre-pruning scope inputs, not final comparisons
or diagnostic counts. Separating callback and array allocations exposes inputs
that were previously internal to a larger scope.

| Function | Removed | Added |
| --- | ---: | ---: |
| AnimatedButton | 3 | 8 |
| AppearanceProvider | 3 | 3 |
| Code | 2 | 3 |
| ColorPicker | 5 | 10 |
| Confetti | 1 | 18 |
| DialogMenu | 1 | 1 |
| WebSocketViaSharedWorkerProviderInternal | 2 | 5 |
| useEventSourceStream | 1 | 5 |
| ColorPickerAxisPanel | 0 | 7 |
| ColorPickerAxisSlider | 0 | 9 |
| ColorPickerWheelPanel | 0 | 1 |
| FadeSweepElement | 0 | 9 |
| LineLoadingAnimation | 0 | 10 |
| LoadingAnimation | 0 | 8 |
| useColorPickerInteraction | 0 | 3 |
| useEventSourceSnapshot | 0 | 4 |
| useTimeSeriesDataWithCache | 0 | 7 |

The private experiment is not a population measurement. The preceding committed
population is 177; compare a committed repair binary on the same frozen snapshot
only after the fixtures, structural suite and build pass. Retain added, removed
and unchanged findings, and adjudicate additions rather than crediting a net drop.
