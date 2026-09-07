# Immutable alias edges

## Mechanism and regression fixtures

React compiler 1.0.0 refines CreateFrom on a Frozen or Primitive source into Create
(and ImmutableCapture for Frozen, a range no-op). Its Assign refinement likewise
does not emit a mutable alias edge for either kind. Cohere propagated the kind but
retained the edge. A later mutation reached backwards through that stale edge,
including through a join combining an immutable value with a mutable fallback.

The repair omits those two edges at graph construction. Mutable sources still
create and traverse their original edges. This does not weaken the mutation walk,
change hook recognition, or exempt findings in the validator.

The source reproduction reads a custom hook's data inside useMemo, optionally
falls back to an empty array, iterates it to build a new array and supplies the
original data as the dependency. React compiler 1.0.0 emits CompileSuccess with
preservation validation enabled. The deliberately bad dependency controls run
first and produce a preservation CompileError.

The remaining newly introduced state-freezing false positive in
SidebarNewProjectDialog is cleared by the private intervention on its exact
snapshot source. NotificationsContainer and useVariableVirtualScroll remain clean.

Permanent fixtures cover:
- Frozen and Primitive versus Mutable for each of CreateFrom and Assign.
- Property reads and local aliases flowing through a mutable fallback, with a
  Frozen component parameter versus an ordinary Mutable parameter.
- Memoized iteration both with and without the fallback, a silent no-iteration
  control, and an actual unpreserved dependency.

Against the original implementation, four immutable edge cases, two Frozen
source cases and both iteration cases fail. Mutable-source and no-iteration
controls remain green, as does the case that must report. All pass with the repair.
The mutable fallback itself still widens in both source cases.

## Corpus attribution

Private Go overlays compare identical inputs and preserve dependency multiplicity.
Only the range implementation differs between the two sides.

| Instrument | Before | After |
| --- | ---: | ---: |
| Deep inputs | 649 | 685 |
| Flat inputs | 3118 | 3227 |
| Merge unions, ordinary/skip-function-operands | 135/127 | 139/138 |
| Function-operand skip upper bound | 8 | 1 |
| Functions needing method alignment | 109/680 | 113/680 |
| Composite-value instruction loss cases | 5 | 4 |

Shorter ranges split previously conflated scopes. Their newly external inputs
change the pre-pruning distribution; these are not final memo dependencies or
diagnostic counts. The unchanged golden dependency and preservation tests are
independent gates, not inferred from these totals.

### Direct operand-gate fixture

Only identifiers 37 and 38 leave the partition: their ranges shrink from [6,24)
and [7,24) to [6,7) and [7,8). These are the settled primitive initializer copies.
The partition has 12 members in three classes instead of 14 in three. A fresh
Contains-free mutation still fails, now with 22 members in seven classes.
The test's controls are retained and its calibration follows that measured shape.

### Alignment and conservation

The four additional alignment functions are reconcileDirectConnection and
reconcileWorkerSubscription in useEventSourceStream.tsx, formatDateByTimeInterval
in TimeSeriesFormatters.tsx, and getTextFromChildren in Markdown.tsx.

findBottomBarDataKey in TimeSeriesBar.tsx previously emitted 31 of 32 instructions;
it now conserves all 32. contentNavigatorItemsEqual in ContentNavigator.tsx improves
from 49 of 52 to 50 of 52 but remains a declared composite-value loss. No other
loss row changes. Unattributed loss, unmatched gotos and non-implicit scope breaks
remain zero; the four pre-existing double emissions remain four.

### Complete merge delta

Values are ordinary/skip-function-operands union counts. All other function rows
are unchanged. The skip removes all function-expression operands, so its remaining
one-union divergence in AnimatedButton is an upper bound, not a measured type-aware
reference difference.

| File and function | Before | After |
| --- | ---: | ---: |
| ChoiceDistributionDialogContent.tsx ChoiceDistributionDialogContent | 2/2 | 1/1 |
| ColorPaletteSwatches.tsx ColorPaletteSwatches | 1/1 | 0/0 |
| ColorPickerAxisSlider.tsx renderSliderCanvas | 3/3 | 4/4 |
| Confetti.tsx | 1/1 | 0/0 |
| DialogRoot.tsx DialogRoot | 1/1 | 0/0 |
| FileCarousel.tsx FileCarousel | 1/0 | 0/0 |
| InputMultipleSelect.tsx InputMultipleSelect | 1/0 | 0/0 |
| InputSelect.tsx InputSelect | 2/0 | 0/0 |
| InputText.tsx InputText | 1/0 | 0/0 |
| InputTimeRange.tsx InputTimeRange | 11/9 | 7/7 |
| MapLandMask.ts tagDotsWithCountryIndices | 0/0 | 1/1 |
| Markdown.tsx getTextFromChildren | 0/0 | 2/2 |
| Menu.tsx Menu | 2/2 | 0/0 |
| ResizableSidePanel.tsx ResizableSidePanel | 1/1 | 0/0 |
| TableCellChoiceColors.ts hashStringToPaletteIndex | 0/0 | 1/1 |
| TableColumnLayout.ts resolveColumnBasePixelWidth | 0/0 | 1/1 |
| TableColumnLayout.ts resolveColumnPlaceholderPixelWidth | 0/0 | 1/1 |
| TimeSeriesBar.tsx TimeSeriesBar | 1/1 | 0/0 |
| TimeSeriesFormatters.tsx formatDateByTimeInterval | 1/1 | 9/9 |
| TimeSeriesFormatters.tsx formatTipLabelByTimeInterval | 4/4 | 10/10 |

### Complete raw dependency delta

Removed: four deep and 13 flat. Added: 40 deep and 122 flat. All remaining input
multisets are unchanged. Counts below are deep/flat; the blank Confetti function
name identifies its anonymous function.

| File and function | Removed | Added |
| --- | ---: | ---: |
| AppearanceProvider.tsx setAppearanceClassOnDom | 0/0 | 1/0 |
| Badge.tsx Badge | 0/0 | 0/5 |
| ColorPaletteSwatches.tsx ColorPaletteSwatches | 1/0 | 3/13 |
| ColorPickerSwatches.tsx ColorPickerSwatches | 0/0 | 1/2 |
| Confetti.tsx | 0/0 | 1/12 |
| Confetti.tsx options | 0/0 | 0/5 |
| Confetti.tsx update | 0/0 | 4/0 |
| DialogRoot.tsx DialogRoot | 1/1 | 8/6 |
| EventSourceSharedWorkerServer.ts broadcastFrame | 0/2 | 0/0 |
| EventSourceSharedWorkerServer.ts onClientConnectionMessage | 0/0 | 0/1 |
| EventSourceStream.ts register | 0/0 | 1/0 |
| GraphQlMutationForm.tsx GraphQlMutationForm | 0/0 | 0/2 |
| TimeSeriesBar.tsx TimeSeriesBar | 2/7 | 2/6 |
| TimeSeriesBar.tsx findBottomBarDataKey | 0/0 | 0/1 |
| TimeSeriesChart.tsx TimeSeriesChart | 0/0 | 11/10 |
| TimeSeriesExport.ts convertTimeSeriesDataToCsv | 0/0 | 0/1 |
| TimeSeriesLegend.tsx TimeSeriesLegend | 0/0 | 3/0 |
| ToggleButton.tsx ToggleButton | 0/0 | 0/10 |
| ToggleGroupButton.tsx ToggleGroupButton | 0/0 | 0/10 |
| WebSocketConnection.ts connect | 0/0 | 0/4 |
| WebSocketConnection.ts disconnect | 0/0 | 0/4 |
| WebSocketConnection.ts handleClose | 0/0 | 1/0 |
| WebSocketConnection.ts handleMessage | 0/0 | 3/5 |
| WebSocketConnection.ts handleOpen | 0/0 | 1/0 |
| WebSocketConnection.ts reconnect | 0/0 | 0/1 |
| WebSocketConnection.ts send | 0/0 | 0/4 |
| WebSocketConnection.ts sendPing | 0/0 | 0/1 |
| useColorPickerState.ts useColorPickerState | 0/0 | 0/1 |
| useEventSourceStream.tsx reconcileDirectConnection | 0/0 | 0/1 |
| useEventSourceStream.tsx reconcileWorkerSubscription | 0/0 | 0/1 |
| useGraphQlInfiniteScroll.ts useGraphQlInfiniteScroll | 0/3 | 0/14 |
| useTimeSeriesState.tsx useTimeSeriesState | 0/0 | 0/2 |

## Committed population

The `f693b4f` archive binary measures 45 findings on the same frozen snapshot,
against 162 at `a9b43dd`: 121 removed, four added and 41 unchanged. Its SHA256 is
`4d49218e9563ebdeeda6d5d9c759087c9895abad8a1326dadec6cc692bb7d57e`.

There are 29 covering CompileSuccess occurrences, hence confirmed false positives,
and 16 bailout-covered occurrences (15 Todo, one Suppression). Three of the four
additions are confirmed false positives: FinancePeopleView at 79:27 and two
TableRoot occurrences sharing 342:11. The InputMultipleSelect addition at 124:18
is Todo-covered and remains unadjudicated. A net reduction does not excuse the
three regressions, and the parent task remains open.
