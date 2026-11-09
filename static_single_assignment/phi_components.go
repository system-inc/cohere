package static_single_assignment

import "slices"

// phiComponents is Algorithm 5's working state over one function's phis, each named by its index in
// phis, which is the block slice's order and each block's phi order, so the same function always gives
// the same components in the same order.
type phiComponents[G Graph[F, B, P], F any, B comparable, P any] struct {
	graph G
	phis  []*Phi[P]
	// index maps a phi's result to its index in phis.
	index map[IdentifierId]int
	// rewrites maps the result of each phi in a collapsed component to the value it collapsed to, and
	// resolve reads an identifier through it, so an operand from a collapsed component reads as its value.
	rewrites map[IdentifierId]IdentifierId
	resolve  func(IdentifierId) IdentifierId

	// member[i] is the number of the search phi i last joined: a search covers the phis handed to
	// removeRedundant, and an edge to a phi outside them is not followed. Searches are numbered from 1,
	// so a phi no search has covered is in none.
	member []int
	search int
	// order, lowest, onStack and stack are Tarjan's: when the search first reached each phi, the
	// earliest phi on the stack it reaches back to, and the phis whose component is still open.
	order, lowest []int
	onStack       []bool
	stack         []int
	visited       int
	// component[i] is the number of the component phi i was last placed in, numbered from 1 across
	// every search, so a phi is in a component exactly when the numbers match.
	component  []int
	components int
	// found is the current search's components, in the order Tarjan's algorithm closes them: a component
	// closes after every component its phis use, which is the order Algorithm 5 wants.
	found [][]int
}

// anyPhiOperand reports whether some phi has another phi as an operand, without which no component has
// two phis and the step has nothing to do.
func (components *phiComponents[G, F, B, P]) anyPhiOperand() bool {
	for _, phi := range components.phis {
		result := components.graph.IdentifierOf(phi.Place)
		for _, operand := range phi.Operands {
			id := components.graph.IdentifierOf(operand.Place)
			if _, isPhi := components.index[id]; isPhi && id != result {
				return true
			}
		}
	}
	return false
}

// operand is the phi an operand names, after the components collapsed so far, and whether it names one.
func (components *phiComponents[G, F, B, P]) operand(place P) (int, bool) {
	id := components.resolve(components.graph.IdentifierOf(place))
	index, isPhi := components.index[id]
	return index, isPhi
}

// removeRedundant is Algorithm 5's removeRedundantPhis over the phis at these indices: their strongly
// connected components, each processed once every component it uses has been.
func (components *phiComponents[G, F, B, P]) removeRedundant(phis []int) {
	components.search++
	for _, index := range phis {
		components.member[index] = components.search
		components.order[index] = -1
	}
	components.found = nil
	for _, index := range phis {
		if components.order[index] == -1 {
			components.connect(index)
		}
	}
	// Processing may recurse into another search, which replaces found, but range reads the slice once, so this
	// search still walks its own components.
	for _, component := range components.found {
		components.process(component)
	}
}

// connect is Tarjan's strongly connected components from a phi, over the phis in the current search.
func (components *phiComponents[G, F, B, P]) connect(phi int) {
	components.order[phi] = components.visited
	components.lowest[phi] = components.visited
	components.visited++
	components.stack = append(components.stack, phi)
	components.onStack[phi] = true

	for _, entry := range components.phis[phi].Operands {
		next, isPhi := components.operand(entry.Place)
		if !isPhi || components.member[next] != components.search {
			continue
		}
		if components.order[next] == -1 {
			components.connect(next)
			components.lowest[phi] = min(components.lowest[phi], components.lowest[next])
		} else if components.onStack[next] {
			components.lowest[phi] = min(components.lowest[phi], components.order[next])
		}
	}

	if components.lowest[phi] != components.order[phi] {
		return
	}
	components.components++
	var component []int
	for {
		top := components.stack[len(components.stack)-1]
		components.stack = components.stack[:len(components.stack)-1]
		components.onStack[top] = false
		components.component[top] = components.components
		component = append(component, top)
		if top == phi {
			break
		}
	}
	// Tarjan's algorithm pops a component in reverse; this puts it back in phis' order.
	slices.Sort(component)
	components.found = append(components.found, component)
}

// process is Algorithm 5's processSCC: a component with one value from outside collapses to it, and one
// with several recurses on its inner phis.
func (components *phiComponents[G, F, B, P]) process(component []int) {
	if len(component) < 2 {
		return
	}
	number := components.component[component[0]]
	var inner []int
	// outside is the first value from outside the component, and outsideValues counts the distinct ones
	// up to two, since one is the only count that collapses.
	var outside IdentifierId
	outsideValues := 0
	for _, phi := range component {
		isInner := true
		for _, entry := range components.phis[phi].Operands {
			if next, isPhi := components.operand(entry.Place); isPhi && components.component[next] == number {
				continue
			}
			isInner = false
			value := components.resolve(components.graph.IdentifierOf(entry.Place))
			if outsideValues == 0 {
				outside, outsideValues = value, 1
			} else if value != outside {
				outsideValues = 2
			}
		}
		if isInner {
			inner = append(inner, phi)
		}
	}
	switch {
	case outsideValues == 1:
		for _, phi := range component {
			components.rewrites[components.graph.IdentifierOf(components.phis[phi].Place)] = outside
		}
	case outsideValues > 1 && len(inner) > 1:
		components.removeRedundant(inner)
	}
}
