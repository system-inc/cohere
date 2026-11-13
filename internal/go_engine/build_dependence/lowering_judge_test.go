package build_dependence

import (
	"fmt"
	"go/token"
)

// loweringJudge is one build's judgment, read by unit.
type loweringJudge struct {
	dependence *Dependence
	units      []Unit
}

// unitAt is the unit of packageId holding position in fileSet, by the file a reader sees and the offset in the file
// parsed, as the judgment names units, and whether one does.
func (judge loweringJudge) unitAt(packageId string, fileSet *token.FileSet, position token.Pos) (int, bool) {
	tokenFile := fileSet.File(position)
	if tokenFile == nil {
		return 0, false
	}
	return judge.dependence.UnitContaining(packageId, tokenFile.Position(position).Filename,
		tokenFile.Offset(position))
}

// unitLabel names a unit for a report: its package, the file a reader sees and its offset.
func (judge loweringJudge) unitLabel(index int) string {
	unit := judge.units[index]
	return fmt.Sprintf("%s %s:%d", unit.PackageId, unit.File, unit.Start)
}

// calleeDependent is whether a unit the unit at index calls statically is dependent.
func (judge loweringJudge) calleeDependent(index int) bool {
	for _, callee := range judge.units[index].Calls {
		if judge.units[callee].Dependent {
			return true
		}
	}
	return false
}
