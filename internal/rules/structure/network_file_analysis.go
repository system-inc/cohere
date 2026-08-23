package structure

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// Per-file analysis of the NetworkService hooks a file declares.
//
// Three rules need the same two facts, "does this file import NetworkService" and "which of its
// hooks call it", and each of the three then judges those hooks differently: one on the name, one on
// the parameters, one on the type arguments. Ported as its own unit rather than inside whichever
// rule reaches it first, because a helper written inside its first consumer is shaped for that
// consumer and the next two get something that almost fits.
//
// The original memoizes on a module-level WeakMap keyed by the Program node. verify has the same
// mechanism as a first-class thing: rule.Cached on the shared per-file cache, which every rule in a
// run shares, so the walk happens once per file rather than once per rule per file.

// NetworkServiceHookMethods are the NetworkService methods that make a hook a network hook.
//
// A closed set rather than a prefix test. `networkService.somethingElse()` is a call on the same
// object that does not make the enclosing function a network hook, and the three rules built on this
// all impose naming and shape requirements that would be wrong to apply on that basis.
var NetworkServiceHookMethods = map[string]bool{
	"useGraphQlQuery":         true,
	"useGraphQlMutation":      true,
	"graphQlRequest":          true,
	"useSuspenseGraphQlQuery": true,
}

// NetworkHookDeclaration is one hook that calls NetworkService.
type NetworkHookDeclaration struct {
	Name string

	// NameNode is the identifier, which is what a finding points at. The function node is the
	// wrong place to report: a reader looking at a naming complaint wants the name underlined.
	NameNode *ast.Node

	// FunctionNode is the arrow, function expression, or declaration itself.
	FunctionNode *ast.Node

	Body       *ast.Node
	Parameters []*ast.Node
	IsExported bool
}

// NetworkFileAnalysis summarizes what one file does with NetworkService.
type NetworkFileAnalysis struct {
	HasNetworkServiceImport bool
	HookDeclarations        []NetworkHookDeclaration
}

// NetworkFileAnalysisFor returns the analysis for the file being linted, computing it at most once
// per file across every rule that asks.
func NetworkFileAnalysisFor(ctx rule.Context) *NetworkFileAnalysis {
	return rule.Cached(ctx.FileCache, "structure.networkFileAnalysis", func() *NetworkFileAnalysis {
		analysis := &NetworkFileAnalysis{}
		if ctx.SourceFile == nil {
			return analysis
		}
		analysis.collect(ctx.SourceFile.AsNode())

		// Without the import, no hook in the file can be calling NetworkService, and the original
		// clears the list rather than returning hooks nobody should judge. Reproduced: every rule
		// built on this early-exits on the import, so a populated list with the flag false would be
		// a shape none of them expect.
		if !analysis.HasNetworkServiceImport {
			analysis.HookDeclarations = nil
		}
		return analysis
	})
}

// collect walks the file's top-level statements only.
//
// Top-level is the original's scope and it is a real limit rather than an oversight: a hook nested
// inside another function is not the file's exported surface, and these rules are about what a file
// offers its importers.
func (analysis *NetworkFileAnalysis) collect(sourceFile *ast.Node) {
	sourceFile.ForEachChild(func(statement *ast.Node) bool {
		switch statement.Kind {
		case ast.KindImportDeclaration:
			declaration := statement.AsImportDeclaration()
			if declaration.ModuleSpecifier != nil && ast.IsStringLiteralLike(declaration.ModuleSpecifier) {
				if strings.Contains(declaration.ModuleSpecifier.Text(), "NetworkService") {
					analysis.HasNetworkServiceImport = true
				}
			}

		case ast.KindFunctionDeclaration:
			analysis.collectFunctionDeclaration(statement)

		case ast.KindVariableStatement:
			analysis.collectVariableStatement(statement)
		}
		return false
	})
}

// collectFunctionDeclaration handles `function useThing() {}` and its exported form.
func (analysis *NetworkFileAnalysis) collectFunctionDeclaration(statement *ast.Node) {
	declaration := statement.AsFunctionDeclaration()
	name := declaration.Name()
	if name == nil || !strings.HasPrefix(name.Text(), "use") || declaration.Body == nil {
		return
	}
	if !bodyCallsNetworkService(declaration.Body) {
		return
	}

	analysis.HookDeclarations = append(analysis.HookDeclarations, NetworkHookDeclaration{
		Name:         name.Text(),
		NameNode:     name,
		FunctionNode: statement,
		Body:         declaration.Body,
		Parameters:   parameterNodes(declaration.Parameters),
		IsExported:   isExportedStatement(statement),
	})
}

// collectVariableStatement handles `const useThing = () => {}` and its exported form.
func (analysis *NetworkFileAnalysis) collectVariableStatement(statement *ast.Node) {
	declarationList := statement.AsVariableStatement().DeclarationList
	if declarationList == nil {
		return
	}
	isExported := isExportedStatement(statement)

	for _, declarationNode := range declarationList.AsVariableDeclarationList().Declarations.Nodes {
		declaration := declarationNode.AsVariableDeclaration()
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier || !strings.HasPrefix(name.Text(), "use") {
			continue
		}

		initializer := declaration.Initializer
		if initializer == nil {
			continue
		}
		var body *ast.Node
		var parameters *ast.NodeList
		switch initializer.Kind {
		case ast.KindArrowFunction:
			arrow := initializer.AsArrowFunction()
			body, parameters = arrow.Body, arrow.Parameters
		case ast.KindFunctionExpression:
			function := initializer.AsFunctionExpression()
			body, parameters = function.Body, function.Parameters
		default:
			continue
		}
		if body == nil || !bodyCallsNetworkService(body) {
			continue
		}

		analysis.HookDeclarations = append(analysis.HookDeclarations, NetworkHookDeclaration{
			Name:         name.Text(),
			NameNode:     name,
			FunctionNode: initializer,
			Body:         body,
			Parameters:   parameterNodes(parameters),
			IsExported:   isExported,
		})
	}
}

// parameterNodes flattens a parameter list, tolerating a nil one.
func parameterNodes(parameters *ast.NodeList) []*ast.Node {
	if parameters == nil {
		return nil
	}
	return parameters.Nodes
}

// networkServiceCallDepthLimit bounds the search, matching the original's limit of 40.
//
// Reproduced rather than dropped for the same reason as the JSX walk's limit of 20: the bound is
// behavior. A NetworkService call buried deeper than this does not make the enclosing function a
// hook to the gate, and an unbounded walk would find it and produce findings the gate does not have.
const networkServiceCallDepthLimit = 40

// bodyCallsNetworkService reports a `networkService.<hookMethod>(...)` anywhere in a function body.
//
// Unlike the JSX walk, this one does not stop at nested function boundaries, matching the original:
// a call inside a callback inside the hook is still the hook using NetworkService, since the hook is
// what the caller sees.
func bodyCallsNetworkService(body *ast.Node) bool {
	found := false

	var visit func(*ast.Node, int)
	visit = func(current *ast.Node, depth int) {
		if current == nil || found || depth > networkServiceCallDepthLimit {
			return
		}
		if current.Kind == ast.KindCallExpression && isNetworkServiceHookCall(current) {
			found = true
			return
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child, depth+1)
			return found
		})
	}
	visit(body, 0)
	return found
}

// isNetworkServiceHookCall reports a call on the `networkService` binding to one of the hook methods.
//
// The receiver is matched by name rather than resolved, which is the original's approach and its
// known limit: a local binding shadowing `networkService` would be read as the real one. Matching
// the name is what makes this work without type information.
func isNetworkServiceHookCall(node *ast.Node) bool {
	callee := ast.SkipParentheses(node.AsCallExpression().Expression)
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return false
	}

	access := callee.AsPropertyAccessExpression()
	receiver := ast.SkipParentheses(access.Expression)
	if receiver == nil || receiver.Kind != ast.KindIdentifier || receiver.Text() != "networkService" {
		return false
	}

	method := access.Name()
	return method != nil && NetworkServiceHookMethods[method.Text()]
}
