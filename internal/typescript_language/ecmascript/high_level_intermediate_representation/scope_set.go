package high_level_intermediate_representation

// scopeSet is a union-find over ScopeId, mirroring upstream's `DisjointSet` including its
// insertion-ordered iteration.
//
// `DisjointSet` in this package is keyed on IdentifierId and cannot be reused: this pass unions
// scopes rather than values, and the two id spaces are unrelated. Generics were the other option and
// were declined because `DisjointSet`'s doc comment is written entirely about values and mutation
// aliasing, and making it generic would leave that comment describing one of two instantiations.
//
// `order` exists because Go map iteration is randomised where upstream's `Map` is insertion-ordered,
// and upstream's `forEach` walks that order to assign group representatives. Without it the
// representative of a group would vary between runs.
type scopeSet struct {
	parent map[ScopeId]ScopeId
	order  []ScopeId
}

// union merges every scope in items into one class, transcribing upstream's `union`.
//
// The first item becomes the representative when none of the items is already in the set. An empty
// list is a no-op here; upstream raises an invariant on it, and the difference is deliberate for the
// reason `DisjointSet.Union` records: the call sites build the list conditionally.
func (set *scopeSet) union(items []ScopeId) {
	if len(items) == 0 {
		return
	}
	if set.parent == nil {
		set.parent = map[ScopeId]ScopeId{}
	}
	first := items[0]
	root, ok := set.find(first)
	if !ok {
		root = first
		set.parent[first] = first
		set.order = append(set.order, first)
	}
	for _, item := range items[1:] {
		itemParent, present := set.parent[item]
		if !present {
			set.parent[item] = root
			set.order = append(set.order, item)
			continue
		}
		if itemParent == root {
			continue
		}
		// Walk this item's chain to the root, repointing every link, exactly as upstream does. It
		// rewrites the whole path rather than only the item, which keeps the structure flat when two
		// established classes merge.
		current := item
		for itemParent != root {
			set.parent[current] = root
			current = itemParent
			itemParent = set.parent[current]
		}
	}
}

// find returns the representative of a scope's class, and whether the scope is in the set at all.
//
// Path compression on the way out, which is what keeps the recursion depth safe. The second return
// is upstream's `null`, kept separate from the id because zero is the absent ScopeId and conflating
// the two would make an absent scope read as a member of class zero.
func (set *scopeSet) find(item ScopeId) (ScopeId, bool) {
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
	root, ok := set.find(parent)
	if !ok {
		return 0, false
	}
	set.parent[item] = root
	return root, true
}
