package react_conformance

// NothingImplemented is the Implementation used until a real one exists.
//
// It declines every fixture. It does not return an empty result, and the difference is the whole
// reason the type is written out rather than left as a nil check: an empty result asserts "no
// errors here", which is a wrong answer on a corpus where every fixture expects at least one, and
// would score as 325 failures, a number that looks like a working implementation doing badly
// rather than an absent one.
type NothingImplemented struct{}

// Analyze declines.
func (NothingImplemented) Analyze(Fixture) (Result, error) {
	return Result{}, &ErrUnsupported{Reason: "no React compiler rules are implemented in cohere yet"}
}
