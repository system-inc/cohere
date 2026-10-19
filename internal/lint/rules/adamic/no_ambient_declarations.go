package adamic

import (
	"path"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tsoptions"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var ambientDeclarationText = policy.MessageOf("adamic/no-ambient-declarations", "ambientDeclaration")

// NoAmbientDeclarations refuses runtime declarations without definitions (#4emavvv).
// The strict tsc/Node probes are in testdata/ambient-declarations. Declaration files
// describe an external runtime and are outside this rule. Class field annotations
// do not introduce a binding; an inherited, defined field is the quiet Node control.
// An unused reference does not promise a runtime value, so only a reference whose
// declaration is actually read is reported. Type-only references stay quiet too.
var NoAmbientDeclarations = rule.Rule{
	Name:             "adamic/no-ambient-declarations",
	NeedsTypeChecker: true,
	ProgramReads:     rule.ReadsOtherFiles | rule.ReadsCompilerOptions | rule.ReadsDefaultLibrary,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		file := ctx.SourceFile
		if file == nil || file.IsDeclarationFile {
			return nil
		}
		name := file.FileName().AsString()
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") && !strings.HasSuffix(name, ".a") {
			return nil
		}
		message := func(name string) rule.Message {
			return rule.Message{Id: "ambientDeclaration", Description: ambientDeclarationText.Render(map[string]string{"declaration": name})}
		}
		report := func(node *ast.Node) {
			ctx.ReportNode(node, message(text.TrimWhitespace(file.Text()[node.Pos():node.End()])))
		}
		ambient := func(node *ast.Node) {
			if ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) {
				name := node.Name()
				if name == nil {
					name = node
				}
				report(name)
			}
		}
		// Attribute a read through any transitive reference to this file's directive.
		targets := map[*ast.SourceFile][]*ast.FileReference{}
		baseline := map[*ast.SourceFile]bool{}
		if ctx.Program != nil && len(file.ReferencedFiles)+len(file.TypeReferenceDirectives)+len(file.LibReferenceDirectives) > 0 {
			// A directive that repeats the configured library adds no runtime promise.
			options := ctx.Program.Options()
			libraries := options.Lib
			if libraries == nil && !options.NoLib.IsTrue() {
				libraries = []string{tsoptions.GetDefaultLibFileName(options)}
			}
			baseNames := map[string]bool{}
			for _, library := range libraries {
				if name, ok := tsoptions.GetLibFileName(library); ok {
					baseNames[name] = true
				} else {
					baseNames[library] = true
				}
			}
			var markBaseline func(*ast.SourceFile)
			markBaseline = func(target *ast.SourceFile) {
				if baseline[target] {
					return
				}
				baseline[target] = true
				for _, child := range ctx.Program.ReferenceTargets(target) {
					markBaseline(child)
				}
			}
			for _, candidate := range ctx.Program.SourceFiles() {
				if ctx.Program.IsSourceFileDefaultLibrary(candidate.PathKey()) && baseNames[path.Base(candidate.FileName().AsString())] {
					markBaseline(candidate)
				}
			}
			for ref, target := range ctx.Program.ReferenceTargets(file) {
				visited := map[*ast.SourceFile]bool{}
				var follow func(*ast.SourceFile)
				follow = func(target *ast.SourceFile) {
					if visited[target] {
						return
					}
					visited[target] = true
					targets[target] = append(targets[target], ref)
					for _, child := range ctx.Program.ReferenceTargets(target) {
						follow(child)
					}
				}
				follow(target)
			}
		}
		reported := map[*ast.FileReference]bool{}
		return rule.Listeners{
			ast.KindVariableStatement: func(node *ast.Node) {
				if !ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) {
					return
				}
				for _, declaration := range node.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
					report(declaration.Name())
				}
			},
			ast.KindFunctionDeclaration: ambient,
			ast.KindClassDeclaration:    ambient,
			ast.KindModuleDeclaration: func(node *ast.Node) {
				if ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) && (ast.IsGlobalScopeAugmentation(node) || ast.IsStringLiteral(node.Name())) {
					report(node.Name())
				}
			},
			ast.KindIdentifier: func(node *ast.Node) {
				if ctx.TypeChecker == nil || len(targets) == 0 || ast.IsPartOfTypeNode(node) || ast.IsPartOfTypeQuery(node) {
					return
				}
				symbol := ctx.TypeChecker.GetSymbolAtLocation(node)
				if symbol == nil || symbol.Flags&ast.SymbolFlagsValue == 0 {
					return
				}
				for _, declaration := range symbol.Declarations {
					declaredFile := ast.GetSourceFileOfNode(declaration)
					if declaredFile == nil || !declaredFile.IsDeclarationFile || baseline[declaredFile] {
						continue
					}
					for _, ref := range targets[declaredFile] {
						if !reported[ref] {
							reported[ref] = true
							ctx.ReportRange(ref.TextRange, message(ref.FileName))
						}
					}
				}
			},
		}
	},
}
