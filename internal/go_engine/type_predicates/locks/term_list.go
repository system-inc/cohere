package locks

import "slices"

// termList is the union of its terms' sets of types, in normal form when no two of its terms share a type. Its
// operations take lists in any form. x/tools' typeparams.termlist, generated there from the type checker's own.
type termList []*term

// allTermList is every type, in normal form.
var allTermList = termList{&term{}}

// isEmpty reports whether list holds no type.
func (list termList) isEmpty() bool {
	for _, member := range list {
		if member != nil {
			return false
		}
	}
	return true
}

// isAll reports whether list holds every type.
func (list termList) isAll() bool {
	for _, member := range list {
		if member != nil && member.denoted == nil {
			return true
		}
	}
	return false
}

// normalized is list in normal form, each term merged with every later one it overlaps, in the original's order.
func (list termList) normalized() termList {
	merged := make([]bool, len(list))
	var normal termList
	for index, member := range list {
		if member == nil || merged[index] {
			continue
		}
		widened := member
		for later := index + 1; later < len(list); later++ {
			other := list[later]
			if other == nil || merged[later] {
				continue
			}
			union, disjoint := widened.union(other)
			if disjoint != nil {
				continue
			}
			// A term holding every type makes the whole list every type, and continuing would leave it beside
			// other terms, out of normal form.
			if union.denoted == nil {
				return allTermList
			}
			widened = union
			merged[later] = true
		}
		normal = append(normal, widened)
	}
	return normal
}

// union is the types in list or other.
func (list termList) union(other termList) termList {
	return slices.Concat(list, other).normalized()
}

// intersect is the types in both list and other.
func (list termList) intersect(other termList) termList {
	if list.isEmpty() || other.isEmpty() {
		return nil
	}
	var intersection termList
	for _, member := range list {
		for _, otherMember := range other {
			if shared := member.intersect(otherMember); shared != nil {
				intersection = append(intersection, shared)
			}
		}
	}
	return intersection.normalized()
}
