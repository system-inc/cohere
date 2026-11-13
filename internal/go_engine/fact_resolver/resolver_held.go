package fact_resolver

import (
	"go/types"
)

// Held is a test's hand on the answers a run gives (#h350vqr), which only a test sets. The unit-isolation test
// checks a package whole, hearing every answer the run computes about its objects, then checks it again with half its
// function bodies stubbed, and answers every question about the package's objects with what the whole package
// answered, so a kept unit reads the whole run's answers and never one recomputed from a stub: the only difference a
// kept unit can see is another unit's syntax and types. Its methods are called from every pass at once.
type Held interface {
	// Answer is the answer of the kind named kindName about object, encoded as Ask encodes one, to hand the question in
	// place of computing it, and whether there is one; false has the run answer as it would without a hand on it.
	Answer(kindName string, object types.Object) ([]byte, bool)
	// Computed hears each answer the run computed, of the kind named kindName about object, encoded as Answer hands
	// one, once per answer, after it is kept.
	Computed(kindName string, object types.Object, encoded []byte)
}

// HoldAnswers puts held's hand on every answer the resolver gives, before the run asks anything: each question it
// would compute is offered to held.Answer first, and each answer it computes told to held.Computed. An answer the
// memo, the store or the types alone of a function without a body give is never offered, since it is computed from
// no body either way. A run under a hand writes no store: a held answer is memoized but never recorded, so a store's
// whole answers would lack it. Only a test calls it: an engine run has no hand on its answers, and a test holds that
// no file but a test's calls HoldAnswers.
func (resolver *Resolver) HoldAnswers(held Held) {
	resolver.held = held
}
