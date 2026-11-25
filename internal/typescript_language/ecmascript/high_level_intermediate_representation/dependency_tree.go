package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// dependencyTree accumulates a scope's accesses and reduces them to a minimal set.
//
// Upstream's `ReactiveScopeDependencyTreeHIR`. Two trees, not one: `hoistable` is what may be read
// early, `roots` is what was actually read, and `addDependency` walks them in lockstep.
type dependencyTree struct {
	hoistable map[static_single_assignment.IdentifierId]*hoistableNode
	roots     map[static_single_assignment.IdentifierId]*dependencyNode
	rootOrder []static_single_assignment.IdentifierId
}

// addDependency records one access path, truncating it where the path stops being safe to hoist.
//
// # How a path truncates, and why truncation is the safe direction
//
// The loop walks the access path and the hoistable tree together. An entry survives only if the
// hoistable cursor says the object is already known non-null, or the entry itself is optional; a
// non-optional entry under an unknown object hits `break` and the path stops there.
//
// Under an empty hoistable set the cursor is nil at every step, so `props.a.b` truncates to bare
// `props`. That is not an approximation of upstream -- it is exactly what React does when handed an
// empty hoistable set (bundle 47019, the `else { break; }` arm), and it is the SAFE direction: the
// scope is reported as depending on all of `props` rather than on `props.a.b`, so it invalidates
// more often than strictly necessary and never less. `CollectScopeDependencies` is the spelling
// that still sees that; `CollectScopeDependenciesWithHoistable` supplies a real set and descends.
//
// # The open question this comment used to pose is answered, and it was not about this tree
//
// The question was why a bare access is recorded alongside the deep path, given the tree then emits
// the root and prunes the deep path under it as redundant -- upstream's own shape in
// `collectMinimalInSubtree`, and correct given the root is there.
//
// The root should not have been there. `t = propA` records `t -> propA` in the temporaries sidemap,
// and the instruction reading `t` resolves through that sidemap and records the full path. Visiting
// the intermediate instruction's operands as well recorded the prefix a second time, as a bare root.
// Upstream skips such an instruction entirely; see `isDeferredDependency`, now transcribed.
//
// Measured on the corpus, that duplicate submission was the whole gap: 71 bare roots were replaced
// by 147 specific paths, every added path an extension of a removed root and every removed root
// replaced. The path distribution moved from 237 deep / 2,205 flat to 386 / 2,136.
func (tree *dependencyTree) addDependency(dep ReactiveScopeDependency) {
	root, ok := tree.roots[dep.Identifier]
	if !ok {
		root = newDependencyNode(unconditionalAccess, dep.Reactive)
		tree.roots[dep.Identifier] = root
		tree.rootOrder = append(tree.rootOrder, dep.Identifier)
	}

	cursor := root
	var hoistableCursor *hoistableNode
	if seed, present := tree.hoistable[dep.Identifier]; present {
		hoistableCursor = seed
	}

	for _, entry := range dep.Path {
		var nextHoistable *hoistableNode
		var access propertyAccessType
		switch {
		case hoistableCursor != nil && hoistableCursor.nonNull:
			nextHoistable = hoistableCursor.properties[entry.Property]
			access = unconditionalAccess
		case entry.Optional:
			if hoistableCursor != nil {
				nextHoistable = hoistableCursor.properties[entry.Property]
			}
			access = optionalAccess
		default:
			// The path stops being hoistable here. See this function's comment: with no null
			// analysis this is reached on the first non-optional entry, which is upstream's own
			// behaviour under an empty hoistable set rather than a shortcut taken here.
			cursor.access = mergeAccess(cursor.access, optionalDependency)
			return
		}
		cursor = cursor.makeOrMergeProperty(entry.Property, access)
		hoistableCursor = nextHoistable
	}

	cursor.access = mergeAccess(cursor.access, optionalDependency)
}

// deriveMinimalDependencies returns the shallowest set of paths covering every access.
//
// A node marked as a dependency is emitted and its children are not walked, because depending on
// `props.a` already covers `props.a.b`. That pruning is the whole point of the tree: a scope reading
// `props.a`, `props.a.b` and `props.a.c` reports one dependency rather than three.
func (tree *dependencyTree) deriveMinimalDependencies() []ReactiveScopeDependency {
	var results []ReactiveScopeDependency
	for _, rootId := range tree.rootOrder {
		root := tree.roots[rootId]
		collectMinimalInSubtree(root, rootId, nil, &results)
	}
	return results
}
