package build_dependence

import (
	"maps"
)

// loweringFinding is a difference in a function of a unit block 1 calls independent, with the unit's name, whether a
// unit the unit calls statically is dependent, and whether the no-return facts judgment sends the unit to be judged
// again, which explains the difference: such a unit is not in block 4's independent set.
type loweringFinding struct {
	unit            string
	difference      loweringDifference
	calleeDependent bool
	judgedAgain     bool
}

// loweringTally is what comparing one build against the host found.
type loweringTally struct {
	packages        int
	skipped         []string
	independent     int
	instructions    int
	synthetic       int
	dependent       int
	dependentDiffer int
	findings        []loweringFinding

	// The host's functions and initializer segments with no counterpart in the build, by why
	// (packageComparison.unpaired): built from no syntax, in a file only the host compiles, in an independent unit,
	// and anything else, the last two named.
	unpairedNoSyntax    int
	unpairedHostOnly    int
	unpairedIndependent []string
	unpairedOther       []string

	// judgedAgain are the independent functions and segments compared whose unit the no-return facts judgment sends
	// to be judged again, counted among independent.
	judgedAgain int

	// segments are the package initializers' segments of independent units compared, counted among independent.
	segments int

	// packageLevel are the package initializers' segments that belong to no unit, the frame and the imports'
	// initializers, compared, and packageLevelDiffer those that differ, which no unit's verdict answers for.
	packageLevel       int
	packageLevelDiffer []loweringFinding

	unknownFields map[string]bool
}

// merge adds other to tally.
func (tally *loweringTally) merge(other loweringTally) {
	tally.packages += other.packages
	tally.skipped = append(tally.skipped, other.skipped...)
	tally.unpairedNoSyntax += other.unpairedNoSyntax
	tally.unpairedHostOnly += other.unpairedHostOnly
	tally.unpairedIndependent = append(tally.unpairedIndependent, other.unpairedIndependent...)
	tally.unpairedOther = append(tally.unpairedOther, other.unpairedOther...)
	tally.independent += other.independent
	tally.instructions += other.instructions
	tally.synthetic += other.synthetic
	tally.dependent += other.dependent
	tally.dependentDiffer += other.dependentDiffer
	tally.findings = append(tally.findings, other.findings...)
	tally.judgedAgain += other.judgedAgain
	tally.segments += other.segments
	tally.packageLevel += other.packageLevel
	tally.packageLevelDiffer = append(tally.packageLevelDiffer, other.packageLevelDiffer...)
	if tally.unknownFields == nil {
		tally.unknownFields = map[string]bool{}
	}
	maps.Copy(tally.unknownFields, other.unknownFields)
}
