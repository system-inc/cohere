package react_conformance

import "fmt"

// UnknownPragmaError reports a directive the runner does not model.
//
// This is an error rather than a skip on purpose, and the reason is the whole point of the type.
//
// A pragma reconfigures what a fixture expects. `@validateRefAccessDuringRender` turns a validation
// on; without it the same source is legal and the expectation is wrong. So an unmodelled directive
// does not make a fixture merely unrunnable, it makes the fixture's verdict *unrelated to the
// implementation being scored*, and a runner that skips it reports "not applicable" for a case it
// silently got wrong, while a runner that ignores it reports a pass or a fail it has not earned.
//
// Upstream demonstrates the failure rather than merely permitting it. `parseConfigPragmaForTests`
// in `Utils/TestUtils.ts` reads:
//
//	for (const {key, value: val} of splitPragma(pragma)) {
//	  if (!hasOwnProperty(defaultOptions, key)) {
//	    continue;
//	  }
//
// and `parseConfigPragmaEnvironmentForTest` has the identical `continue` against
// `EnvironmentConfigSchema.shape`. Both drop an unrecognised key without a word. Measured against
// the 325 error fixtures at the pinned sha, six distinct directives in active use fall through
// those two `continue`s: `@skip`, `@flow`, `@enableFlowSuppressions`, `@enableNewMutationAliasingModel`,
// `@enablePropagateDepsInHIR`, and `@compilationMode(infer)`, the last because `splitPragma` splits
// values on `:` only, so the parenthesised spelling yields the key `compilationMode(infer)`, which
// matches nothing. `@skip` is the sharpest of them: it is not read anywhere in `packages/snap`, so
// eight fixtures carry an instruction that has never done anything.
//
// Refusing is cheap here and silence is not. A refusal is one line naming the directive and the
// file; a silent skip is a green suite over a corpus a third of which was never scored.
type UnknownPragmaError struct {
	FixturePath string
	Pragma      Pragma
}

func (pragmaError *UnknownPragmaError) Error() string {
	return fmt.Sprintf(
		"%s: unrecognised pragma %s; the runner refuses rather than skipping, because an unmodelled directive changes what the fixture expects and a skipped fixture that reports as a pass is exactly the defect this suite exists to catch. Add it to knownPragmas with a note on what it means, or record it in ignoredPragmas with the evidence that it does not affect diagnostics",
		pragmaError.FixturePath, pragmaError.Pragma,
	)
}
