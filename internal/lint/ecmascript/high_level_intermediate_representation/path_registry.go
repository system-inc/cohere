package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// pathRegistry interns every access path seen, upstream's `PropertyPathRegistry`.
//
// This lives here rather than in `dependencies.go` because it belongs to this machinery: upstream
// declares it inside `CollectHoistablePropertyLoads.ts`, which is this pass, and the dependency tree
// in `dependencies.go` is a different structure that happens to also be a property trie. Conflating
// them is easy and was the seam error that sent one agent back; the registry indexes paths for this
// dataflow, the dependency tree reduces accesses to a minimal set.
type pathRegistry struct {
	nodes []pathNode
	roots map[static_single_assignment.IdentifierId]int
}

// identifierNode returns the interned node for a bare root, creating it if absent.
func (registry *pathRegistry) identifierNode(id static_single_assignment.IdentifierId, reactive bool) int {
	if index, ok := registry.roots[id]; ok {
		return index
	}
	index := len(registry.nodes)
	registry.nodes = append(registry.nodes, pathNode{
		properties:         map[string]int{},
		optionalProperties: map[string]int{},
		fullPath:           ReactiveScopeDependency{Identifier: id, Reactive: reactive},
	})
	registry.roots[id] = index
	return index
}

// propertyNode returns the interned node one step below parent, creating it if absent.
//
// The new node's `fullPath` is the parent's path with the entry appended, copied rather than shared:
// two children of one parent must not alias a backing array, or extending one would corrupt the
// other.
func (registry *pathRegistry) propertyNode(parent int, entry DependencyPathEntry) int {
	table := registry.nodes[parent].properties
	if entry.Optional {
		table = registry.nodes[parent].optionalProperties
	}
	if index, ok := table[entry.Property]; ok {
		return index
	}

	parentPath := registry.nodes[parent].fullPath
	path := make([]DependencyPathEntry, len(parentPath.Path), len(parentPath.Path)+1)
	copy(path, parentPath.Path)
	path = append(path, entry)

	index := len(registry.nodes)
	registry.nodes = append(registry.nodes, pathNode{
		properties:         map[string]int{},
		optionalProperties: map[string]int{},
		fullPath: ReactiveScopeDependency{
			Identifier: parentPath.Identifier,
			Reactive:   parentPath.Reactive,
			Path:       path,
		},
		hasOptional: registry.nodes[parent].hasOptional || entry.Optional,
	})
	table[entry.Property] = index
	return index
}

// pathIndex interns a whole dependency, returning the node naming it.
func (registry *pathRegistry) pathIndex(dep ReactiveScopeDependency) int {
	current := registry.identifierNode(dep.Identifier, dep.Reactive)
	for _, entry := range dep.Path {
		current = registry.propertyNode(current, entry)
	}
	return current
}
