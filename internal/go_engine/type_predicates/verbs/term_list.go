package verbs

// termList is the union of its terms' type sets, and in normal form when its terms are disjoint: x/tools' typeparams
// termlist, translated as far as structuralTerms reads it.
type termList []*typeTerm

// allTerms is the set of every type, in normal form.
var allTerms = termList{new(typeTerm)}

// isEmpty reports whether the list is the empty set.
func (list termList) isEmpty() bool {
	for _, term := range list {
		if term != nil {
			return false
		}
	}
	return true
}

// isAll reports whether the list is the set of every type.
func (list termList) isAll() bool {
	for _, term := range list {
		if term != nil && term.termType == nil {
			return true
		}
	}
	return false
}

// norm is the list in normal form, quadratic, as x/tools' is.
func (list termList) norm() termList {
	used := make([]bool, len(list))
	var normal termList
	for index, term := range list {
		if term == nil || used[index] {
			continue
		}
		merged := term
		for later := index + 1; later < len(list); later++ {
			laterTerm := list[later]
			if laterTerm == nil || used[later] {
				continue
			}
			union, second := merged.union(laterTerm)
			if second != nil {
				continue
			}
			// A term of every type makes the whole list every type, and returning now keeps the result in normal
			// form, which going on with other terms beside it would not.
			if union.termType == nil {
				return allTerms
			}
			merged = union
			used[later] = true // laterTerm is in merged now, so later iterations skip it.
		}
		normal = append(normal, merged)
	}
	return normal
}

// union is list ∪ other.
func (list termList) union(other termList) termList {
	return append(list, other...).norm()
}

// intersect is list ∩ other, quadratic, as x/tools' is.
func (list termList) intersect(other termList) termList {
	if list.isEmpty() || other.isEmpty() {
		return nil
	}
	var intersection termList
	for _, term := range list {
		for _, otherTerm := range other {
			if both := term.intersect(otherTerm); both != nil {
				intersection = append(intersection, both)
			}
		}
	}
	return intersection.norm()
}
