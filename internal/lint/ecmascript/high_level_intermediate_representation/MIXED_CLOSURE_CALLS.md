# Conditional calls through mixed captures

Reducing WisdomHome exposed a second, synthetic counterpart after computed
read kinds were repaired. Calling an unknown function with a MaybeFrozen value
directly is not a conditional mutation in React's model. Wrapping the same call
in a closure inside a conditional expression incorrectly widened the capture in
cohere. That closure cannot be inlined in an expression block, so inlining was
not an appropriate repair or exemption.

The existing closure refinement rejected every conditional mutation before
consulting capture kinds. The bounded extension first preserves that existing
read-only proof, then admits a separate proof for MaybeFrozen captures. It maps
outer capture kinds to the nested function's own context identifiers and runs
the aliasing graph with those entry kinds. A retained mutation rejects the proof.
Default graph callers still initialize contexts exactly as before.

This extension requires an actual MaybeFrozen capture and rejects Mutable,
unknown and Global captures. Frozen-only edges already do not propagate mutable
range widening; broadening that separate case is unnecessary here. Unknown
methods and indirect calls, constructors, nested function definitions, context
and global writes, suspension and freeze effects remain conservative barriers.
Direct Date calls are excluded as known impure; method and indirect-call guards
also exclude Math.random and aliased clocks. This is not a complete purity or
interprocedural effect analysis.

## Validation

Six exact sources were compared with compiler 1.0.0 and an independently failing
bad-dependency control. The conditional mixed-capture case, its frozen counterpart,
direct call and unconditional call all compile successfully. A mutable receiver
retains two preservation errors; an omitted dependency retains one.

Restoring the previous ranges and closure helper makes the mixed case report two
false positives and makes the low-level conditional-mutation case remain Mutable.
The other memo controls and eight effect exclusions remain green. The repaired
implementation passes the complete lint suite with no corpus repin: 796 deep /
3310 flat dependencies, 159 assigned / 142 surviving scopes, and the existing
preservation goldens are unchanged.

An earlier, broader private experiment cleared the reproduction but misclassified
an impure builtin and increased the scope corpus to 162 assigned / 145 surviving.
It was not landed or used for a population claim. The bounded proof above retains
the original guards and passes those same oracles.
