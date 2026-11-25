package shift

import "slices"

// termList is the union of its terms' type sets, in normal form when its terms are disjoint. Its operations don't
// need their operands in normal form. x/tools' typeparams.termlist.
type termList []*term

// allTerms is the set of every type, in normal form. x/tools' typeparams.allTermlist.
var allTerms = termList{new(term)}

// isEmpty is whether list holds no type at all. x/tools' termlist.isEmpty.
func (list termList) isEmpty() bool {
	for _, member := range list {
		if member != nil {
			return false
		}
	}
	return true
}

// isAll is whether list holds every type. x/tools' termlist.isAll.
func (list termList) isAll() bool {
	for _, member := range list {
		if member != nil && member.termType == nil {
			return true
		}
	}
	return false
}

// normalized is list in normal form, each term merged with every later one its union absorbs. x/tools'
// termlist.norm.
func (list termList) normalized() termList {
	used := make([]bool, len(list))
	var normal termList
	for index, member := range list {
		if member == nil || used[index] {
			continue
		}
		merged := member
		for later := index + 1; later < len(list); later++ {
			candidate := list[later]
			if candidate == nil || used[later] {
				continue
			}
			union, remainder := merged.union(candidate)
			if remainder != nil {
				continue
			}
			// A union that holds every type makes the whole list every type, and continuing could leave that term
			// beside others, out of normal form.
			if union.termType == nil {
				return allTerms
			}
			merged = union
			used[later] = true
		}
		normal = append(normal, merged)
	}
	return normal
}

// union is the union of list and other. x/tools' termlist.union, whose append can write into list's spare capacity;
// Concat never does, and the normalized result is the same.
func (list termList) union(other termList) termList {
	return slices.Concat(list, other).normalized()
}

// intersect is the intersection of list and other. x/tools' termlist.intersect.
func (list termList) intersect(other termList) termList {
	if list.isEmpty() || other.isEmpty() {
		return nil
	}
	var intersection termList
	for _, first := range list {
		for _, second := range other {
			if common := first.intersect(second); common != nil {
				intersection = append(intersection, common)
			}
		}
	}
	return intersection.normalized()
}
