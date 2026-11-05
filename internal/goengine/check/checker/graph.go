// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package checker

import (
	"io"
	"iter"
)

// Graph holds the results of a round of analysis, including the graph
// of requested actions (analyzers applied to packages) plus any
// dependent actions that it was necessary to compute.
type Graph struct {
	// Roots contains the roots of the action graph.
	// Each node (a, p) in the action graph represents the
	// application of one analyzer a to one package p.
	// (A node thus corresponds to one analysis.Pass instance.)
	// Roots holds one action per element of the product
	// of the analyzers × packages arguments to Analyze,
	// in unspecified order.
	//
	// Each element of Action.Deps represents an edge in the
	// action graph: a dependency from one action to another.
	// An edge of the form (a, p) -> (a, p2) indicates that the
	// analysis of package p requires information ("facts") from
	// the same analyzer applied to one of p's dependencies, p2.
	// An edge of the form (a, p) -> (a2, p) indicates that the
	// analysis of package p requires information ("results")
	// from a different analyzer a2 applied to the same package.
	// These two kind of edges are called "vertical" and "horizontal",
	// respectively.
	Roots []*Action
}

// All returns an iterator over the action graph in depth-first postorder.
//
// Example:
//
//	for act := range graph.All() {
//		...
//	}
func (g *Graph) All() iter.Seq[*Action] {
	return func(yield func(*Action) bool) {
		forEach(g.Roots, func(act *Action) error {
			if !yield(act) {
				return io.EOF // any error will do
			}
			return nil
		}) // ignore error
	}
}
