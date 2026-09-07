# For-of header block kinds

Task `#3txtdnt` is a prerequisite discovered while testing `#09814t2`'s frozen-state candidate.
The production change reserves the `for...of` initializer and test as `BlockKindLoop`, not
`BlockKindBlock`. No state signature, validator exemption, or scope-pruning predicate changes.

## Reference and mechanism

Compiler 1.0.0's `BuildHIR` reserves both headers with `builder.reserve("loop")` in its
`ForOfStatement` arm. Cohere's scope alignment uses block kinds: ordinary statement blocks do
not inherit the enclosing value-block interval. Misclassifying headers therefore leaves a scope
that starts in a header and ends in the body crossing the loop boundary.

The new isolated fixture demonstrates both headers. Initializer `[5,10)` and test `[7,10)`
remain unchanged before the fix; both align to the enclosing loop's `[3,17)` afterward.
Scopes wholly before and after the loop remain unchanged. The fixture also checks that
reactive-tree conversion preserves every instruction without duplicated blocks or unmatched gotos.

Three source-level conservation fixtures are minimized from event-source dispatch code:
listener invocation, conditional dispatch, and nested dispatch. Without the correction their
reactive trees contain 5/6, 15/16, and 19/20 input instructions respectively. With it they retain
all instructions. A separate nested-loop test checks both header kinds and ensures continuations
remain ordinary statement blocks. All defect cases fail against the previous lower.go, while the
before/after alignment controls pass.

## Corpus differential

Same 400-file / 680-function sample, compared with an overlay changing only lower.go:

- Seven scoped-loop instruction-loss cases become zero.
- Composite-value loss cases fall from six to five; the five survivors are unchanged.
- Eighteen instructions are recovered across eight functions; no new loss cases appear.
- Four double emissions remain; unmatched gotos and non-implicit scope breaks stay zero.

Recovered functions are `notifyConnectionChange`, `dispatchEnvelope`, `dispatchResync`,
`useLinkedFields`, `toWireSurveySubmission`, `interleavedVertices`, `findClosestDotIndices`, and
`resolveChoiceSemanticGroup`. The last previously belonged to the composite-value bucket.
Conservation assertions are tightened to five composite-value and zero scoped-loop losses.

The separate 150-file hoistable-dependency sample changes from 625 deep / 2843 flat to
625 deep / 2841 flat. This is five removed and three added occurrences, not simply two removals:

- Each of the three event-source dispatch helpers loses an anonymous temporary dependency.
- `dispatchEnvelope` gains `channel` and `envelope`; `dispatchResync` gains `channels`.
- `rewriteOnce` loses an anonymous temporary and `updatedSource` after loop-scope alignment.

These changes are confined to two source files. The golden-dependency and scope oracle tests
retain their existing assertions; no oracle threshold is loosened. The flat-count calibration
is updated to the measured 2841, with this finding-level record rather than a claim that any
smaller dependency count is automatically more correct.

## Scope of the repair

This does not claim a complete for-of lowering port. The upstream collection-expression placement,
binding placement, and body classification also differ and are not changed here. The controlled
failure addressed here is specifically header classification and the downstream scope boundary.

State freezing remains separate. Its private candidate originally exposed one component/hook
scope-nesting violation in `useForm`; changing these two kinds removes that violation. The other
candidate corpus-count movements still require reevaluation and attribution before activation.
Neither this document nor that intervention establishes a new whole-tree preservation count.

The complete `go test ./internal/lint/... -count=1` suite and `go build ./...` pass for this fix.
A private overlay of the frozen-state candidate also passes all 21 origin/memoization cases and
the component/hook scope-nesting invariant with these header kinds. That targeted check does not
certify the candidate's broader suite or activate it in the shared checkout.

Raw before/after traces and red-control logs are under `/private/tmp/wisebeing-89qdg8e.KDVsxI/`.
