package rule

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

type pragmaCoverage struct {
	root   *ast.Node
	inside bool
}
type pragmaBindingReader struct {
	home     *ast.SourceFile
	coverage map[*ast.Node]pragmaCoverage
}

// IsReactPragmaBinding recognizes the existing React pragma import/initializer forms, without following
// aliases. A foreign initializer is allowed when the syntax actually inspected is retained by
// SignatureEntry.Syntax; silently dropping foreign declarations would change cold findings.
//
// The identifier must belong to home. Before each declaration/header/text read, the reader checks the
// exact shared body-cut definition, including enclosing cuts. A violation panics:
// the walk reports it and refuses caching instead of quietly serving a stale or missing finding.
// Only a bool leaves the reader. Its per-file cache memoizes ancestor coverage, so every ancestor is
// examined once across all lookups; symbol answers are memoized too. Recognition is linear in the
// distinct declarations and header nodes read across a file walk.
func IsReactPragmaBinding(
	home *ast.SourceFile,
	typeChecker *checker.Checker,
	cache *FileCache,
	identifier *ast.Node,
) bool {
	reader := pragmaBindingReader{home: home, coverage: Cached(
		cache,
		"rule.IsReactPragmaBinding.coverage",
		func() map[*ast.Node]pragmaCoverage { return map[*ast.Node]pragmaCoverage{} },
	)}
	if home == nil || identifier == nil || reader.cover(identifier).root != home.AsNode() {
		// Invariant: every caller hands an identifier from the file its rule is walking, which is the only
		// file the findings cache keys this answer on.
		panic("rule.IsReactPragmaBinding requires an identifier in the rule's own file")
	}
	if typeChecker == nil {
		return false
	}
	symbol := typeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return false
	}
	answers := Cached(
		cache,
		"rule.IsReactPragmaBinding.answers",
		func() map[*ast.Symbol]bool { return map[*ast.Symbol]bool{} },
	)
	if answer, found := answers[symbol]; found {
		return answer
	}
	for _, declaration := range symbol.Declarations {
		if reader.declaration(declaration) {
			answers[symbol] = true
			return true
		}
	}
	answers[symbol] = false
	return false
}

// cover follows only ancestry and cut metadata. No semantic answer is extracted until requireHeader
// succeeds. Memoization bounds both deep namespace chains and merged symbols to linear work.
func (reader *pragmaBindingReader) cover(node *ast.Node) pragmaCoverage {
	if result, ok := reader.coverage[node]; ok {
		return result
	}
	result := pragmaCoverage{}
	if node.Parent == nil {
		if node.Kind != ast.KindSourceFile {
			// Invariant: a parsed node's parent chain always ends at its source file, so a root of another
			// kind means the tree was built by hand or torn apart.
			panic("pragma declaration has no source file")
		}
		result.root = node
	} else {
		result = reader.cover(node.Parent)
		if start, end, cut := ElidedBodyRange(node.Parent); cut && node.Pos() >= start && node.Pos() < end {
			result.inside = true
		}
	}
	reader.coverage[node] = result
	return result
}

// The recognizer reads node kinds, relationships and identifier/string token values, never complete
// node text, trivia or offsets as a verdict. A node containing a nested body is not itself a body read;
// every child actually inspected is checked separately before its kind or token value is used.
func (reader *pragmaBindingReader) requireHeader(node *ast.Node) {
	if node == nil {
		return
	}
	coverage := reader.cover(node)
	if coverage.root != reader.home.AsNode() && coverage.inside {
		// Invariant: foreign syntax is read only outside a SignatureEntry body cut, the part the fingerprint
		// covers; a read inside one would serve a stale finding, so the walk reports it and refuses caching.
		panic(fmt.Sprintf(
			"rule.IsReactPragmaBinding read foreign syntax inside a SignatureEntry body cut at %d",
			node.Pos(),
		))
	}
}

func (reader *pragmaBindingReader) declaration(node *ast.Node) bool {
	reader.requireHeader(node)
	switch node.Kind {
	case ast.KindImportSpecifier:
		named := node.Parent
		reader.requireHeader(named)
		if named == nil || named.Kind != ast.KindNamedImports {
			return false
		}
		clause := named.Parent
		reader.requireHeader(clause)
		if clause == nil || clause.Kind != ast.KindImportClause {
			return false
		}
		declaration := clause.Parent
		reader.requireHeader(declaration)
		if declaration == nil || declaration.Kind != ast.KindImportDeclaration {
			return false
		}
		module := declaration.AsImportDeclaration().ModuleSpecifier
		reader.requireHeader(module)
		return module != nil && module.Kind == ast.KindStringLiteral && module.Text() == "react"
	case ast.KindBindingElement:
		pattern := node.Parent
		reader.requireHeader(pattern)
		if pattern == nil || pattern.Kind != ast.KindObjectBindingPattern {
			return false
		}
		variable := pattern.Parent
		reader.requireHeader(variable)
		if variable == nil || variable.Kind != ast.KindVariableDeclaration {
			return false
		}
		return reader.initializer(variable.AsVariableDeclaration().Initializer)
	case ast.KindVariableDeclaration:
		return reader.initializer(node.AsVariableDeclaration().Initializer)
	}
	return false
}

func (reader *pragmaBindingReader) initializer(node *ast.Node) bool {
	if node == nil {
		return false
	}
	reader.requireHeader(node)
	switch node.Kind {
	case ast.KindIdentifier:
		return node.Text() == "React"
	case ast.KindPropertyAccessExpression:
		object := node.AsPropertyAccessExpression().Expression
		reader.requireHeader(object)
		if object == nil {
			return false
		}
		if object.Kind == ast.KindIdentifier && object.Text() == "React" {
			return true
		}
		return reader.requireCall(object)
	case ast.KindCallExpression:
		return reader.requireCall(node)
	}
	return false
}

func (reader *pragmaBindingReader) requireCall(node *ast.Node) bool {
	reader.requireHeader(node)
	if node.Kind != ast.KindCallExpression {
		return false
	}
	call := node.AsCallExpression()
	callee := call.Expression
	reader.requireHeader(callee)
	if callee == nil || callee.Kind != ast.KindIdentifier || callee.Text() != "require" {
		return false
	}
	if call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
		return false
	}
	first := call.Arguments.Nodes[0]
	reader.requireHeader(first)
	return first.Kind == ast.KindStringLiteral && first.Text() == "react"
}
