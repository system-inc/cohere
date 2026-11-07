package rename

import (
	"go/ast"
	"go/types"
	"strings"
	"testing"
)

// proposeClauseVariables proposes newName for the clause variables of every type switch, every clause when
// onlyFirst is false and the first clause's alone when it is true.
func proposeClauseVariables(newName string, onlyFirst bool) func(Package) []Proposal {
	return func(loadedPackage Package) []Proposal {
		var proposals []Proposal
		for _, file := range loadedPackage.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				statement, isTypeSwitch := node.(*ast.TypeSwitchStmt)
				if !isTypeSwitch {
					return true
				}
				for index, clause := range statement.Body.List {
					if variable, isVariable := loadedPackage.Info.Implicits[clause].(*types.Var); isVariable &&
						(!onlyFirst || index == 0) {
						proposals = append(proposals, Proposal{Object: variable, NewName: newName})
					}
				}
				return true
			})
		}
		return proposals
	}
}

const typeSwitchFixture = `package fixture

func describe(value any) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case []int:
		return len(v)
	default:
		_ = v
		return 0
	}
}
`

func TestTypeSwitchRenamedWholeRewritesTheGuard(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixtureProposing(test, typeSwitchFixture, proposeClauseVariables("typed", false))
	if len(plan.Accepted) != 3 || len(plan.Rejections) != 0 {
		test.Fatalf(
			"accepted %d and rejected %+v, want all three clauses accepted",
			len(plan.Accepted),
			plan.Rejections,
		)
	}
	if !strings.Contains(rewritten, "switch typed := value.(type) {") || strings.Contains(rewritten, "len(v)") {
		test.Fatalf("the guard and every clause's use were not rewritten:\n%s", rewritten)
	}
}

func TestTypeSwitchPartlyProposedIsRefused(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixtureProposing(test, typeSwitchFixture, proposeClauseVariables("typed", true))
	requireRejected(test, plan, "take one name together")
	if rewritten != typeSwitchFixture {
		test.Fatalf("a refused switch was rewritten:\n%s", rewritten)
	}
}

func TestTypeSwitchConflictInOneClauseRefusesEveryClause(test *testing.T) {
	test.Parallel()
	source := strings.Replace(
		typeSwitchFixture,
		"func describe(value any) int {",
		"func describe(value any, typed int) int {",
		1,
	)
	source = strings.Replace(source, "\t\treturn len(v)\n\tcase []int:", "\t\treturn len(v) + typed\n\tcase []int:", 1)
	plan, _ := renameFixtureProposing(test, source, proposeClauseVariables("typed", false))
	if len(plan.Accepted) != 0 {
		test.Fatalf("accepted %d clause variables, want none when one clause would capture typed", len(plan.Accepted))
	}
}
