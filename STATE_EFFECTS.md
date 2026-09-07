# State effects and manual memoization

React's compiler 1.0.0 gives `useState` a Frozen result and freezes its arguments.
Cohere previously used the unknown-call Mutable result. A method read such as
`newName.trim()` could consequently extend a state value's mutable interval over
unrelated declarations, including another hook's stable setter. Scope-output
reactivity then turned that setter into a dependency of a memoized callback.

The paired SeeClusterCard reproduction is in `effects_react_state_test.go`:
`newName.trim().length` previously reports, while `newName.length` does not.
Both exact sources compile successfully with preservation validation enabled in
React compiler 1.0.0. A bad-dependency control produces a preservation CompileError.
The isolated Frozen-result intervention narrows the state interval from `[21,45)`
to `[21,22)`; no setter or method-call exemption is added to the validator.

## Origin, not spelling

Lowering records a checker-resolved module/export origin on calls. Effect inference
uses React's state signature only for `react/useState`. The graph retains strings,
not a checker or symbols. Recognition covers named, namespace, default and renamed
imports, static computed namespace access, constant aliases, named re-exports and
star re-exports. Star resolution checks exported-symbol identity so an explicit
local override does not inherit React's signature.

Fixtures reject local and namespace shadows, unrelated packages, overridden star
exports, mutable aliases, same-typed impostors and unresolved names. Dynamic access,
destructured aliases and other unrecognized forms conservatively retain the prior
unknown-call behavior; this is not a complete module-provenance analysis.

The effect fixtures, paired method/property reproduction and positive mismatch
control are accompanied by a raw-versus-pruned setter dependency fixture. Replacing
only `effects.go` with the previous version via a private Go overlay makes the ten
genuine-origin cases, both method variants and the new setter-separation assertion
fail; the seven rejected origins, property-read control and real mismatch still pass.

## Attributed corpus changes

The for-of header classification prerequisite is already present. The same sampled
corpus and pipeline were compared with and without the state effect, using private
Go overlays. Temporary source-directory prefixes were removed before comparing
function keys. Counts below include repeated dependencies in distinct scopes.

| Instrument | Before | After | Attribution |
| --- | ---: | ---: | --- |
| Golden raw dependencies | 123 | 124 | One stable `setState` input in `allow-global-mutation-in-effect-indirect-usecallback` |
| Matched golden dependencies | 81/88 | 81/88 | No changed matches |
| Functions requiring method alignment | 60/680 | 61/680 | `Menu`, `findIndex` at evaluation order 163 |
| Function-operand skip upper bound | 3 | 7 | `InputMultipleSelect` +1, `InputSelect` +1, `InputTimeRange` +2 |
| Hoistable deep dependencies | 625 | 631 | 9 removed, 15 added |
| Hoistable flat dependencies | 2841 | 3043 | 4 removed, 206 added |

The golden collector runs before non-reactive pruning, while the reference cache
slots describe final output. The added `setState` dependency is removed by that
pruning: the final named dependency remains `setGlobal` in both versions. The new
fixture pins both the exposed raw input and its removal, rather than interpreting
the raw +1 as an additional cache comparison. Existing golden inaccuracies are not
declared resolved by this result.

`Menu` previously has both the `findIndex` result and property in scope 1. With
Frozen state the primitive result is unscoped while the property remains in scope 1;
the existing alignment pass removes the property's scope. No other function enters
or leaves this instrument's changed set.

The merge probe deliberately skips all function-expression operands, an upper bound
on the missing primitive-only gate rather than React's real predicate. The added
unions occur on captures named `value`: `InputMultipleSelect` at order 122 adds one;
`InputSelect` at 233 adds one while its existing union at 243 remains; and
`InputTimeRange` at 76 adds two. Narrower state scopes expose those overlaps. Total
unions move from 134/131 to 136/129 without/with the probe gate; the bound is now seven,
not a claim that seven real primitive captures require different handling.

### Hoistable dependency multiset

All 13 removed and 221 added rows are accounted for by these 17 functions; every
other function's dependency multiset is unchanged.

| Function | Removed | Added |
| --- | ---: | ---: |
| useGraphQlInfiniteScroll | 1 | 18 |
| CopyButton | 4 | 16 |
| TimeSeriesContainer | 2 | 2 |
| useTimeSeriesState | 5 | 26 |
| DialogMenu | 1 | 3 |
| useEventSourceSnapshot | 0 | 2 |
| WebSocketViaSharedWorkerProviderInternal | 0 | 17 |
| AppearanceProvider | 0 | 24 |
| Confetti | 0 | 1 |
| LineLoadingAnimation | 0 | 3 |
| LoadingAnimation | 0 | 9 |
| AnimatedButton | 0 | 67 |
| useTimeSeriesDataWithCache | 0 | 3 |
| JsonNode | 0 | 13 |
| ColorPicker | 0 | 6 |
| useColorPickerState | 0 | 10 |
| ColorPickerAxisSelector | 0 | 1 |

These are raw scope inputs, not findings or final cache slots. For example,
`AnimatedButton`'s outer scope formerly spans `[1,492)` and names only the properties
root. Its parameter scope now ends at 4, the following large scope begins at 5, and
independent state/effect scopes expose inputs that previously were internal members.
The dependency table grows from 8 to 24 entries, including repeated state values,
setters and separate `buttonProperties` paths. Inserted scope terminals renumber
later evaluation orders, so a comparison of endpoints alone would be misleading.

`useTimeSeriesState` similarly separates a `[1,219)` scope into smaller scopes;
the first ends at 22 and retains only `defaultTimeRange` from its previous six paths.
`TimeSeriesContainer`'s initial scope ends at 7 rather than 38: its two properties
paths cease to be inputs of that state scope, while the now-separate effect scope
exposes its setter and callee. `CopyButton` exposes its state and callback scopes
and their previously internal inputs. The collector still converges in at most two
iterations. The count pins are recalibrated to these attributed inputs, not relaxed.

## Population gate

Source archives of `3a0e70f` and repair `b9f24f3`, built with the same pinned TypeScript
checkout, expose identical 450-rule inventories. On the frozen snapshot the baseline
reproduces the preceding 217 finding multiset exactly. The repaired binary reports
177: **58 removed, 18 added, 159 unchanged**. This is not a fresh live-tree ESLint run.

Direct React compiler 1.0.0 runs on all 58 remaining flagged files, with preservation
validation enabled and the bad-dependency controls run first, give covering
CompileSuccess events for 130 occurrences. The other 47 are covered by different
bailouts: 33 Todo, 7 Hooks and 7 Suppression. None has a preservation CompileError;
none throws out of the harness. Bailouts are not evidence that those 47 findings
are correct or incorrect.

Five of the 18 additions fall in successfully compiled functions: one in
`SidebarNewProjectDialog`, two in `NotificationsContainer` and two in
`useVariableVirtualScroll`. They are unresolved false positives, not an improvement
to claim as stricter checking. The other 13 additions have bailout-covered verdicts.
The state repair fixes its demonstrated mechanism but does not close parity, and
the net reduction does not excuse these additions.

All 28 scoreable preservation-error goldens still report and all 69 clean goldens
remain silent with zero unsupported. The full lint suite, build and scope oracle
pass; scope under-production remains zero. Those controls bound the repair without
substituting for the population differential. Subsequent comparisons must retain
finding multiplicity and disable ESLint's cache for live source changes.
