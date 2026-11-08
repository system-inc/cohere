package composite

import "slices"

// termList is the union of its terms' type sets, in normal form when its terms are disjoint. Its operations take
// lists in any form. x/tools' typeparams termlist, itself the compiler's types2 termlist.go.
type termList []*term

// everyType is the list of the one term of every type, in normal form. x/tools' typeparams allTermlist, built anew
// for each use so no list shares its backing array.
func everyType() termList {
	return termList{&term{}}
}

// isEmpty is whether list is the empty set of types. x/tools' typeparams termlist.isEmpty.
func (list termList) isEmpty() bool {
	for _, member := range list {
		if member != nil {
			return false
		}
	}
	return true
}

// isAll is whether list is the set of every type. x/tools' typeparams termlist.isAll.
func (list termList) isAll() bool {
	for _, member := range list {
		if member != nil && member.denoted == nil {
			return true
		}
	}
	return false
}

// normalized is list in normal form, each term merged with every later one that overlaps it, in the order of the
// terms that stay. x/tools' typeparams termlist.norm.
func (list termList) normalized() termList {
	used := make([]bool, len(list))
	var normal termList
	for index, member := range list {
		if member == nil || used[index] {
			continue
		}
		merged := member
		for laterIndex := index + 1; laterIndex < len(list); laterIndex++ {
			later := list[laterIndex]
			if later == nil || used[laterIndex] {
				continue
			}
			joined, separate := merged.union(later)
			if separate != nil {
				continue
			}
			// A merge into the term of every type makes the whole list every type, and stops here, since going on
			// could leave that term beside others and the list out of normal form.
			if joined.denoted == nil {
				return everyType()
			}
			merged = joined
			used[laterIndex] = true
		}
		normal = append(normal, merged)
	}
	return normal
}

// union is list ∪ other. x/tools' typeparams termlist.union, concatenating into a new list where the original
// appends, which may write into list's spare capacity, so no list a term set holds is ever changed.
func (list termList) union(other termList) termList {
	return slices.Concat(list, other).normalized()
}

// intersect is list ∩ other. x/tools' typeparams termlist.intersect.
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
