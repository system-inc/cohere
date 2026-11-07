package main

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/cfg"
)

// functionIndex finds the innermost function body holding a position.
type functionIndex struct {
	units []functionUnit
	// graphs caches each body's control-flow graph.
	graphs map[*ast.BlockStmt]*cfg.CFG
}

// innermost is the smallest function body containing position, or the zero unit.
func (index *functionIndex) innermost(position token.Pos) functionUnit {
	var found functionUnit
	for _, unit := range index.units {
		if unit.body.Pos() <= position && position < unit.body.End() {
			if found.body == nil || (unit.body.Pos() >= found.body.Pos() && unit.body.End() <= found.body.End()) {
				found = unit
			}
		}
	}
	return found
}

// graph is the unit's control-flow graph. Every call is taken to return: a call that never returns only
// removes edges, and an extra edge can only merge webs.
func (index *functionIndex) graph(unit functionUnit) *cfg.CFG {
	if graph, built := index.graphs[unit.body]; built {
		return graph
	}
	graph := cfg.New(unit.body, func(*ast.CallExpr) bool { return true })
	index.graphs[unit.body] = graph
	return graph
}
