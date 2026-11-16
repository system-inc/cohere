package check

import "fmt"

// positionJudgment is whether a finding of a package names a file outside it, so its findings can change with a line of
// another package (see positionDependent), or that nobody judged it: an entry encoded without the load's files, as the
// keep encodes those it holds for the extra builds, whose reads never ask. Its zero value is not judged, so an entry
// that skipped the judgment can never read as one judged independent of other packages' positions.
type positionJudgment struct {
	judged    bool
	dependent bool
}

// judgedPosition is a judgment made: dependent is whether a finding names a file outside its package.
func judgedPosition(dependent bool) positionJudgment {
	return positionJudgment{judged: true, dependent: dependent}
}

// read is whether owner's findings can change with a line of another package, and an error, with no answer, when its
// entry was encoded without that judgment.
func (judgment positionJudgment) read(owner string) (bool, error) {
	if !judgment.judged {
		return false, fmt.Errorf("the entry of %s was encoded without judging whether its findings name another "+
			"package's files, so it is given no alias", owner)
	}
	return judgment.dependent, nil
}
