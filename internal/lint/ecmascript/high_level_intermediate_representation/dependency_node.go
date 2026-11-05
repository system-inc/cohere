package high_level_intermediate_representation

// dependencyNode is one value in the access-path tree a scope's dependencies form.
//
// `properties` is ordered rather than a bare map because `deriveMinimalDependencies` walks it and
// the walk's order becomes the output's order. Upstream uses a JS `Map`, which is insertion-ordered;
// a Go map is randomised, so insertion order is kept explicitly in `keys`.
type dependencyNode struct {
	properties map[string]*dependencyNode
	keys       []string
	access     propertyAccessType
	reactive   bool
}

// makeOrMergeProperty is upstream's `makeOrMergeProperty`: descend one step, creating or merging.
func (node *dependencyNode) makeOrMergeProperty(property string, access propertyAccessType) *dependencyNode {
	if existing, ok := node.properties[property]; ok {
		existing.access = mergeAccess(existing.access, access)
		return existing
	}
	child := newDependencyNode(access, node.reactive)
	node.properties[property] = child
	node.keys = append(node.keys, property)
	return child
}
