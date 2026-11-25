package rule

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// HasDeclaration compares identity only. A caller can ask whether a symbol names a node it already
// collected in its own file without receiving or inspecting any foreign syntax.
func HasDeclaration(symbol *ast.Symbol, candidate *ast.Node) bool {
	if symbol != nil {
		for _, declaration := range symbol.Declarations {
			if declaration == candidate {
				return true
			}
		}
	}
	return false
}

// HasDeclarationInSet is the any-declaration version of FirstDeclarationInSet. False entries in the
// set remain false: the existing import-binding maps use their values, not just key presence.
func HasDeclarationInSet(symbol *ast.Symbol, candidates map[*ast.Node]bool) bool {
	if symbol != nil {
		for _, declaration := range symbol.Declarations {
			if candidates[declaration] {
				return true
			}
		}
	}
	return false
}

// FirstDeclarationInSet preserves the checker's order, including a foreign first declaration. It
// compares identity only and must not be replaced with an any-declaration or own-file filter.
func FirstDeclarationInSet(symbol *ast.Symbol, candidates map[*ast.Node]bool) bool {
	return symbol != nil && len(symbol.Declarations) != 0 && candidates[symbol.Declarations[0]]
}

// IsDeclaredInFile compares declaration-file identity, ignoring nil declarations. It exposes no node
// and reads neither positions nor declaration kinds, even when the symbol comes from another file.
func IsDeclaredInFile(file *ast.SourceFile, symbol *ast.Symbol) bool {
	if symbol != nil {
		for _, declaration := range symbol.Declarations {
			if declaration != nil && ast.GetSourceFileOfNode(declaration) == file {
				return true
			}
		}
	}
	return false
}
