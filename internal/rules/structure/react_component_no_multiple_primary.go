package structure

import (
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/ecmascript/module"
	"github.com/system-inc/verify/internal/utils/react"
)

// ReactComponentNoMultiplePrimaryOptions tunes where the thresholds sit.
//
// MaximumComponentLines is the size past which a component is "primary" and its file should hold
// only it. MaximumHelperLines is the size under which a component is small enough to keep it
// company. The band between them is the interesting one: a component too big to be a helper and too
// small to be primary still counts against the file.
type ReactComponentNoMultiplePrimaryOptions struct {
	MaximumComponentLines int
	MaximumHelperLines    int
}

const (
	defaultMaximumComponentLines = 60
	defaultMaximumHelperLines    = 10

	// helperCompanyLimit is how many small components a large one may keep before the file is
	// carrying a crowd. Four is the first count that reports, since the slice starts at index 3.
	helperCompanyLimit = 3
)

func messageNoMultiplePrimary(primaryLineCount int, maximumComponentLines int, lineCount int) rule.Message {
	return rule.Message{
		Id: "noMultiplePrimary",
		Description: "File contains a component with " + strconv.Itoa(primaryLineCount) +
			" lines. When any component exceeds " + strconv.Itoa(maximumComponentLines) +
			" lines, the file should contain only one primary component. Consider moving this " +
			strconv.Itoa(lineCount) + "-line component to its own file.",
	}
}

var messageTooManyHelpers = rule.Message{
	Id: "tooManyHelpers",
	Description: "Too many helper components in a file with a large primary component. Consider " +
		"moving some helpers to separate files or combining them.",
}

// trackedComponent is one component the file declares, in source order.
type trackedComponent struct {
	node      *ast.Node
	lineCount int
}

// ReactComponentNoMultiplePrimary keeps a file from holding more than one substantial component.
//
//	valid:   one 80-line component, alone in its file
//	valid:   one 80-line component with three small helpers beside it
//	invalid: an 80-line component and a 46-line component in one file
//
// A file is the unit a reader opens, and a name in an import is the promise of what they will find.
// Two substantial components in one file break that promise in both directions: the file's name
// describes one of them, and the other is reachable only by someone who already knows it is there.
// Small helpers are different, because a helper read beside its only caller is easier than a helper
// read in a file of its own.
//
// The rule reports every component after the first, rather than reporting the file once. That is
// deliberate: the finding is attached to the component that should move, so the reader sees it at
// the thing they would edit rather than at the top of the file.
//
// No fix. Moving a component to its own file means creating that file, choosing its name, moving
// the imports it needs, and updating every reference. That is a refactor, not an edit.
var ReactComponentNoMultiplePrimary = rule.Rule{
	Name: "react-component-no-multiple-primary",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		// JSX is what makes a component file, and a Next.js special file's shape is the framework's
		// contract rather than an authoring choice. Both are decided from the path, so the file is
		// declined before a node is visited.
		fileContext := FileContextFor(ctx.SourceFile.FileName())
		if !fileContext.IsReactFile || fileContext.IsSpecialNextJsFile {
			return nil
		}

		maximumComponentLines, maximumHelperLines := defaultMaximumComponentLines, defaultMaximumHelperLines
		if settings, hasSettings := options.(ReactComponentNoMultiplePrimaryOptions); hasSettings {
			if settings.MaximumComponentLines > 0 {
				maximumComponentLines = settings.MaximumComponentLines
			}
			if settings.MaximumHelperLines > 0 {
				maximumHelperLines = settings.MaximumHelperLines
			}
		}

		// The whole analysis runs in one pass over the file rather than in per-kind listeners.
		//
		// The original does its work in Program:exit, because the verdict is about the file as a
		// whole and cannot be reached until every component is known. This walk is pre-order, so a
		// source-file listener fires before its children and would see an empty list. Collecting
		// inside the source-file listener puts the gathering and the verdict in the same place,
		// which is what the original's exit hook was buying.
		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				components := collectComponents(node)
				if len(components) <= 1 {
					return
				}
				reportComponents(ctx, components, maximumComponentLines, maximumHelperLines)
			},
		}
	},
}

// collectComponents finds every component the file declares, in source order.
//
// Only the three declaration shapes the original listens for count: a named function declaration, an
// exported const holding a function literal, and a default export of a function. A component reached
// any other way is not counted, here or there.
func collectComponents(sourceFile *ast.Node) []trackedComponent {
	components := []trackedComponent{}
	add := func(node *ast.Node) {
		components = append(components, trackedComponent{node: node})
	}

	// The walk is recursive, not a scan of top-level statements.
	//
	// The original is an ESLint visitor, so its FunctionDeclaration handler fires at any depth: a
	// component declared inside another component still counts, and four of them are real findings
	// on this tree. Only the function-declaration case recurses that way; an exported const and a
	// default export are statements by construction and cannot be nested.
	var visit func(*ast.Node)
	visit = func(statement *ast.Node) {
		switch statement.Kind {
		case ast.KindFunctionDeclaration:
			declaration := statement.AsFunctionDeclaration()
			name := declaration.Name()

			// An unnamed default export is still a component; the original names it DefaultExport
			// rather than skipping it.
			if name == nil {
				if module.IsDefaultExported(statement) && HasJsxOrReactHookCalls(statement) {
					add(statement)
				}
			} else if react.IsLikelyComponentName(name.Text()) && HasJsxOrReactHookCalls(statement) {
				add(statement)
			}

		case ast.KindVariableStatement:
			// Only exported ones. A file-local arrow component is a helper by construction: nothing
			// outside can reach it, so it cannot be the thing an importer came for.
			if !module.IsExported(statement) {
				break
			}
			declarationList := statement.AsVariableStatement().DeclarationList
			if declarationList == nil {
				break
			}
			for _, declarationNode := range declarationList.AsVariableDeclarationList().Declarations.Nodes {
				declaration := declarationNode.AsVariableDeclaration()
				name := declaration.Name()
				if name == nil || name.Kind != ast.KindIdentifier {
					continue
				}
				if !react.IsLikelyComponentName(name.Text()) {
					continue
				}

				// The declared value has to be a function literal. A component reached through
				// memo(...) is a call rather than a literal, and the original does not count it
				// either, so neither does this.
				initializer := declaration.Initializer
				if initializer == nil {
					continue
				}
				if initializer.Kind != ast.KindArrowFunction &&
					initializer.Kind != ast.KindFunctionExpression {
					continue
				}
				if HasJsxOrReactHookCalls(initializer) {
					add(initializer)
				}
			}

		case ast.KindExportAssignment:
			// A `export default function () {}` arrives as a function declaration and is handled
			// above. This is `export default <expression>`, where a function literal is still a
			// component and the original names it DefaultExport.
			expression := statement.AsExportAssignment().Expression
			if expression == nil {
				break
			}
			if expression.Kind != ast.KindArrowFunction &&
				expression.Kind != ast.KindFunctionExpression {
				break
			}
			if HasJsxOrReactHookCalls(expression) {
				add(expression)
			}
		}

		statement.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile)

	return components
}

// reportComponents applies the size bands and reports, which is where the rule's judgment lives.
func reportComponents(
	ctx rule.Context,
	components []trackedComponent,
	maximumComponentLines int,
	maximumHelperLines int,
) {
	sourceLines := strings.Split(ctx.SourceFile.Text(), "\n")

	large := []trackedComponent{}
	medium := []trackedComponent{}
	small := []trackedComponent{}

	for index := range components {
		components[index].lineCount = countCodeLines(ctx, sourceLines, components[index].node)
		switch component := components[index]; {
		case component.lineCount > maximumComponentLines:
			large = append(large, component)
		case component.lineCount <= maximumHelperLines:
			small = append(small, component)
		default:
			medium = append(medium, component)
		}
	}

	// A file of nothing but small components is fine however many there are, since a helper read
	// beside its caller is the point.
	if len(large) == 0 && len(medium) == 0 {
		return
	}

	// The large ones come first, so the primary is the largest-band component that appears earliest
	// rather than simply the first in the file. Everything else in the non-small bands is a
	// component that should move.
	nonSmall := append(append([]trackedComponent{}, large...), medium...)
	if len(nonSmall) > 1 {
		primary := nonSmall[0]
		for _, component := range nonSmall[1:] {
			ctx.ReportNode(component.node, messageNoMultiplePrimary(
				primary.lineCount, maximumComponentLines, component.lineCount,
			))
		}
	}

	// Only when something large is present. A crowd of helpers around a medium component is a file
	// of small parts, which is not what this half is about.
	if len(large) > 0 && len(small) > helperCompanyLimit {
		for _, component := range small[helperCompanyLimit:] {
			ctx.ReportNode(component.node, messageTooManyHelpers)
		}
	}
}

// countCodeLines counts the lines of a node that carry code.
//
// Blank lines and comment lines do not count, which is what keeps a well-documented component from
// reading as a large one. The test is the original's exactly, including its looseness: a line whose
// trimmed text begins with "//" or "*" is a comment. That misses a block comment's opening "/*" and
// counts it as code, and it treats a line beginning with a multiplication as a comment. Both are
// reproduced rather than fixed, because the 128 findings on the tree were produced by this
// arithmetic and a better count would disagree with them.
func countCodeLines(ctx rule.Context, sourceLines []string, node *ast.Node) int {
	// GetTokenPosOfNode rather than Pos(): a node's Pos() is where its leading trivia begins, not
	// where its first token does. Counting from it folds the comment block above a component into
	// the component, which reads several lines larger than it is. That is not a cosmetic drift, it
	// moves a component across a band boundary: two documented ten-line helpers measured 12 and 14,
	// crossed maximumHelperLines, and were reported as medium components the gate does not report.
	startLine, _ := scanner.GetECMALineAndByteOffsetOfPosition(
		ctx.SourceFile, scanner.GetTokenPosOfNode(node, ctx.SourceFile, false),
	)
	endLine, _ := scanner.GetECMALineAndByteOffsetOfPosition(ctx.SourceFile, node.End())

	codeLines := 0
	for lineNumber := startLine; lineNumber <= endLine && lineNumber < len(sourceLines); lineNumber++ {
		line := strings.TrimSpace(sourceLines[lineNumber])
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
			continue
		}
		codeLines++
	}
	return codeLines
}
