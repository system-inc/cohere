package rename

import (
	"go/ast"
	"go/types"
)

// A type switch's guard, `switch value := subject.(type)`, declares no object of its own: go/types gives each
// case clause an implicit variable, typed for its case, and the guard's identifier names all of them. The
// engine renames them as one: every clause variable of a switch takes the same new name or none does, and the
// guard's identifier is rewritten with them.

// typeSwitchClauseVariables maps each clause variable of every type switch in files to the guard identifier
// that declares it.
func typeSwitchClauseVariables(info *types.Info, files []*ast.File) map[types.Object]*ast.Ident {
	guards := map[types.Object]*ast.Ident{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			statement, isTypeSwitch := node.(*ast.TypeSwitchStmt)
			if !isTypeSwitch {
				return true
			}
			guard := typeSwitchGuard(statement)
			if guard == nil {
				return true
			}
			for _, clause := range statement.Body.List {
				if object := info.Implicits[clause]; object != nil {
					guards[object] = guard
				}
			}
			return true
		})
	}
	return guards
}

// typeSwitchGuard is the identifier a type switch binds, or nil when it binds none.
func typeSwitchGuard(statement *ast.TypeSwitchStmt) *ast.Ident {
	assignment, isAssignment := statement.Assign.(*ast.AssignStmt)
	if !isAssignment || len(assignment.Lhs) != 1 {
		return nil
	}
	identifier, isIdentifier := assignment.Lhs[0].(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	return identifier
}

// partialTypeSwitches finds every switch whose clause variables are not all renamed to one name, and returns
// its accepted clause variables, each with the reason to drop it.
func partialTypeSwitches(guards map[types.Object]*ast.Ident, accepted map[types.Object]string) map[types.Object]string {
	clauses := map[*ast.Ident][]types.Object{}
	for object, guard := range guards {
		clauses[guard] = append(clauses[guard], object)
	}
	drops := map[types.Object]string{}
	for _, objects := range clauses {
		newNames := map[string]bool{}
		renamed := 0
		for _, object := range objects {
			if newName, isRenamed := accepted[object]; isRenamed {
				newNames[newName] = true
				renamed++
			}
		}
		if renamed == 0 || (renamed == len(objects) && len(newNames) == 1) {
			continue
		}
		for _, object := range objects {
			if _, isRenamed := accepted[object]; isRenamed {
				drops[object] = "a type switch's clause variables take one name together, and not all of them can"
			}
		}
	}
	return drops
}

// typeSwitchGuardEdits rewrites the guard identifier of every switch whose clause variables are renamed.
func typeSwitchGuardEdits(loadedPackage Package, accepted map[types.Object]string) []Edit {
	var edits []Edit
	seen := map[*ast.Ident]bool{}
	for object, guard := range typeSwitchClauseVariables(loadedPackage.Info, loadedPackage.Files) {
		newName, renamed := accepted[object]
		if !renamed || seen[guard] {
			continue
		}
		seen[guard] = true
		position := loadedPackage.FileSet.PositionFor(guard.Pos(), false)
		edits = append(
			edits,
			Edit{Filename: position.Filename, Offset: position.Offset, OldText: guard.Name, NewText: newName},
		)
	}
	return edits
}
