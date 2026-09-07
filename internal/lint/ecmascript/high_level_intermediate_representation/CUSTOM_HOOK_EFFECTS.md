# Custom-hook effects

React Compiler 1.0.0's default custom-hook signature reads its receiver, freezes its
arguments, and returns a Frozen value. This is a hook convention, not a statement
that an import resolves to a React builtin. Named, renamed, default, namespace,
computed namespace, module-local and copied bindings now reach that signature.
Unresolved globals, parameter shadows and dynamic computed names remain unknown.
The first-line `@enableAssumeHooksFollowRulesOfReact:false` override selects the
reference's mutable custom-hook signature instead.

Global provenance was independently repaired at `4c7dade`. React builtin origin
resolution is unchanged. In particular, a function named `useState` imported from
another package is a custom hook, not React's state hook. Earlier state/effect
tests conflated “not the builtin” with “Mutable”; they now assert builtin origin
separately from the result kind. Direct reference compilation of a mutation of
a wrong-package or module-local `useState` result reports hook immutability, while
the ordinary `getData` counterpart compiles.

## Reproduction and controls

A property copied from `useData()` before `useCallback(..., [data])`, and passed
to an unknown call afterwards, previously widened the hook result across the
memo. Freezing only at StartMemoize was too late: the earlier property alias was
already mutable. Returning Frozen at the hook call prevents that alias edge.

The direct and optional-property fixtures each report two findings before the
repair and none after. React Compiler 1.0.0 compiles both successfully. Replacing
only `useData` with `getData` produces two preservation CompileErrors in the
reference and still reports here; deleting the dependency produces a reference
preservation CompileError and still reports here. Removing the later call stays
clean. The reference harness also retains its bad-dependency positive control.

Eight module-binding effect cases and both memo reproductions were red before
the repair; ordinary calls, shadows, unresolved names and the disabled option
were green controls. Namespace tests separately caught the distinction between
MethodCall's primitive property key and a PropertyLoad value.

This uses the existing legacy effect-signature application, as the builtin
state/effect signatures do. Preferred upstream rest-argument alias substitution
is not implemented here. In particular, spread arguments retain the conservative
iterator mutation effect; upstream's preferred signature can bail out on mutable
hook spreads. No validator exemption or bailout-as-success is introduced.

## Corpus attribution

The three changing pins were measured with identical sources, first with
`4c7dade` effects and then with this repair:

- Scope oracle: 48 scored, 97 upstream inequality guards; assigned 157 to 159,
  surviving 140 to 142. Neither under-production nor the 26 exact fixtures moves.
  `useCallback-extended-contextvar-scope` and
  `repro-preserve-memoization-inner-destructured-value-mistaken-as-dependency`
  each change assigned/surviving 4/4 to 5/5. Frozen hook results stop joining
  subsequent computations. This is additional scope over-production, not proof
  of exact cache-block parity; the oracle does not count constant-cache guards.
- Merge operand-skip upper bound: 139/138 to 130/126. Its gap grows from one to
  four, all in AnimatedButton. This remains an upper bound obtained by skipping
  every function-expression operand, not a claim that all four should be skipped.
- Hoistable dependencies: 685 deep / 3227 flat to 791 / 3371. Multiplicity-aware
  subtraction removes 13 deep / 13 flat and adds 119 deep / 157 flat across the
  37 functions below. Maximum iterations remains two.

All changed merge rows (ordinary / all-function-operands-skipped):

| Function | Before | After |
| --- | --- | --- |
| GraphQlMutationForm | 1/1 | 0/0 |
| AnimatedButton | 2/1 | 5/1 |
| CalendarHeader | 0/0 | 1/1 |
| DialogFooter | 1/1 | 0/0 |
| DialogHeader | 1/1 | 0/0 |
| DrawerFooter | 1/1 | 0/0 |
| Field | 0/0 | 1/1 |
| FieldMessage | 2/2 | 0/0 |
| InputTimeRange | 7/7 | 2/2 |
| Link | 1/1 | 0/0 |
| ChoiceDistributionDialogContent | 1/1 | 0/0 |
| TableRecordCount | 1/1 | 0/0 |

Complete hoistable delta, with removed/added entries shown as deep/flat:

| File and function | Removed | Added |
| --- | --- | --- |
| AnimatedButton.tsx AnimatedButton | 0/0 | 0/4 |
| AppearanceSwitch.tsx AppearanceSwitch | 0/0 | 0/1 |
| Badge.tsx Badge | 0/0 | 0/3 |
| Button.tsx Button | 0/0 | 0/1 |
| Calendar.tsx CalendarRoot | 0/0 | 0/3 |
| CalendarDayCell.tsx CalendarDayCell | 0/0 | 5/3 |
| CalendarFooter.tsx CalendarFooter | 0/0 | 1/1 |
| CalendarGrid.tsx CalendarGrid | 0/0 | 13/5 |
| CalendarHeader.tsx CalendarHeader | 0/0 | 6/4 |
| Card.tsx Card | 0/0 | 0/3 |
| ColorPicker.tsx ColorPicker | 0/0 | 0/1 |
| ColorPickerAlphaBar.tsx ColorPickerAlphaBar | 1/0 | 0/4 |
| ColorPickerAxisPanel.tsx ColorPickerAxisPanel | 2/1 | 0/3 |
| ColorPickerAxisSelector.tsx ColorPickerAxisSelector | 0/0 | 0/5 |
| ColorPickerAxisSlider.tsx ColorPickerAxisSlider | 3/1 | 0/4 |
| ColorPickerBrightnessBar.tsx ColorPickerBrightnessBar | 1/0 | 0/4 |
| ColorPickerChannelItem.tsx ColorPickerChannelItem | 0/0 | 0/2 |
| CopyButton.tsx CopyButton | 0/0 | 0/3 |
| DialogBody.tsx DialogBody | 0/0 | 4/0 |
| DialogCloseButton.tsx DialogCloseButton | 1/0 | 0/2 |
| DialogFooter.tsx DialogFooter | 0/0 | 7/3 |
| DialogHeader.tsx DialogHeader | 0/0 | 7/0 |
| DialogRoot.tsx DialogRoot | 0/0 | 2/6 |
| DrawerBody.tsx DrawerBody | 0/0 | 3/0 |
| DrawerFooter.tsx DrawerFooter | 0/0 | 7/3 |
| DrawerHeader.tsx DrawerHeader | 0/0 | 8/3 |
| DrawerOverlay.tsx DrawerOverlay | 0/1 | 2/1 |
| FadeSweepElement.tsx FadeSweepElement | 3/0 | 0/8 |
| GraphQlMutationForm.tsx GraphQlMutationForm | 0/3 | 33/16 |
| TimeSeriesChart.tsx TimeSeriesChart | 2/7 | 17/36 |
| TimeSeriesTip.tsx TimeSeriesTip | 0/0 | 1/6 |
| ToggleButton.tsx ToggleButton | 0/0 | 0/3 |
| ToggleGroup.tsx ToggleGroup | 0/0 | 0/3 |
| ToggleGroupButton.tsx ToggleGroupButton | 0/0 | 3/4 |
| useColorPickerState.ts useColorPickerState | 0/0 | 0/1 |
| useEventSourceSnapshot.tsx useEventSourceSnapshot | 0/0 | 0/4 |
| useTimeSeriesState.tsx useTimeSeriesState | 0/0 | 0/4 |

Population measurement is deliberately separate from these corpus pins. Before
this repair the frozen snapshot has 30 findings: 17 with covering reference
CompileSuccess and 13 covered only by bailouts. A private exact-source experiment
cleared 13 of the 17 confirmed false positives; this is not a whole-tree verdict
and does not establish that no findings were added.

The subsequent whole-snapshot measurement, from an archive-built binary at
`48ad641`, is **30 to 11: 19 removed, zero added, 11 unchanged**. Four remaining
occurrences have covering reference CompileSuccess; six have only Todo errors
and one only Suppression. No bailout-covered occurrence is declared correct or
incorrect. The metadata-only prerequisite `4c7dade` independently reproduces all
30 occurrences from `22b3128`, with no added or removed findings.

Binary SHA256: `62d976c3888bf2b5d6c70caa3dee82f34ca51cc004397bb7c1df73c77621e30e`.
