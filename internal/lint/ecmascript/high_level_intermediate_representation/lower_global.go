package high_level_intermediate_representation

import "github.com/microsoft/TypeScript/tsc/shim/ast"

func moduleGlobalLoad(node *ast.Node, symbol *ast.Symbol) *LoadGlobal {
	load := &LoadGlobal{Name: node.Text(), BindingKind: GlobalBindingKindGlobal}
	if symbol == nil {
		return load
	}
	for _, declaration := range symbol.Declarations {
		switch declaration.Kind {
		case ast.KindImportSpecifier, ast.KindImportClause, ast.KindNamespaceImport:
			for container := declaration.Parent; container != nil; container = container.Parent {
				if container.Kind != ast.KindImportDeclaration {
					continue
				}
				module := container.AsImportDeclaration().ModuleSpecifier
				if module == nil || !ast.IsStringLiteralLike(module) {
					break
				}
				load.Source = module.Text()
				switch declaration.Kind {
				case ast.KindImportSpecifier:
					load.BindingKind = GlobalBindingKindImportSpecifier
					load.Imported = declaration.Name().Text()
					if declaration.PropertyName() != nil {
						load.Imported = declaration.PropertyName().Text()
					}
				case ast.KindNamespaceImport:
					load.BindingKind = GlobalBindingKindImportNamespace
				default:
					load.BindingKind = GlobalBindingKindImportDefault
				}
				return load
			}
		default:
			if ast.GetSourceFileOfNode(declaration) == ast.GetSourceFileOfNode(node) {
				load.BindingKind = GlobalBindingKindModuleLocal
			}
		}
	}
	return load
}
