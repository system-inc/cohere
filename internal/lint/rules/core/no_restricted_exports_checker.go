package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noRestrictedExportsChecker carries the resolved configuration across the listeners.
type noRestrictedExportsChecker struct {
	ctx        rule.Context
	restricted map[string]bool
	pattern    *esregexp.RegExp
	defaults   *NoRestrictedExportsRestrictDefaultExports
}

// checkDeclarationStatement handles a declaration that carries its own export modifier.
//
// This is upstream's `ExportNamedDeclaration` arm for the case where `node.declaration` is set, plus
// its `ExportDefaultDeclaration` arm for the declaration forms, because this tree does not separate
// the two: `export default function foo() {}` is a function declaration with two modifiers on it,
// not a distinct node.
func (checker *noRestrictedExportsChecker) checkDeclarationStatement(node *ast.Node) {
	if !noRestrictedExportsHasModifier(node, ast.KindExportKeyword) {
		return
	}

	if noRestrictedExportsHasModifier(node, ast.KindDefaultKeyword) {
		// `export default function foo() {}`. Upstream's ExportDefaultDeclaration arm reports the
		// whole statement and never consults the name, so `restrictedNamedExports: ['foo']` is
		// clean on it while `restrictDefaultExports.direct` reports. Both measured.
		checker.reportDefaultIf(node, checker.defaults != nil && noRestrictedExportsIsOn(checker.defaults.Direct))
		return
	}

	if node.Kind == ast.KindVariableStatement {
		// Upstream maps `getDeclaredVariables` over the declaration and checks each bound name.
		// Every declarator in the list contributes, so `export const b = 1, a = 2;` checks both.
		list := node.AsVariableStatement().DeclarationList
		if list == nil {
			return
		}
		declarations := list.AsVariableDeclarationList().Declarations
		if declarations == nil {
			return
		}
		for _, declaration := range declarations.Nodes {
			name := declaration.Name()
			if name == nil {
				continue
			}
			noRestrictedExportsForEachBoundName(name, func(bound *ast.Node) {
				checker.checkExportedName(bound)
			})
		}
		return
	}

	// A function, class, interface, type alias, enum or namespace exports exactly its own name.
	if name := node.Name(); name != nil {
		checker.checkExportedName(name)
	}
}

// checkExportDeclaration handles every `export ... from`-shaped statement and the bare
// `export { ... }`.
//
// This is upstream's ExportNamedDeclaration-without-declaration arm and its ExportAllDeclaration arm
// at once, because this tree gives both the same node kind and separates them by what hangs off it:
// a `KindNamedExports` child for the specifier list, a `KindNamespaceExport` child for
// `export * as a`, and neither for the bare `export * from 'm'`.
func (checker *noRestrictedExportsChecker) checkExportDeclaration(node *ast.Node) {
	declaration := node.AsExportDeclaration()
	if declaration == nil || declaration.ExportClause == nil {
		// `export * from 'foo';` exports no name of its own. Upstream's ExportAllDeclaration arm
		// guards on `node.exported`, which is null here, so it checks nothing. The corpus tests
		// `export * from 'a'` under `restrictedNamedExports: ['a']` and it is clean: `a` there is
		// the module, not an exported name.
		return
	}

	switch declaration.ExportClause.Kind {
	case ast.KindNamespaceExport:
		// `export * as a from 'm'`. Upstream's ExportAllDeclaration arm with `node.exported` set.
		if name := declaration.ExportClause.AsNamespaceExport().Name(); name != nil {
			checker.checkExportedName(name)
		}

	case ast.KindNamedExports:
		elements := declaration.ExportClause.AsNamedExports().Elements
		if elements == nil {
			return
		}
		for _, specifier := range elements.Nodes {
			if name := specifier.Name(); name != nil {
				checker.checkExportedName(name)
			}
		}
	}
}

// checkExportAssignment handles `export default <expression>;`.
//
// `export = foo` shares this node kind and is not a default export. Upstream has no such syntax and
// therefore no opinion, so it is skipped rather than reported.
func (checker *noRestrictedExportsChecker) checkExportAssignment(node *ast.Node) {
	assignment := node.AsExportAssignment()
	if assignment == nil || assignment.IsExportEquals {
		return
	}
	checker.reportDefaultIf(node, checker.defaults != nil && noRestrictedExportsIsOn(checker.defaults.Direct))
}

// checkExportedName is upstream's `checkExportedName`, and the order of its arms decides which
// message a reader sees.
//
// A name matching the restricted list or the pattern reports `restrictedNamed` and returns, so the
// default arms below never run for it. That ordering is upstream's and it is observable: with both
// `restrictedNamedExports: ['default']` and a `restrictDefaultExports` flag configured, the named
// message wins. Upstream's schema forbids exactly that combination, which is why the decoder here
// refuses it too.
func (checker *noRestrictedExportsChecker) checkExportedName(node *ast.Node) {
	name := noRestrictedExportsNameText(node)

	// The pattern deliberately does not apply to `default`. Upstream guards with
	// `name !== "default"` before testing, so `restrictedNamedExportsPattern: 'default'` is clean on
	// every one of the five default forms -- five corpus cases turn on this, and a port that tested
	// the pattern unconditionally would report all five under the wrong message.
	matchesPattern := false
	if checker.pattern != nil && name != "default" {
		matchesPattern = checker.pattern.Test(name)
	}

	if matchesPattern || checker.restricted[name] {
		checker.ctx.ReportNode(node, noRestrictedExportsRestrictedNamedMessage(name))
		return
	}

	if name != "default" || checker.defaults == nil {
		return
	}

	parent := node.Parent
	if parent == nil {
		return
	}

	if parent.Kind == ast.KindNamespaceExport {
		// `export * as default from 'mod';`
		checker.reportDefaultIf(node, noRestrictedExportsIsOn(checker.defaults.NamespaceFrom))
		return
	}

	if parent.Kind != ast.KindExportSpecifier {
		return
	}

	// Which of the three specifier flags applies turns on two questions: whether the statement has
	// a source module, and what the local side of the specifier is. Upstream reads the local name
	// through `node.parent.local`, which for an unaliased `export { default }` is the same node as
	// the exported name.
	hasSource := noRestrictedExportsSpecifierHasSource(parent)
	localName := name
	if propertyName := parent.AsExportSpecifier().PropertyName; propertyName != nil {
		localName = noRestrictedExportsNameText(propertyName)
	}

	if !hasSource {
		// `export { foo as default };` -- a local binding re-exported as the default.
		checker.reportDefaultIf(node, noRestrictedExportsIsOn(checker.defaults.Named))
		return
	}

	if localName == "default" {
		// `export { default } from 'mod';` and `export { default as default } from 'mod';`
		checker.reportDefaultIf(node, noRestrictedExportsIsOn(checker.defaults.DefaultFrom))
		return
	}
	// `export { foo as default } from 'mod';`
	checker.reportDefaultIf(node, noRestrictedExportsIsOn(checker.defaults.NamedFrom))
}

// reportDefaultIf reports the restricted-default message when the flag that selects this form is on.
func (checker *noRestrictedExportsChecker) reportDefaultIf(node *ast.Node, restricted bool) {
	if restricted {
		checker.ctx.ReportNode(node, messageNoRestrictedExportsRestrictedDefault)
	}
}
