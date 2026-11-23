package consistency_no_any

import (
	"go/ast"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/ast/inspector"
)

// FlowNode is a declaration the rule's flow routes a type through, as it keys one: a variable, field, parameter,
// result or function by its object, or a function literal, which has no object, by its node.
type FlowNode struct {
	Object  types.Object
	Literal *ast.FuncLit
}

// FlowEdges are the two tables of the rule's flow that settle spreads a fixed type along: DefinedFrom, each variable a
// short declaration or a var with no type defines, to the declarations its value came from, so a place fixing the
// variable fixes them; and Feeds, each declaration, to those given its value at their own type, which a source on it
// then fixes.
type FlowEdges struct {
	DefinedFrom map[FlowNode][]FlowNode
	Feeds       map[FlowNode][]FlowNode
}

// RecordFlows is the rule's flow tables over files, recorded by the rule's own walk (recordUntilFixed) with no early
// stop, as it records them a top-level declaration at a time, but for each function declaration walks says to leave
// out, whose body a strip has taken or will take. It matches no method and settles nothing: a reader that bounds what
// the rule can route to a declaration (standing_presence's noany row) reads the edges themselves.
func RecordFlows(
	information *types.Info,
	own *types.Package,
	files []*ast.File,
	walks func(declaration *ast.FuncDecl) bool,
) FlowEdges {
	flows := &flowAnalysis{information: information, own: own, sinkFixed: map[flowKey]bool{},
		definedFrom: map[flowKey][]flowKey{}, sourceFixed: map[flowKey]bool{}, feeds: map[flowKey][]flowKey{}}
	for file := range inspector.New(files).Root().Children() {
		for declaration := range file.Children() {
			if function, isFunction := declaration.Node().(*ast.FuncDecl); isFunction && !walks(function) {
				continue
			}
			for cursor := range declaration.Preorder(handingKinds...) {
				flows.record(cursor)
			}
		}
	}
	return FlowEdges{DefinedFrom: flowNodes(flows.definedFrom), Feeds: flowNodes(flows.feeds)}
}

// flowNodes is table with each key a FlowNode.
func flowNodes(table map[flowKey][]flowKey) map[FlowNode][]FlowNode {
	nodes := make(map[FlowNode][]FlowNode, len(table))
	for from, to := range table {
		fromNode := FlowNode{Object: from.object, Literal: from.literal}
		for _, each := range to {
			nodes[fromNode] = append(nodes[fromNode], FlowNode{Object: each.object, Literal: each.literal})
		}
	}
	return nodes
}
