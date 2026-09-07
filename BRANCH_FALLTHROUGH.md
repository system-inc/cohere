# Remapping an inlined branch's fallthrough

EachBlockReferencePointer rewrote Branch.Consequent and Branch.Alternate but
omitted Branch.Fallthrough. CopyNestedBodyInto uses that visitor to move a nested
function into the parent's block-ID space. The copy retained the child's numeric
fallthrough ID, which could name an unrelated block already in the parent.

In a two-memo reproduction, each callback returns a conditional Set allocation.
The second copied branch's fallthrough points to the first callback's test block.
Reverse postorder therefore interleaves the two bodies, alignment merges their
scopes and the second memo is compared against the first memo's dependency too.
The diagnostic is downstream of the incorrect block reference, not a reason to
exempt conditional memoization from validation.

The repair adds the missing field to the mutable-reference visitor. It applies
to every caller that renames blocks, not only memo callbacks. The direct copy
fixture observes an original child fallthrough of 3 which must become 8; before
the repair it stays 3.

## Fixtures and controls

The existing read-only/mutable visitor cross-check had a blind spot: its Branch
sample never populated Fallthrough. The sample now assigns that field its own
distinct block ID, so the old visitor fails visibly. A direct copy test inspects
Branch.Fallthrough itself rather than reusing the faulty visitor as its oracle.

The end-to-end pair covers conditional Set and array allocations, an unconditional
variant and a genuinely unpreserved dependency. The first two fail before the
repair; the unconditional case stays silent and the bad dependency still reports.
All three clean exact sources get CompileSuccess from React compiler 1.0.0 with
preservation validation enabled and a proven-failing bad-dependency control.

The private intervention passes the existing HIR suite without changing any
corpus-count expectations. The production fixture suite and broader lint suite
both pass, as does go build ./....

The `22b3128` archive binary measures 30 findings on the frozen snapshot, against
44 at `dc82ac8`: 14 removed, none added and 30 unchanged. Seventeen occurrences
have covering CompileSuccess and remain confirmed false positives. Thirteen are
bailout-covered (12 Todo, one Suppression) and remain unadjudicated.
The binary's SHA256 is
`328e45cce1d52ba81b206154e93533e66c7f9f1758a029733aac1e8f13d378ed`.
