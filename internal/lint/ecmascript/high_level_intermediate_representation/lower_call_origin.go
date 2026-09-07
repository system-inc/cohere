package high_level_intermediate_representation

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

type importedCallee struct {
	module *ast.Node
	name   string
}

type callOriginResolver struct {
	checker *checker.Checker
	symbols map[*ast.Symbol]bool
	exports map[importedCallee]bool
}

func (b *builder) calleeModuleOrigin(expression *ast.Node) ModuleExportOrigin {
	if b.typeChecker == nil {
		return ModuleExportOrigin{}
	}
	resolver := callOriginResolver{checker: b.typeChecker, symbols: map[*ast.Symbol]bool{}, exports: map[importedCallee]bool{}}
	origin := resolver.expression(expression)
	if origin.module != nil && origin.module.Text() == "react" {
		return ModuleExportOrigin{Module: "react", Export: origin.name}
	}
	return ModuleExportOrigin{}
}

func (r *callOriginResolver) expression(node *ast.Node) importedCallee {
	if node == nil {
		return importedCallee{}
	}
	switch node.Kind {
	case ast.KindParenthesizedExpression, ast.KindAsExpression, ast.KindTypeAssertionExpression, ast.KindNonNullExpression, ast.KindSatisfiesExpression:
		return r.expression(node.Expression())
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		receiver := r.expression(node.Expression())
		var name string
		if node.Kind == ast.KindPropertyAccessExpression && node.Name() != nil {
			name = node.Name().Text()
		} else if node.Kind == ast.KindElementAccessExpression {
			property := node.AsElementAccessExpression().ArgumentExpression
			if property != nil && ast.IsStringLiteralLike(property) {
				name = property.Text()
			}
		}
		if receiver.module != nil && (receiver.name == "*" || receiver.name == "default") && name != "" {
			return r.export(receiver.module, name)
		}
	case ast.KindIdentifier:
		return r.symbol(r.checker.GetSymbolAtLocation(node))
	}
	return importedCallee{}
}

func (r *callOriginResolver) symbol(symbol *ast.Symbol) importedCallee {
	if symbol == nil || r.symbols[symbol] {
		return importedCallee{}
	}
	r.symbols[symbol] = true
	defer delete(r.symbols, symbol)
	for _, declaration := range symbol.Declarations {
		switch declaration.Kind {
		case ast.KindImportSpecifier, ast.KindImportClause, ast.KindNamespaceImport, ast.KindExportSpecifier:
			var container *ast.Node
			for parent := declaration.Parent; parent != nil; parent = parent.Parent {
				if parent.Kind == ast.KindImportDeclaration || parent.Kind == ast.KindExportDeclaration {
					container = parent
					break
				}
			}
			if container == nil {
				continue
			}
			var module *ast.Node
			if container.Kind == ast.KindImportDeclaration {
				module = container.AsImportDeclaration().ModuleSpecifier
			} else {
				module = container.AsExportDeclaration().ModuleSpecifier
			}
			if module == nil || !ast.IsStringLiteralLike(module) {
				continue
			}
			name := "default"
			if declaration.Kind == ast.KindNamespaceImport {
				return importedCallee{module: module, name: "*"}
			}
			if declaration.Kind == ast.KindImportSpecifier || declaration.Kind == ast.KindExportSpecifier {
				if declaration.PropertyName() != nil {
					name = declaration.PropertyName().Text()
				} else if declaration.Name() != nil {
					name = declaration.Name().Text()
				}
			}
			if origin := r.export(module, name); origin.module != nil {
				return origin
			}
		case ast.KindVariableDeclaration:
			if declaration.Parent != nil && declaration.Parent.Kind == ast.KindVariableDeclarationList && declaration.Parent.Flags&ast.NodeFlagsConst != 0 {
				if origin := r.expression(declaration.AsVariableDeclaration().Initializer); origin.module != nil {
					return origin
				}
			}
		}
	}
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return r.symbol(checker.Checker_getImmediateAliasedSymbol(r.checker, symbol))
	}
	return importedCallee{}
}

func (r *callOriginResolver) moduleSymbol(module *ast.Node) *ast.Symbol {
	symbol := r.checker.GetSymbolAtLocation(module)
	if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
		symbol = checker.Checker_resolveAlias(r.checker, symbol)
	}
	if symbol == nil || symbol.Flags&ast.SymbolFlagsModule == 0 {
		return nil
	}
	return symbol
}

func (r *callOriginResolver) exportedSymbol(module *ast.Symbol, name string) *ast.Symbol {
	if module != nil {
		for _, symbol := range r.checker.GetExportsOfModule(module) {
			if symbol.Name == name {
				return symbol
			}
		}
	}
	return nil
}

func (r *callOriginResolver) resolvedSymbol(symbol *ast.Symbol) *ast.Symbol {
	if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return checker.Checker_resolveAlias(r.checker, symbol)
	}
	return symbol
}

func (r *callOriginResolver) export(module *ast.Node, name string) importedCallee {
	key := importedCallee{module: module, name: name}
	if r.exports[key] {
		return importedCallee{}
	}
	if module.Text() == "react" {
		return key
	}
	r.exports[key] = true
	defer delete(r.exports, key)
	moduleSymbol := r.moduleSymbol(module)
	exported := r.exportedSymbol(moduleSymbol, name)
	if exported == nil {
		return importedCallee{}
	}
	if origin := r.symbol(exported); origin.module != nil {
		return origin
	}
	for _, declaration := range moduleSymbol.Declarations {
		if declaration.Kind != ast.KindSourceFile {
			continue
		}
		for _, statement := range declaration.AsSourceFile().Statements.Nodes {
			if statement.Kind != ast.KindExportDeclaration {
				continue
			}
			star := statement.AsExportDeclaration()
			if star.ExportClause != nil || star.ModuleSpecifier == nil {
				continue
			}
			candidate := r.exportedSymbol(r.moduleSymbol(star.ModuleSpecifier), name)
			if candidate != nil && r.resolvedSymbol(candidate) == r.resolvedSymbol(exported) {
				if origin := r.export(star.ModuleSpecifier, name); origin.module != nil {
					return origin
				}
			}
		}
	}
	return importedCallee{}
}
