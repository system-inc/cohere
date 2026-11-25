package differences

// tree is a slice whose elements are trees, so a tree is both a list of elements and one element.
type tree []tree

// names is a named slice type, whose empty literal is as much a slice that is not nil as []string{}.
type names []string

func spreads(m map[string][]any, trees map[string]tree, k string, values []any, branch tree) {
	// The append spreads the elements of values and the literal holds values itself as one element, so a missing
	// key ends with one element where the original's fix leaves len(values): the guard is needed. The pattern's
	// CallExpr has no Ellipsis, so the original matches; the port requires a call that spreads nothing.
	if _, ok := m[k]; ok { // port drops: unnecessary guard around map access
		m[k] = append(m[k], values...)
	} else {
		m[k] = []any{values}
	}
	// The same where the element type is the slice's own, not an interface.
	if _, ok := trees[k]; ok { // port drops: unnecessary guard around map access
		trees[k] = append(trees[k], branch...)
	} else {
		trees[k] = tree{branch}
	}
	// Spread or not, a call that does not match the literal is no finding of either.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], values...)
	} else {
		m[k] = values
	}
	// Without the spread both report it, and both fixes put the append in the if's place.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], values)
	} else {
		m[k] = []any{values}
	}
}

func emptyAppends(m map[string][]string, named map[string]names, k string) {
	// An append of nothing to a missing key's zero value is nil, and the else stores an empty slice that is not, which
	// a comparison with nil, reflect and encoding/json tell apart: the guard is needed.
	if _, ok := m[k]; ok { // port drops: unnecessary guard around map access
		m[k] = append(m[k])
	} else {
		m[k] = []string{}
	}
	// The same with a literal of a named slice type.
	if _, ok := named[k]; ok { // port drops: unnecessary guard around map access
		named[k] = append(named[k])
	} else {
		named[k] = names{}
	}
	// With an element to append, both report it, as the zero value's append makes the literal's elements.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
}

func elseBranches(m map[string]int, lists map[string][]string, k string, first bool, second bool) {
	if first {
		m[k] = 0
		// An else branch is an if or a block, and the original's fix writes `} else m[k]++`, which does not parse.
		// The port's fix writes the if's body there, `} else {` and the increment alone.
	} else if _, ok := m[k]; ok { // port moves: unnecessary guard around map access
		m[k]++
	} else {
		m[k] = 1
	}
	if first {
		m[k] = 0
		// The original's fix writes `} else m[k] += 2`.
	} else if _, ok := m[k]; ok { // port moves: unnecessary guard around map access
		m[k] += 2
	} else {
		m[k] = 2
	}
	if first {
		m[k] = 0
	} else if second {
		m[k] = 1
		// The original's fix writes `} else lists[k] = append(lists[k], "a")`, at the end of a chain of else ifs.
	} else if _, ok := lists[k]; ok { // port moves: unnecessary guard around map access
		lists[k] = append(lists[k], "a")
	} else {
		lists[k] = []string{"a"}
	}
	// A guard in an else block, not an else if, is a statement of the block, and both fixes put the bare write in its
	// place.
	if first {
		m[k] = 0
	} else {
		if _, ok := m[k]; ok {
			m[k]++
		} else {
			m[k] = 1
		}
	}
	// A guard whose own else is another guard: the outer one's else is an if, so neither reports the outer.
	if _, ok := m["outer"]; ok {
		m["outer"]++
		// The inner one is an else branch: the original's fix writes `} else m[k]++`, and the port's a block.
	} else if _, ok := m[k]; ok { // port moves: unnecessary guard around map access
		m[k]++
	} else {
		m[k] = 1
	}
}
