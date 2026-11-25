package program

// suppressionTally is what one file's directives did, summed across the run.
//
// directives and withheld are one file's, for the findings cache, and add leaves them out: whether the file has
// a directive or names a rule in one, and which of its findings the directives withheld.
type suppressionTally struct {
	directives bool
	withheld   []LintCacheWithheld

	applied          int
	appliedNoReason  int
	unusedDirectives int

	// unusedForUnrunRule is the subset of unusedDirectives naming only rules this run did not run.
	//
	// Separated because the two mean opposite things to a reader. A directive that silenced nothing
	// while its rule ran is dead scaffolding worth deleting. A directive naming a rule cohere has
	// not ported yet silenced nothing because nothing looked, and deleting it would remove a
	// suppression the gate still needs. Reporting them as one number tells a reader to go delete
	// comments that are load-bearing today.
	unusedForUnrunRule int

	// dead is where each unused directive outside unusedForUnrunRule sits.
	dead []DeadSuppression
}

func (tally *suppressionTally) add(other suppressionTally) {
	tally.applied += other.applied
	tally.appliedNoReason += other.appliedNoReason
	tally.unusedDirectives += other.unusedDirectives
	tally.unusedForUnrunRule += other.unusedForUnrunRule
	tally.dead = append(tally.dead, other.dead...)
}
