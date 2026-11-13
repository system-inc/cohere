package build_dependence

import (
	"maps"
)

// loweringFinding is a difference in a function of a unit block 1 calls independent, with the unit's name, and
// whether a unit the unit calls statically is dependent, which a callee's facts or no-return can carry.
type loweringFinding struct {
	unit            string
	difference      loweringDifference
	calleeDependent bool
}

// loweringTally is what comparing one build against the host found.
type loweringTally struct {
	packages        int
	skipped         []string
	unpaired        int
	independent     int
	instructions    int
	synthetic       int
	dependent       int
	dependentDiffer int
	findings        []loweringFinding

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
	tally.unpaired += other.unpaired
	tally.independent += other.independent
	tally.instructions += other.instructions
	tally.synthetic += other.synthetic
	tally.dependent += other.dependent
	tally.dependentDiffer += other.dependentDiffer
	tally.findings = append(tally.findings, other.findings...)
	tally.segments += other.segments
	tally.packageLevel += other.packageLevel
	tally.packageLevelDiffer = append(tally.packageLevelDiffer, other.packageLevelDiffer...)
	if tally.unknownFields == nil {
		tally.unknownFields = map[string]bool{}
	}
	maps.Copy(tally.unknownFields, other.unknownFields)
}
