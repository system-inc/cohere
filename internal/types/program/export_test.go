package program

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// RecordedRealSignatures is a previous table in which every project source file has a real signature recorded
// for other bytes, so Signatures emits each one, as it does a file edited since its signature was computed.
// For tests about what a computed signature holds: a first run with nothing recorded computes none (#5txm9gg).
func RecordedRealSignatures(g *Graph) map[string]SignatureEntry {
	recorded := map[string]SignatureEntry{}
	for _, sourceFile := range g.ProjectFiles() {
		if sourceFile.IsDeclarationFile || ast.IsJsonSourceFile(sourceFile) {
			continue
		}
		recorded[sourceFile.FileName()] = SignatureEntry{Version: "recorded for other bytes", Signature: "a real signature, recorded for other bytes"}
	}
	return recorded
}

// SignatureEmits is how many files Signatures has emitted declarations for on this graph.
func (g *Graph) SignatureEmits() int {
	return g.signatureEmits
}
