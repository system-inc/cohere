package high_level_intermediate_representation

import (
	"sort"

	"github.com/system-inc/cohere/static_single_assignment"
)

// DisjointSet is a union-find over values, keyed by IdentifierId.
//
// This is React's `DisjointSet` (line 32144) with the same two operations and the same path
// compression. Membership is sparse: a value that was never passed to `Union` is absent, and `Find`
// answers `0, false` for it rather than inventing a singleton class. That absence is meaningful --
// upstream's consumer spells it `find(id) ?? id`, falling back to the value itself -- and
// `RepresentativeOf` is that fallback written once so every consumer does not re-spell it.
//
// The zero value is an empty set and is ready to use.
type DisjointSet struct {
	parent map[static_single_assignment.IdentifierId]static_single_assignment.IdentifierId
}

// Union merges every value in items into one class.
//
// The first item becomes the class representative when none of the items is already in the set,
// which is upstream's rule exactly: it takes `items.shift()`, finds its root, and adopts the rest
// under that root. An empty list is a no-op here; upstream raises an invariant on it, and the
// difference is deliberate because every call site below builds the list conditionally and an empty
// one is the ordinary outcome rather than a bug.
//
// Note that upstream's `union` mutates the caller's array through `shift()`. This takes the slice by
// value and never writes to it, which is a difference in the implementation rather than in what it
// decides; the call sites below would be correct under either.
func (set *DisjointSet) Union(items []static_single_assignment.IdentifierId) {
	if len(items) == 0 {
		return
	}
	if set.parent == nil {
		set.parent = map[static_single_assignment.IdentifierId]static_single_assignment.IdentifierId{}
	}

	first := items[0]
	root, ok := set.Find(first)
	if !ok {
		root = first
		set.parent[first] = first
	}

	for _, item := range items[1:] {
		itemParent, present := set.parent[item]
		if !present {
			set.parent[item] = root
			continue
		}
		if itemParent == root {
			continue
		}
		// Walk this item's chain to the root, repointing every link at the new root. Upstream's
		// loop, transcribed: it rewrites the whole path rather than only the item, which is what
		// keeps the structure flat when two established classes merge.
		current := item
		for itemParent != root {
			set.parent[current] = root
			current = itemParent
			itemParent = set.parent[current]
		}
	}
}

// Find returns the representative of a value's class, and whether the value is in the set at all.
//
// Path compression is applied on the way out, which is what keeps the structure flat and what makes
// the recursion depth safe. Upstream returns `null` for an absent value; the second return is that
// null, kept separate from the id because zero is a usable IdentifierId and conflating them would
// make an absent value read as a member of class zero.
func (set *DisjointSet) Find(item static_single_assignment.IdentifierId) (static_single_assignment.IdentifierId, bool) {
	if set.parent == nil {
		return 0, false
	}
	parent, ok := set.parent[item]
	if !ok {
		return 0, false
	}
	if parent == item {
		return item, true
	}
	root, ok := set.Find(parent)
	if !ok {
		return 0, false
	}
	set.parent[item] = root
	return root, true
}

// RepresentativeOf returns a value's class representative, falling back to the value itself.
//
// This is upstream's consumer spelling, `aliasedIdentifiers.find(place.identifier) ?? place.identifier`
// (line 43011), written once here so that every consumer does not re-derive the fallback. A value
// that was never unified is its own representative, which is the answer that lets a caller key a map
// on the result without first asking whether the value is a member.
func (set *DisjointSet) RepresentativeOf(
	item static_single_assignment.IdentifierId,
) static_single_assignment.IdentifierId {
	if root, ok := set.Find(item); ok {
		return root
	}
	return item
}

// Has reports whether a value was ever unified into a class.
func (set *DisjointSet) Has(item static_single_assignment.IdentifierId) bool {
	if set.parent == nil {
		return false
	}
	_, ok := set.parent[item]
	return ok
}

// Size reports how many values are members of some class.
func (set *DisjointSet) Size() int { return len(set.parent) }

// Sets returns the classes, each sorted, with the outer list ordered by first member.
//
// This is upstream's `buildSets` with a determinism guarantee added. Upstream iterates a `Map` whose
// order is insertion order and therefore stable for it; the backing map here is a Go map, so the
// order is randomised and would leak into anything a caller derived from the result. Both levels are
// sorted for that reason. The classes themselves are identical either way -- only the order is
// pinned, and it is pinned because a non-deterministic ordering is the shape that makes a cache
// non-reproducible without ever producing a wrong answer.
func (set *DisjointSet) Sets() [][]static_single_assignment.IdentifierId {
	if len(set.parent) == 0 {
		return nil
	}
	members := make([]static_single_assignment.IdentifierId, 0, len(set.parent))
	for item := range set.parent {
		members = append(members, item)
	}
	sort.Slice(members, func(left, right int) bool { return members[left] < members[right] })

	grouped := map[static_single_assignment.IdentifierId][]static_single_assignment.IdentifierId{}
	var order []static_single_assignment.IdentifierId
	for _, item := range members {
		root := set.RepresentativeOf(item)
		if _, seen := grouped[root]; !seen {
			order = append(order, root)
		}
		grouped[root] = append(grouped[root], item)
	}

	sets := make([][]static_single_assignment.IdentifierId, 0, len(order))
	for _, root := range order {
		sets = append(sets, grouped[root])
	}
	return sets
}
