package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noRestrictedImportsChecker carries the compiled configuration across the listeners.
type noRestrictedImportsChecker struct {
	ctx rule.Context

	// paths is grouped by module name, because upstream groups them and a module named twice
	// reports twice. `import { bar } from 'mod'` with `mod` listed both bare and with importNames
	// produces two findings, which the corpus tests.
	paths    map[string][]NoRestrictedImportsPath
	patterns []noRestrictedImportsCompiledPattern
}

// checkImportDeclaration handles `import ... from '...'` in all its forms.
func (checker *noRestrictedImportsChecker) checkImportDeclaration(node *ast.Node) {
	declaration := node.AsImportDeclaration()
	if declaration == nil || declaration.ModuleSpecifier == nil {
		return
	}
	source, ok := noRestrictedImportsSpecifierText(declaration.ModuleSpecifier)
	if !ok {
		return
	}

	// `imports.BindingsOf` decides which of the three shapes this statement carries, which is the
	// question upstream answers by filtering one flat `specifiers` array. The three live in three
	// different places in this AST, and a rule reading only one of them pins half the forms and
	// looks complete -- a mistake this tree has made and corrected once already.
	declared := imports.BindingsOf(node)

	bindings := []noRestrictedImportsBinding{}
	if declared.Default != nil {
		bindings = append(bindings, noRestrictedImportsBinding{
			name:        "default",
			reportRange: rule.TokenRange(checker.ctx.SourceFile, declared.Default),
		})
	}
	if declared.Namespace != nil {
		bindings = append(bindings, noRestrictedImportsBinding{
			name:        "*",
			reportRange: rule.TokenRange(checker.ctx.SourceFile, declared.Namespace),
		})
	}
	for _, specifier := range declared.Named {
		bindings = append(bindings, noRestrictedImportsBinding{
			name:        noRestrictedImportsImportedName(specifier),
			reportRange: rule.TokenRange(checker.ctx.SourceFile, specifier),
			typeOnly:    specifier.AsImportSpecifier().IsTypeOnly,
		})
	}

	checker.check(node, source, bindings, noRestrictedImportsDeclarationIsTypeOnly(node))
}

// checkExportDeclaration handles a re-export, which names a module exactly as an import does.
//
// An export with no module specifier (`export { a };`) names nothing and is skipped, which is
// upstream's `if (node.source)` guard.
func (checker *noRestrictedImportsChecker) checkExportDeclaration(node *ast.Node) {
	declaration := node.AsExportDeclaration()
	if declaration == nil || declaration.ModuleSpecifier == nil {
		return
	}
	source, ok := noRestrictedImportsSpecifierText(declaration.ModuleSpecifier)
	if !ok {
		return
	}

	bindings := []noRestrictedImportsBinding{}
	switch {
	case declaration.ExportClause == nil:
		// `export * from 'm'`. Upstream reports at `sourceCode.getFirstToken(node, 1)`, the token
		// after `export`, which is the `*`.
		if star := noRestrictedImportsStarToken(checker.ctx, node); star != nil {
			bindings = append(bindings, noRestrictedImportsBinding{name: "*", reportRange: *star})
		}

	case declaration.ExportClause.Kind == ast.KindNamespaceExport:
		// `export * as ns from 'm'`. Upstream's ExportAllDeclaration arm reaches this with the same
		// `*` token, because it reads the token rather than the clause.
		if star := noRestrictedImportsStarToken(checker.ctx, node); star != nil {
			bindings = append(bindings, noRestrictedImportsBinding{name: "*", reportRange: *star})
		}

	case declaration.ExportClause.Kind == ast.KindNamedExports:
		if elements := declaration.ExportClause.AsNamedExports().Elements; elements != nil {
			for _, specifier := range elements.Nodes {
				bindings = append(bindings, noRestrictedImportsBinding{
					name:        noRestrictedImportsExportedLocalName(specifier),
					reportRange: rule.TokenRange(checker.ctx.SourceFile, specifier),
					typeOnly:    specifier.AsExportSpecifier().IsTypeOnly,
				})
			}
		}
	}

	checker.check(node, source, bindings, noRestrictedImportsExportIsTypeOnly(node))
}

// checkImportEquals handles `import foo = require('bar')`.
//
// Upstream passes an empty import-name map for this form, so only a whole-module restriction can
// report on it and an `importNames` entry never does. Ported from upstream's source rather than
// from a measured verdict: its corpus has no case for this arm.
func (checker *noRestrictedImportsChecker) checkImportEquals(node *ast.Node) {
	declaration := node.AsImportEqualsDeclaration()
	if declaration == nil || declaration.ModuleReference == nil {
		return
	}
	if declaration.ModuleReference.Kind != ast.KindExternalModuleReference {
		return
	}
	expression := declaration.ModuleReference.AsExternalModuleReference().Expression
	source, ok := noRestrictedImportsSpecifierText(expression)
	if !ok {
		return
	}
	checker.check(node, source, nil, declaration.IsTypeOnly)
}

// check is upstream's `checkNode`: the path list first, then every matching pattern group.
func (checker *noRestrictedImportsChecker) check(
	node *ast.Node,
	source string,
	bindings []noRestrictedImportsBinding,
	declarationIsTypeOnly bool,
) {
	// Upstream trims the specifier before comparing, so `import x from ' fs '` is restricted by
	// `fs`. The trim applies to both the path list and the pattern list.
	trimmedSource := text.TrimWhitespace(source)

	checker.checkPaths(node, trimmedSource, bindings, declarationIsTypeOnly)

	for _, pattern := range checker.patterns {
		if !checker.patternMatches(pattern, trimmedSource) {
			continue
		}
		checker.reportForPattern(node, pattern, trimmedSource, bindings, declarationIsTypeOnly)
	}
}

// patternMatches answers upstream's `isRestrictedPattern`.
func (checker *noRestrictedImportsChecker) patternMatches(
	pattern noRestrictedImportsCompiledPattern,
	source string,
) bool {
	if pattern.regularExpression != nil {
		return pattern.regularExpression.Test(source)
	}
	return pattern.matcher.Ignores(source)
}

// checkPaths is upstream's `checkRestrictedPathAndReport`.
func (checker *noRestrictedImportsChecker) checkPaths(
	node *ast.Node,
	source string,
	bindings []noRestrictedImportsBinding,
	declarationIsTypeOnly bool,
) {
	entries, restricted := checker.paths[source]
	if !restricted {
		return
	}

	for _, entry := range entries {
		if entry.AllowTypeImports && declarationIsTypeOnly {
			continue
		}

		if len(entry.ImportNames) == 0 && len(entry.AllowImportNames) == 0 {
			// The whole module is banned, so the finding is on the statement rather than on any
			// one binding.
			checker.report(node, noRestrictedImportsPathMessage(source, entry.Message))
			continue
		}

		for _, binding := range bindings {
			if binding.name == "*" {
				if len(entry.ImportNames) > 0 {
					checker.reportRange(binding.reportRange, noRestrictedImportsEverythingMessage(
						source, entry.ImportNames, entry.Message))
				} else if len(entry.AllowImportNames) > 0 {
					checker.reportRange(binding.reportRange,
						noRestrictedImportsEverythingWithAllowMessage(
							source, entry.AllowImportNames, entry.Message))
				}
				continue
			}

			if entry.AllowTypeImports && binding.typeOnly {
				continue
			}

			if noRestrictedImportsContains(entry.ImportNames, binding.name) {
				checker.reportRange(binding.reportRange,
					noRestrictedImportsImportNameMessage(source, binding.name, entry.Message))
			}
			if len(entry.AllowImportNames) > 0 &&
				!noRestrictedImportsContains(entry.AllowImportNames, binding.name) {
				checker.reportRange(binding.reportRange, noRestrictedImportsAllowedImportNameMessage(
					source, binding.name, entry.AllowImportNames, entry.Message))
			}
		}
	}
}

// reportForPattern is upstream's `reportPathForPatterns`.
//
// The four name-restriction fields are mutually exclusive, which the decoder enforces, so the arms
// below never both apply. Their order still matters for the namespace case, where upstream picks
// among four messages by which field is set.
func (checker *noRestrictedImportsChecker) reportForPattern(
	node *ast.Node,
	pattern noRestrictedImportsCompiledPattern,
	source string,
	bindings []noRestrictedImportsBinding,
	declarationIsTypeOnly bool,
) {
	entry := pattern.entry
	if entry.AllowTypeImports && declarationIsTypeOnly {
		return
	}

	restrictsNames := len(entry.ImportNames) > 0 || pattern.importNamePattern != nil
	allowsNames := len(entry.AllowImportNames) > 0 || pattern.allowedNamePattern != nil
	if !restrictsNames && !allowsNames {
		checker.report(node, noRestrictedImportsPatternMessage(source, entry.Message))
		return
	}

	for _, binding := range bindings {
		if binding.name == "*" {
			switch {
			case len(entry.ImportNames) > 0:
				checker.reportRange(binding.reportRange, noRestrictedImportsPatternEverythingMessage(
					source, entry.ImportNames, entry.Message))
			case len(entry.AllowImportNames) > 0:
				checker.reportRange(binding.reportRange,
					noRestrictedImportsEverythingWithAllowMessage(
						source, entry.AllowImportNames, entry.Message))
			case pattern.allowedNamePattern != nil:
				checker.reportRange(binding.reportRange,
					noRestrictedImportsEverythingWithAllowedPatternMessage(
						source, entry.AllowImportNamePattern, entry.Message))
			default:
				checker.reportRange(binding.reportRange,
					noRestrictedImportsPatternEverythingWithRegexMessage(
						source, entry.ImportNamePattern, entry.Message))
			}
			continue
		}

		if entry.AllowTypeImports && binding.typeOnly {
			continue
		}

		if noRestrictedImportsContains(entry.ImportNames, binding.name) ||
			(pattern.importNamePattern != nil && pattern.importNamePattern.Test(binding.name)) {
			checker.reportRange(binding.reportRange, noRestrictedImportsPatternAndImportNameMessage(
				source, binding.name, entry.Message))
		}

		// An `else if` upstream, not two independent tests: a name permitted by neither list is
		// reported once, under whichever list was configured.
		if len(entry.AllowImportNames) > 0 &&
			!noRestrictedImportsContains(entry.AllowImportNames, binding.name) {
			checker.reportRange(binding.reportRange, noRestrictedImportsAllowedImportNameMessage(
				source, binding.name, entry.AllowImportNames, entry.Message))
		} else if pattern.allowedNamePattern != nil &&
			!pattern.allowedNamePattern.Test(binding.name) {
			checker.reportRange(binding.reportRange, noRestrictedImportsAllowedNamePatternMessage(
				source, binding.name, entry.AllowImportNamePattern, entry.Message))
		}
	}
}

// report anchors a finding on a whole statement.
func (checker *noRestrictedImportsChecker) report(node *ast.Node, message rule.Message) {
	checker.ctx.ReportNode(node, message)
}

// reportRange anchors a finding on a span, which a namespace re-export needs because it has no
// specifier node.
func (checker *noRestrictedImportsChecker) reportRange(span core.TextRange, message rule.Message) {
	checker.ctx.ReportRange(span, message)
}
