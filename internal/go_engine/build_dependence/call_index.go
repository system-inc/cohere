package build_dependence

import (
	"slices"
)

// CallIndex is the units the facts census counts, the host's graph's and then the standard library's, with the calls
// between them, as every build shares them (#f9br7sg): each graph's own static calls by their index here, every call of
// a host unit into the standard library found by its callee's path and local name, a live record's and one read back
// from the store alike (#6a3kjvz), as one graph holding both would have found it, and each unit's callers. Nothing in
// it is a build's: a build's judgment adds only each unit's verdict (Dependent).
//
// It is built on each graph's resolution, the one every build's judgment of that graph is made on (Against): a graph
// resolves once, and again only after a package is added, which no graph is once its first build is judged, so every
// build's Dependence of a graph holds the resolution the index was built on, and its units' indexes are the index's.
type CallIndex struct {
	host *resolvedGraph
	// standardLibrary is the standard library's graph's resolution, nil when the host has none.
	standardLibrary *resolvedGraph
	calls           [][]int
	callers         [][]int
}

// NewCallIndex is the call index over host's units and then standardLibrary's, standardLibrary nil when the host has
// no graph of the standard library, whose units the index then leaves out.
func NewCallIndex(host *Graph, standardLibrary *Graph) *CallIndex {
	index := &CallIndex{host: host.resolve()}
	hostUnits := index.host.unitCount()
	if standardLibrary != nil {
		index.standardLibrary = standardLibrary.resolve()
	}
	index.calls = make([][]int, 0, hostUnits+index.standardLibraryUnits())
	for unit := range hostUnits {
		calls := make([]int, 0, len(index.host.calls.row(unit)))
		for _, callee := range index.host.calls.row(unit) {
			calls = append(calls, int(callee))
		}
		index.calls = append(index.calls, calls)
	}
	if index.standardLibrary != nil {
		for unit := range index.standardLibrary.unitCount() {
			calls := make([]int, 0, len(index.standardLibrary.calls.row(unit)))
			for _, callee := range index.standardLibrary.calls.row(unit) {
				calls = append(calls, hostUnits+int(callee))
			}
			index.calls = append(index.calls, calls)
		}
		for unit := range hostUnits {
			for _, callee := range index.host.unitExternalCalls[unit] {
				declaring, isDeclared := index.standardLibrary.unitDeclaringName(callee.Path, callee.Local)
				if isDeclared && !slices.Contains(index.calls[unit], hostUnits+declaring) {
					// A copy, since the host's graph holds the slice the index began with.
					index.calls[unit] = append(slices.Clone(index.calls[unit]), hostUnits+declaring)
				}
			}
		}
	}
	index.callers = make([][]int, len(index.calls))
	for unit, calls := range index.calls {
		for _, callee := range calls {
			index.callers[callee] = append(index.callers[callee], unit)
		}
	}
	return index
}

// standardLibraryUnits counts the standard library's graph's units, none when the host has no such graph.
func (index *CallIndex) standardLibraryUnits() int {
	if index.standardLibrary == nil {
		return 0
	}
	return index.standardLibrary.unitCount()
}

// UnitCount counts the index's units, which its indexes run below.
func (index *CallIndex) UnitCount() int {
	return len(index.calls)
}

// resolvedUnit is the graph that holds the unit of the index's unit index, and the unit's index there.
func (index *CallIndex) resolvedUnit(unit int) (*resolvedGraph, int) {
	if unit < index.host.unitCount() {
		return index.host, unit
	}
	return index.standardLibrary, unit - index.host.unitCount()
}

// Nodes is unit's size in syntax nodes.
func (index *CallIndex) Nodes(unit int) int {
	graph, graphUnit := index.resolvedUnit(unit)
	return int(graph.unitNodes[graphUnit])
}

// Workspace reports whether unit's package is the workspace's.
func (index *CallIndex) Workspace(unit int) bool {
	graph, graphUnit := index.resolvedUnit(unit)
	return graph.packages[graph.unitPackage[graphUnit]].workspace
}

// Calls are the units unit calls statically, by their index here, each once. The slice is the index's, read and never
// written.
func (index *CallIndex) Calls(unit int) []int {
	return index.calls[unit]
}

// Callers are the units that call unit statically, by their index here, each once, in the order of their indexes.
// The slice is the index's, read and never written.
func (index *CallIndex) Callers(unit int) []int {
	return index.callers[unit]
}

// Dependent reports whether a build makes unit dependent, read from the build's judgment of each graph: host is its
// judgment of the host's graph, and standardLibrary of the standard library's, nil when the index has no such graph.
func (index *CallIndex) Dependent(host *Dependence, standardLibrary *Dependence, unit int) bool {
	if unit < index.host.unitCount() {
		return host.unitDependent[unit]
	}
	return standardLibrary.unitDependent[unit-index.host.unitCount()]
}

// UnitContaining is the index here of the unit of the package packageId whose span in file, the one a reader sees,
// holds offset, read from the graph that holds the package, and false when none does (Dependence.UnitContaining).
func (index *CallIndex) UnitContaining(packageId string, file string, offset int) (int, bool) {
	if unit, isUnit := index.host.unitContaining(packageId, file, offset); isUnit {
		return unit, true
	}
	if index.standardLibrary == nil {
		return 0, false
	}
	unit, isUnit := index.standardLibrary.unitContaining(packageId, file, offset)
	return index.host.unitCount() + unit, isUnit
}
