package marker

// The markers the house rules read, each the text of a comment line after its `// `, up to and including the colon,
// and each matched as written. They live here, one home for all of them, so a tool that must leave a marker line as
// it is, line_wrap's reflow, reads them from All rather than guessing a marker from its shape. A test in this package
// fails if any rule hands Read or ExcuseFor a marker that is not one of these, or if one of these is missing from All.
const (
	// NotParallel excuses a test from running in parallel, read by cohere-go/testing-require-parallel. It is the
	// spelling every excused test in cohere already uses.
	NotParallel = "Not parallel:"
	// Ignored excuses a dropped error, read by cohere-go/correctness-require-handled-error.
	Ignored = "Ignored:"
	// Invariant says why a panic can never happen, read by cohere-go/correctness-require-invariant-reason.
	Invariant = "Invariant:"
	// Mutates opens the doc comment line that makes a parameter an out-parameter by contract, read by
	// cohere-go/correctness-no-caller-data-mutation. It is the twin's `@mutates <parameter> <why>` in the shape of the
	// house's Go markers: the marker, the parameter's name, the reason.
	Mutates = "Mutates:"
	// TimingAssertion excuses a timing assertion, read by cohere-go/testing-no-timing-assertion.
	TimingAssertion = "Timing assertion:"
	// Reads declares a walk, read by cohere-go/correctness-require-declared-walk. Its rule matches it with exactly
	// `// ` before it and nothing else, `"// " + Reads`, because a planner reads the glob after it, and a comment the
	// rule took as a declaration the planner skipped would leave the walk undeclared with no finding to say so.
	Reads = "Reads:"
	// Unsafe says why an escape hatch is safe, read by cohere-go/concurrency-require-escape-hatch-reason.
	Unsafe = "Unsafe:"
	// NotAMachinePath excuses a literal that reads as a machine's path, read by cohere-go/testing-no-machine-path.
	NotAMachinePath = "Not a machine path:"
	// Uncached excuses a run from the test cache, read by cohere-go/testing-no-uncached-run.
	Uncached = "Uncached:"
	// WaitDelay excuses a WaitDelay, read by cohere-go/concurrency-no-buffered-wait-delay. It is Adamic's spelling, so
	// a reason written for Adamic's rule carries over.
	WaitDelay = "WaitDelay:"
)

// All is every marker a house rule reads, spelled as its constant is.
var All = []string{
	NotParallel,
	Ignored,
	Invariant,
	Mutates,
	TimingAssertion,
	Reads,
	Unsafe,
	NotAMachinePath,
	Uncached,
	WaitDelay,
}
