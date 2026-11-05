package adamic

import (
	"path/filepath"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/sourcename"
	"github.com/system-inc/cohere/policy"
)

var ambientDeclarationText = policy.MessageOf("adamic/no-ambient-declarations", "ambientDeclaration")

var ambientFieldText = policy.MessageOf("adamic/no-ambient-declarations", "ambientField")

// NoAmbientDeclarations refuses runtime claims with no emitted definition. Each
// form has a strict-accepted, Node-failing probe in testdata/ambient. References
// alone are not runtime claims: only a value use from the referenced declarations
// reports. Declaration files belong to the host's contract, outside this rule.
// Type-only bindings are erased contracts, not runtime reads. An unbacked field
// or a field narrowing its inherited type can instead promise a value absent at
// runtime; same-type inherited fields and class-assigned fields stay quiet.
var NoAmbientDeclarations = rule.Rule[rule.NoOptions]{
	Name:             "adamic/no-ambient-declarations",
	NeedsTypeChecker: true,
	ProgramReads:     rule.ReadsOtherFiles,
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		file := ruleContext.SourceFile
		if file == nil || file.IsDeclarationFile {
			return nil
		}
		// An Adamic `.a` file is TypeScript, so ask sourcename what each name is treated as.
		switch strings.ToLower(filepath.Ext(sourcename.TreatedAs(file.FileName().AsString()))) {
		case ".ts", ".tsx":
		default:
			return nil
		}
		message := func(name string) rule.Message {
			return rule.Message{
				Id:          "ambientDeclaration",
				Description: ambientDeclarationText.Render(map[string]string{"declaration": name}),
			}
		}
		report := func(node *ast.Node) { ruleContext.ReportNode(node, message(node.Text())) }
		declared := func(
			node *ast.Node,
		) bool {
			return ast.GetCombinedModifierFlags(node)&ast.ModifierFlagsAmbient != 0
		}
		var runtimeUses map[*ast.Symbol]bool
		usedAtRuntime := func(node *ast.Node) bool {
			if runtimeUses == nil {
				runtimeUses = ambientRuntimeUses(ruleContext)
			}
			return runtimeUses[ambientCanonicalSymbol(ruleContext, ruleContext.TypeChecker.GetSymbolAtLocation(node))]
		}
		reportDeclaration := func(node *ast.Node) {
			if declared(node) && node.Name() != nil && ruleContext.TypeChecker != nil && usedAtRuntime(node.Name()) {
				report(node.Name())
			}
		}
		// A directive is reported once even if several uses depend on it.
		reported := map[*ast.FileReference]bool{}
		referenceUse := func(node *ast.Node) {
			if ruleContext.TypeChecker == nil ||
				!ast.IsInExpressionContext(node) ||
				ast.IsDeclarationNameOrImportPropertyName(node) ||
				ast.IsPartOfTypeNode(node) {
				return
			}
			symbol := ruleContext.TypeChecker.GetSymbolAtLocation(node)
			if symbol == nil {
				return
			}
			for _, declaration := range symbol.Declarations {
				source := ast.GetSourceFileOfNode(declaration)
				if source == nil || !source.IsDeclarationFile {
					continue
				}
				sourceName := filepath.ToSlash(source.FileName().AsString())
				matches := func(reference *ast.FileReference, kind string) bool {
					switch kind {
					case "path":
						return filepath.Clean(
							sourceName,
						) == filepath.Clean(filepath.Join(filepath.Dir(file.FileName().AsString()), reference.FileName))
					case "types":
						return strings.Contains(
							sourceName,
							"/node_modules/@types/"+strings.ReplaceAll(reference.FileName, "/", "__")+"/",
						)
					case "lib":
						return strings.HasSuffix(sourceName, "/lib."+strings.ToLower(reference.FileName)+".d.ts")
					}
					return false
				}
				for kind, references := range map[string][]*ast.FileReference{
					"path":  file.ReferencedFiles,
					"types": file.TypeReferenceDirectives,
					"lib":   file.LibReferenceDirectives,
				} {
					for _, reference := range references {
						if !reported[reference] && matches(reference, kind) {
							reported[reference] = true
							ruleContext.ReportRange(
								reference.TextRange,
								message("reference "+reference.FileName+" used by "+node.Text()),
							)
						}
					}
				}
			}
		}
		return rule.Listeners{
			ast.KindPropertyDeclaration: func(node *ast.Node) {
				if !declared(node) ||
					node.Name() == nil ||
					ruleContext.TypeChecker == nil ||
					node.Parent == nil ||
					!ast.IsClassLike(node.Parent) ||
					declared(node.Parent) {
					return
				}
				reason := ambientFieldReason(ruleContext, node)
				if reason != "" {
					ruleContext.ReportNode(
						node.Name(),
						rule.Message{Id: "ambientField", Description: ambientFieldText.Render(map[string]string{
							"declaration": node.Symbol().Name,
							"type": ruleContext.TypeChecker.TypeToString(
								ruleContext.TypeChecker.GetTypeAtLocation(node),
							),
							"reason": reason,
						})},
					)
				}
			},
			ast.KindVariableDeclaration: reportDeclaration,
			ast.KindFunctionDeclaration: reportDeclaration,
			ast.KindClassDeclaration:    reportDeclaration,
			ast.KindModuleDeclaration: func(node *ast.Node) {
				// String-named modules and `global` augment the host's declarations.
				if declared(node) &&
					node.Name() != nil &&
					(node.Name().Kind == ast.KindStringLiteral || node.Name().Text() == "global") {
					report(node.Name())
				}
			},
			ast.KindIdentifier: referenceUse,
		}
	},
}

func ambientFieldReason(ruleContext rule.Context, field *ast.Node) string {
	class := field.Parent
	if class.Symbol() == nil || field.Symbol() == nil {
		return ""
	}
	classType := ruleContext.TypeChecker.GetDeclaredTypeOfSymbol(class.Symbol())
	fieldType := ruleContext.TypeChecker.GetTypeAtLocation(field)
	for _, baseType := range ruleContext.TypeChecker.GetBaseTypes(classType) {
		lookup := baseType
		if ast.IsStatic(field) && baseType.Symbol() != nil {
			lookup = ruleContext.TypeChecker.GetTypeOfSymbol(baseType.Symbol())
		}
		base := ruleContext.TypeChecker.GetPropertyOfType(lookup, field.Symbol().Name)
		if base == nil {
			continue
		}
		baseFieldType := ruleContext.TypeChecker.GetTypeOfSymbolAtLocation(base, field)
		if checker.Checker_isTypeIdenticalTo(ruleContext.TypeChecker, fieldType, baseFieldType) {
			return ""
		}
		return "the inherited runtime field has type " +
			ruleContext.TypeChecker.TypeToString(baseFieldType) +
			", which the declaration narrows without defining that narrower value"
	}
	if ambientClassAssignsField(class, field) {
		return ""
	}
	return "no base class provides the property and no assignment in this class supplies its runtime value"
}

func ambientClassAssignsField(class, field *ast.Node) bool {
	var walk func(*ast.Node) bool
	walk = func(node *ast.Node) bool {
		if node == nil || ast.IsClassLike(node) || (ast.IsFunctionLike(node) && node.Kind != ast.KindArrowFunction) {
			return false
		}
		if ast.IsAssignmentExpression(node, false) {
			left := ast.SkipParentheses(node.AsBinaryExpression().Left)
			if left.Kind == ast.KindPropertyAccessExpression {
				access := left.AsPropertyAccessExpression()
				if access.Expression.Kind == ast.KindThisKeyword && access.Name().Text() == field.Symbol().Name {
					return true
				}
			}
			if left.Kind == ast.KindElementAccessExpression {
				access := left.AsElementAccessExpression()
				if access.Expression.Kind == ast.KindThisKeyword &&
					access.ArgumentExpression.Kind == ast.KindStringLiteral &&
					access.ArgumentExpression.Text() == field.Symbol().Name {
					return true
				}
			}
		}
		return node.ForEachChild(walk)
	}
	for _, member := range class.Members() {
		if ast.IsStatic(member) != ast.IsStatic(field) {
			continue
		}
		if ast.IsFunctionLike(member) {
			if walk(member.Body()) {
				return true
			}
		} else if walk(member) {
			return true
		}
	}
	return false
}

func ambientCanonicalSymbol(ruleContext rule.Context, symbol *ast.Symbol) *ast.Symbol {
	if symbol == nil {
		return nil
	}
	resolved := symbol
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		resolved = ruleContext.TypeChecker.GetAliasedSymbol(symbol)
	}
	if resolved != nil && resolved.ExportSymbol != nil {
		return resolved.ExportSymbol
	}
	return resolved
}

// Scan real expression uses across the program, including import aliases. Skip
// erased subtrees as a whole: a computed key in an interface is syntactically an
// expression but is not evaluated. Declaration names and type queries are not
// reads, and an ambient class's extends clause is likewise erased.
func ambientRuntimeUses(ruleContext rule.Context) map[*ast.Symbol]bool {
	uses := map[*ast.Symbol]bool{}
	if ruleContext.TypeChecker == nil {
		return uses
	}
	visit := func(file *ast.SourceFile) {
		if file == nil || file.IsDeclarationFile {
			return
		}
		var walk func(*ast.Node) bool
		walk = func(node *ast.Node) bool {
			if (ast.IsTypeNode(node) &&
				(node.Kind != ast.KindExpressionWithTypeArguments || ast.IsPartOfTypeNode(node))) ||
				node.Kind == ast.KindInterfaceDeclaration ||
				node.Kind == ast.KindTypeAliasDeclaration ||
				node.Kind == ast.KindTypeParameter ||
				ast.GetCombinedModifierFlags(node)&ast.ModifierFlagsAmbient != 0 {
				return false
			}
			if node.Kind == ast.KindIdentifier &&
				!ast.IsDeclarationNameOrImportPropertyName(node) &&
				ast.IsInExpressionContext(node) {
				uses[ambientCanonicalSymbol(ruleContext, ruleContext.TypeChecker.GetSymbolAtLocation(node))] = true
			}
			if node.Kind == ast.KindImportSpecifier && !node.AsImportSpecifier().IsTypeOnly {
				clause := node.Parent.Parent
				if !clause.AsImportClause().IsTypeOnly() {
					uses[ambientCanonicalSymbol(
						ruleContext,
						ruleContext.TypeChecker.GetSymbolAtLocation(node.Name()),
					)] = true
				}
			}
			if node.Kind == ast.KindImportClause && !node.AsImportClause().IsTypeOnly() && node.Name() != nil {
				uses[ambientCanonicalSymbol(
					ruleContext,
					ruleContext.TypeChecker.GetSymbolAtLocation(node.Name()),
				)] = true
			}
			if node.Kind == ast.KindExportSpecifier &&
				!node.AsExportSpecifier().IsTypeOnly &&
				!node.Parent.Parent.AsExportDeclaration().IsTypeOnly {
				uses[ambientCanonicalSymbol(
					ruleContext,
					ruleContext.TypeChecker.GetExportSpecifierLocalTargetSymbol(node),
				)] = true
			}
			if node.Kind == ast.KindShorthandPropertyAssignment {
				uses[ambientCanonicalSymbol(
					ruleContext,
					ruleContext.TypeChecker.GetShorthandAssignmentValueSymbol(node),
				)] = true
			}
			node.ForEachChild(walk)
			return false
		}
		walk(file.AsNode())
	}
	if ruleContext.Program != nil {
		for _, file := range ruleContext.Program.SourceFiles() {
			visit(file)
		}
	} else {
		visit(ruleContext.SourceFile)
	}
	return uses
}
