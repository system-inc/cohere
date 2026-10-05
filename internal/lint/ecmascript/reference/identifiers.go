package reference

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// IdentifiersNamed returns every identifier in the rule's file whose text is name, in the order a
// ForEachChild walk from the source file reaches them, which is source order.
//
// A rule asking "where else is this binding written, or read" has to look at the whole file: a write
// can sit before the declaration, after it, or inside a callback several scopes down. Walking the
// file once per binding visits every identifier for every binding, so a file with a hundred `let`s
// was walked two or three hundred times by prefer-const alone (#hekjpw3). This walks it once, the
// first time any rule asks, and indexes the identifiers by text, so the per-binding question reads
// the few nodes that could answer it. The order is the walk's own, so a caller that took the first
// match of a whole-file walk takes the same node from this list.
//
// Text equality is a pre-filter, not resolution: two bindings of one name in different scopes share
// a list, and the caller still asks the checker which of them a node resolves to.
func IdentifiersNamed(ctx rule.Context, name string) []*ast.Node {
	if ctx.SourceFile == nil {
		return nil
	}
	index := rule.Cached(ctx.FileCache, "reference.identifiersByName", func() map[string][]*ast.Node {
		byName := map[string][]*ast.Node{}
		var visit func(*ast.Node)
		visit = func(current *ast.Node) {
			if current.Kind == ast.KindIdentifier {
				byName[current.Text()] = append(byName[current.Text()], current)
			}
			current.ForEachChild(func(child *ast.Node) bool {
				visit(child)
				return false
			})
		}
		visit(ctx.SourceFile.AsNode())
		return byName
	})
	return index[name]
}
