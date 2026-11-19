package standing_facts

import "github.com/system-inc/cohere/internal/go_engine/fact_reach"

// Result is one package's standing bodies in one build, settled, and what settling them took, for the build's census.
// A nil Result, and the empty one a package with no host's side or one linted whole has, hold nothing standing.
type Result struct {
	// Partial is whether the build lints only part of the package, so that facts were compared at all: false for a
	// package the checker handed no host's side of, and for one the host's side says the build lints whole.
	Partial bool

	// Settlement is what the fixed point took out and asked (fact_reach.Settle), nil when Partial is false.
	Settlement *fact_reach.Settlement

	// FunctionsCompared is how many functions' no-return facts were compared with the host's, and FunctionsNamed how
	// many of them named their object: the fact differed, or the build has no such function.
	FunctionsCompared int
	FunctionsNamed    int

	// ImportedAsked is how many answers the host read from other packages were asked of the build before the walk,
	// and ImportedNamed how many of them named their object: the answer differed, or could not be read.
	ImportedAsked int
	ImportedNamed int

	// standing is the bodies still standing once the walk ended, the host's own copy with every body the facts reach
	// taken out, and only read once run has returned.
	standing map[fact_reach.Place]bool
}

// Stands is whether the function body declared in the file parsed from filename, its `func` at offset, is left to the
// host's findings in this build. It only reads, so every rule of the package may ask it at once.
func (result *Result) Stands(filename string, offset int) bool {
	return result != nil && result.standing[fact_reach.Place{File: filename, Offset: offset}]
}
