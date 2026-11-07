package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"

	"golang.org/x/tools/go/packages"
)

// fieldCandidate is one single-letter struct field and what the rules decided for it. A field is not a local:
// it is renamed through globalrename, which follows every selector and keyed literal that names it and guards
// encoders, interfaces and string literals.
type fieldCandidate struct {
	goPackage *packages.Package
	variable  *types.Var
	newName   string
	rule      string
	category  string
	// structName is the type the field belongs to, for the report.
	structName string
}

// fieldCandidates decides, for every single-letter struct field declared in an editable file, its new name or
// why it has none.
func fieldCandidates(loaded *workspace.Workspace, words vocabulary) []fieldCandidate {
	var candidates []fieldCandidate
	for _, analyzedPackage := range loaded.Analyzed {
		for _, file := range loaded.RenamePackage(analyzedPackage).Files {
			for _, decided := range structFieldDecisions(analyzedPackage.TypesInfo, file, words) {
				decided.goPackage = analyzedPackage
				candidates = append(candidates, decided)
			}
		}
	}
	return candidates
}

// structFieldDecisions names the single-letter fields of every struct type in file: two adjacent letters
// `a, b` of one type are the left and right operands a test table hands a comparison, a field holding a type
// the rules name takes that name (`t *testing.T` is test), and a rune or byte is a character. A field of
// another basic type holds no word the rules can read, so it is left for the waves.
func structFieldDecisions(info *types.Info, file *ast.File, words vocabulary) []fieldCandidate {
	var candidates []fieldCandidate
	importNames := fileImportNames(info, file)
	ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
		structType, isStruct := node.(*ast.StructType)
		if !isStruct {
			return true
		}
		structName := "an unnamed struct"
		if len(stack) > 0 {
			if spec, isTypeSpec := stack[len(stack)-1].(*ast.TypeSpec); isTypeSpec {
				structName = spec.Name.Name
			}
		}
		var names []*ast.Ident
		for _, field := range structType.Fields.List {
			names = append(names, field.Names...)
		}
		decisions := map[*types.Var]decision{}
		for index, name := range names {
			variable, isVariable := info.Defs[name].(*types.Var)
			if !isVariable || !isJudgedLetter(name.Name) {
				continue
			}
			if index+1 < len(names) && name.Name == "a" && names[index+1].Name == "b" {
				if second, isSecond := info.Defs[names[index+1]].(*types.Var); isSecond &&
					types.Identical(variable.Type(), second.Type()) {
					decisions[variable] = decision{names: []string{"left"}, rule: "field pair"}
					decisions[second] = decision{names: []string{"right"}, rule: "field pair"}
				}
			}
			if _, decided := decisions[variable]; decided {
				continue
			}
			fieldNames, rule := namesFromType(variable.Type(), words), "type"
			if basic, isBasic := variable.Type().Underlying().(*types.Basic); isBasic &&
				(basic.Name() == "rune" || basic.Name() == "byte") {
				fieldNames, rule = append(fieldNames, "character"), "letter convention"
			}
			if fieldNames = usableNames(fieldNames, importNames); len(fieldNames) == 0 {
				decisions[variable] = decision{category: "field: no rule names a " + typeKind(variable.Type()) +
					" held by " + variable.Name()}
				continue
			}
			decisions[variable] = decision{names: fieldNames, rule: rule}
		}
		for _, name := range names {
			variable, isVariable := info.Defs[name].(*types.Var)
			if !isVariable {
				continue
			}
			if decided, found := decisions[variable]; found {
				candidate := fieldCandidate{variable: variable, rule: decided.rule, category: decided.category,
					structName: structName}
				if len(decided.names) > 0 {
					candidate.newName = decided.names[0]
				}
				candidates = append(candidates, candidate)
			}
		}
		return true
	})
	return candidates
}

// otherLetterDeclarations lists every other declaration the rule flags that this pass does not rename: a
// constant, a package-level variable, a function or a type named with one letter. Each is printed for the
// waves.
func otherLetterDeclarations(loaded *workspace.Workspace) []string {
	var listed []string
	seen := map[string]bool{}
	for _, analyzedPackage := range loaded.Analyzed {
		for _, file := range loaded.RenamePackage(analyzedPackage).Files {
			ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier || !isJudgedLetter(identifier.Name) {
					return true
				}
				kind := ""
				switch declared := analyzedPackage.TypesInfo.Defs[identifier].(type) {
				case *types.Const:
					kind = "constant"
				case *types.TypeName:
					kind = "type"
				case *types.Func:
					kind = "function"
				case *types.Var:
					if !declared.IsField() && declared.Parent() == analyzedPackage.Types.Scope() {
						kind = "package-level variable"
					}
				}
				if kind == "" {
					return true
				}
				position := relativePosition(loaded, analyzedPackage.Fset.Position(identifier.Pos()))
				if !seen[position] {
					seen[position] = true
					listed = append(listed, fmt.Sprintf("%s: %s in %s: %s, which this pass does not rename",
						position, identifier.Name, enclosingFunction(stack), kind))
				}
				return true
			})
		}
	}
	return listed
}

// planFields hands every field with a name to globalrename and returns its result, and the candidates
// deduplicated by declaration, since a file two variants share declares its fields in both.
func planFields(loaded *workspace.Workspace, candidates []fieldCandidate) (*global_rename.Result, []fieldCandidate) {
	var proposals []global_rename.Proposal
	var unique []fieldCandidate
	seen := map[string]bool{}
	for _, candidate := range candidates {
		position := candidate.goPackage.Fset.PositionFor(candidate.variable.Pos(), false)
		key := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, candidate)
		if candidate.newName != "" {
			proposals = append(proposals, global_rename.Proposal{
				Package: candidate.goPackage,
				Object:  candidate.variable,
				NewName: candidate.newName,
			})
		}
	}
	return global_rename.Plan(loaded, proposals, global_rename.Options{}), unique
}

// fieldOutcome is what globalrename decided for one field: its accepted name, or the reason it refused.
func fieldOutcome(result *global_rename.Result, candidate fieldCandidate) (string, string) {
	position := candidate.goPackage.Fset.PositionFor(candidate.variable.Pos(), false)
	for _, accepted := range result.Accepted {
		if accepted.Position.Filename == position.Filename && accepted.Position.Offset == position.Offset {
			return accepted.NewName, ""
		}
	}
	for _, rejection := range result.Rejections {
		if rejection.Position.Filename == position.Filename && rejection.Position.Offset == position.Offset {
			return "", rejection.Class + ": " + strings.TrimSpace(rejection.Reason)
		}
	}
	return "", ""
}
