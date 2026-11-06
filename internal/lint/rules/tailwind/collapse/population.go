package tailwind

// Population is what a run covered, as distinct from what it found.
//
// The name and the reason are `internal/differential`'s: without this a report cannot tell agreement
// from mutual silence, and a side claiming zero classes is enough on its own to disqualify the
// comparison. Recorded per design system, since two systems is the floor and one of them going empty
// while the other carries the run is exactly the shape a total would hide.
type Population struct {
	// SystemName is the design system this population was measured on.
	SystemName string
	// RegistryClasses and CorpusClasses are the two populations, kept apart because they answer
	// different questions. The registry is what the engine advertises and the corpus is what someone
	// happened to write, and an earlier table in this slice built from a corpus missed nine
	// families.
	RegistryClasses int
	CorpusClasses   int
	// EngineNullReadings is how many classes the engine itself declined, across both populations.
	// A run where this is zero on the corpus has almost certainly lost its corpus.
	EngineNullReadings int
	// UnknownPopulationClasses is how many classes named a population this package does not know.
	//
	// Never zero by accident: it can only be non-zero if a caller invented a population name, and
	// that miscounts the two floors below. Reported rather than absorbed, because absorbing it into
	// the registry count is exactly the shape of silent miscount the floors exist to catch.
	UnknownPopulationClasses int
	// ByOutcome counts every class by where it landed. The five sum to the two class counts.
	ByOutcome map[Outcome]int
}

// Total is every class this population covered, unknown ones included.
//
// Included rather than dropped so that the both-silent rate is computed over what was actually
// walked. A denominator that quietly excluded some classes would understate every rate derived from
// it, which is the direction that makes a run look better than it was.
func (population Population) Total() int {
	return population.RegistryClasses + population.CorpusClasses + population.UnknownPopulationClasses
}

// Answered is the classes on which both sides produced a reading.
//
// This is the denominator of the agreement rate, and it is deliberately not `Total`. Scoring
// agreement over every class would let a run that went entirely silent report a high rate, since the
// silent classes are not disagreements. Over answered classes a silent run reports zero of zero,
// which reads as the absence of evidence it is.
func (population Population) Answered() int {
	return population.ByOutcome[OutcomeAgreed] + population.ByOutcome[OutcomeDisagreed]
}
