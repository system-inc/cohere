# Computed read kinds

React Compiler 1.0.0 computes the same CreateFrom signature for non-primitive
PropertyLoad and ComputedLoad instructions. Cohere used CreateFrom only for
named properties and unconditionally created Mutable results for computed reads.
Thus items[key] lost the receiver's frozen kind while items.first retained it.

The computed arm now copies the receiver kind too. Six reference-checked memo
fixtures cover constant and dynamic indexes, named access, mutable receivers,
missing dependencies and removal of the subsequent call. Before the fix, both
computed frozen cases report two false positives; afterwards both compile clean.
The mutable-receiver control retains two findings and missing dependencies retain
one. A low-level pair verifies the exact CreateFrom effect and distinguishes
component arguments from ordinary mutable function parameters.

## Corpus attribution

The dependency distribution changes from 795 deep / 3303 flat to 796 / 3310.
The complete per-file net changes, measured by multiset comparison, are:

| File | Deep | Flat |
| --- | ---: | ---: |
| TimeSeriesTip.tsx | +1 | +4 |
| Switch.tsx | +13 | -9 |
| DialogBody.tsx | -3 | +2 |
| DialogFooter.tsx | -2 | +2 |
| DialogHeader.tsx | -2 | +2 |
| DrawerBody.tsx | -2 | +2 |
| DrawerFooter.tsx | -2 | +2 |
| DrawerHeader.tsx | -2 | +2 |

TimeSeriesTip gains three dataPoint roots, a properties root, localeCode and
properties.timeInterval, replacing one anonymous root. Switch replaces a broad
scope's root-only inputs with narrower configuration, size, intrinsic-element
and prop paths. The six dialog/drawer changes replace theme lookup paths with
the resulting class-name values and local roots after computed theme lookup
ceases to widen its frozen receiver. No other hoistable rows change.

The overlap corpus changes equally on both sides: Confetti gains one merge,
TimeSeriesTip and InputSlider each lose one. The function-operand-skip gap remains
four. Scope counts and all preservation goldens remain unchanged.

The preceding imperative-handle commit 44d6bf9 measured seven to six findings
on the frozen snapshot: InputMultipleSelect removed, zero additions. Its binary
SHA256 is 7933fcb377be193f06148026cdbc3d6cb955dce8a216f5265d96ef7466331a3e.
Computed reads are a separately tested prerequisite: the conditional nested-call
counterpart still exposes an additional closure-analysis gap, not permission to
silence the preservation validator.
