package iface_assert

// termList is the union of its terms' type sets. It is normal when its terms are disjoint, and its operations take
// lists in any form. x/tools' typeparams.termlist, which copies go/types' own, less the operations the pass never
// calls.
type termList []*term

// allTerms is the list that holds every type, in normal form.
var allTerms = termList{new(term)}

// isEmpty is whether list holds no type: whether its every term is the empty one. x/tools' typeparams.termlist.isEmpty.
func (list termList) isEmpty() bool {
	for _, member := range list {
		if member != nil {
			return false
		}
	}
	return true
}

// isAll is whether list holds every type: whether one of its terms does. x/tools' typeparams.termlist.isAll.
func (list termList) isAll() bool {
	for _, member := range list {
		if member != nil && member.termType == nil {
			return true
		}
	}
	return false
}

// normalized is list's normal form, each term folded into the first it overlaps. x/tools' typeparams.termlist.norm,
// whose quadratic walk it keeps, so the terms come out in the same order.
func (list termList) normalized() termList {
	used := make([]bool, len(list))
	var normal termList
	for index, member := range list {
		if member == nil || used[index] {
			continue
		}
		// folded is member with every later overlapping term unioned into it.
		folded := member
		for laterIndex := index + 1; laterIndex < len(list); laterIndex++ {
			later := list[laterIndex]
			if later == nil || used[laterIndex] {
				continue
			}
			union, disjointRemainder := folded.union(later)
			if disjointRemainder != nil {
				continue
			}
			// A union that holds every type makes the list hold every type, and it returns at once: going on could
			// leave that term beside others, which would not be normal.
			if union.termType == nil {
				return allTerms
			}
			folded = union
			used[laterIndex] = true
		}
		normal = append(normal, folded)
	}
	return normal
}

// union is list ∪ other. x/tools' typeparams.termlist.union.
func (list termList) union(other termList) termList {
	// The full slice expression caps the copy at list's length, so the append never writes into list's backing array,
	// which x/tools' append(xl, yl...) may. The result is the same list either way, and no caller's list is touched.
	return append(list[:len(list):len(list)], other...).normalized()
}

// intersect is list ∩ other. x/tools' typeparams.termlist.intersect.
func (list termList) intersect(other termList) termList {
	if list.isEmpty() || other.isEmpty() {
		return nil
	}
	var intersection termList
	for _, member := range list {
		for _, otherMember := range other {
			if both := member.intersect(otherMember); both != nil {
				intersection = append(intersection, both)
			}
		}
	}
	return intersection.normalized()
}
