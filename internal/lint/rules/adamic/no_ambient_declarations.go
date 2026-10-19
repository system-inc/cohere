package adamic

import (
	"path/filepath"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var ambientDeclarationText = policy.MessageOf("adamic/no-ambient-declarations", "ambientDeclaration")

// NoAmbientDeclarations refuses runtime claims with no emitted definition. Each
// form has a strict-accepted, Node-failing probe in testdata/ambient. References
// alone are not runtime claims: only a value use from the referenced declarations
// reports. Declaration files belong to the host's contract, outside this rule.
// Class property annotations are not ambient bindings: the constructor-assigned
// and inherited-field probes have runtime definitions and must stay quiet. This
// rule does not attempt to prove initialization of arbitrary declared properties.
var NoAmbientDeclarations = rule.Rule{
	Name:             "adamic/no-ambient-declarations",
	NeedsTypeChecker: true,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		file := ctx.SourceFile
		if file == nil || file.IsDeclarationFile {
			return nil
		}
		switch strings.ToLower(filepath.Ext(file.FileName().AsString())) {
		case ".ts", ".tsx", ".a":
		default:
			return nil
		}
		message := func(name string) rule.Message {
			return rule.Message{Id: "ambientDeclaration", Description: ambientDeclarationText.Render(map[string]string{"declaration": name})}
		}
		report := func(node *ast.Node) { ctx.ReportNode(node, message(node.Text())) }
		declared := func(node *ast.Node) bool { return ast.GetCombinedModifierFlags(node)&ast.ModifierFlagsAmbient != 0 }
		reportDeclaration := func(node *ast.Node) {
			if declared(node) && node.Name() != nil {
				report(node.Name())
			}
		}
		// A directive is reported once even if several uses depend on it.
		reported := map[*ast.FileReference]bool{}
		referenceUse := func(node *ast.Node) {
			if ctx.TypeChecker == nil || !ast.IsInExpressionContext(node) || ast.IsDeclarationNameOrImportPropertyName(node) || ast.IsPartOfTypeNode(node) {
				return
			}
			symbol := ctx.TypeChecker.GetSymbolAtLocation(node)
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
						return filepath.Clean(sourceName) == filepath.Clean(filepath.Join(filepath.Dir(file.FileName().AsString()), reference.FileName))
					case "types":
						return strings.Contains(sourceName, "/node_modules/@types/"+strings.ReplaceAll(reference.FileName, "/", "__")+"/")
					case "lib":
						return strings.HasSuffix(sourceName, "/lib."+strings.ToLower(reference.FileName)+".d.ts")
					}
					return false
				}
				for kind, references := range map[string][]*ast.FileReference{"path": file.ReferencedFiles, "types": file.TypeReferenceDirectives, "lib": file.LibReferenceDirectives} {
					for _, reference := range references {
						if !reported[reference] && matches(reference, kind) {
							reported[reference] = true
							ctx.ReportRange(reference.TextRange, message("reference "+reference.FileName+" used by "+node.Text()))
						}
					}
				}
			}
		}
		return rule.Listeners{
			ast.KindVariableDeclaration: reportDeclaration,
			ast.KindFunctionDeclaration: reportDeclaration,
			ast.KindClassDeclaration:    reportDeclaration,
			ast.KindModuleDeclaration: func(node *ast.Node) {
				// String-named modules and `global` augment the host's declarations.
				if declared(node) && node.Name() != nil && (node.Name().Kind == ast.KindStringLiteral || node.Name().Text() == "global") {
					report(node.Name())
				}
			},
			ast.KindIdentifier: referenceUse,
		}
	},
}
