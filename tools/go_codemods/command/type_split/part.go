package main

import "go/ast"

// part is what one file holds of one type with methods: the type's declaration, when the file declares
// it, and the methods of it the file writes. Ruling 13 (c): a file serves every type it holds a part
// of, and may serve at most one.
type part struct {
	name     string
	file     *sourceFile
	record   *typeRecord
	declares bool
	methods  []*ast.FuncDecl
	// ambiguous is a type declared once per build, whose parts never move.
	ambiguous bool
}

// lineCount is the lines the part takes in its file.
func (piece *part) lineCount() int {
	lines := 0
	if piece.declares {
		lines += piece.file.line(piece.record.spec.End()) - piece.file.line(piece.record.spec.Pos()) + 1
	}
	for _, method := range piece.methods {
		lines += piece.file.line(method.End()) - piece.file.line(method.Pos()) + 1
	}
	return lines
}

// readsRuleProgram is whether piece's code reads the program through a rule.Context in a file that defines a rule,
// as internal/guard reads it: a `.Program` selector on a context, named ctx or context or a parameter of type
// rule.Context anywhere in the file, directly or as the last link of a chain, and a `rule.Rule` composite literal
// in the file. The guard requires such a read to sit in a file that defines a rule, so the part cannot leave it.
func (piece *part) readsRuleProgram() bool {
	syntax := piece.file.syntax
	definesRule := false
	contextNames := map[string]bool{"ctx": true, "context": true}
	ast.Inspect(syntax, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CompositeLit:
			if isRuleSelector(typed.Type, "Rule") {
				definesRule = true
			}
		case *ast.FuncType:
			if typed.Params != nil {
				for _, field := range typed.Params.List {
					if isRuleSelector(field.Type, "Context") {
						for _, name := range field.Names {
							contextNames[name.Name] = true
						}
					}
				}
			}
		}
		return true
	})
	if !definesRule {
		return false
	}
	var nodes []ast.Node
	if piece.declares {
		nodes = append(nodes, piece.record.spec)
	}
	for _, method := range piece.methods {
		nodes = append(nodes, method)
	}
	reads := false
	for _, node := range nodes {
		ast.Inspect(node, func(inner ast.Node) bool {
			selector, isSelector := inner.(*ast.SelectorExpr)
			if !isSelector || selector.Sel.Name != "Program" {
				return true
			}
			switch receiver := selector.X.(type) {
			case *ast.Ident:
				reads = reads || contextNames[receiver.Name]
			case *ast.SelectorExpr:
				reads = reads || contextNames[receiver.Sel.Name]
			}
			return true
		})
	}
	return reads
}
