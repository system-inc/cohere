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

	// calls are the units each unit calls, and callers the units that call each, by their index here, a row each.
	calls   packedRows[int32]
	callers packedRows[int32]
}

// NewCallIndex is the call index over host's units and then standardLibrary's, standardLibrary nil when the host has
// no graph of the standard library, whose units the index then leaves out. With no such graph a unit's calls are the
// host's graph's, which the index shares, read and never written; with one, nearly every host unit gains a call into
// it, so the index holds calls of its own. A declarations-only graph, either of the two, panics
// (refuseDeclarationsOnly), having collected no call.
func NewCallIndex(host *Graph, standardLibrary *Graph) *CallIndex {
	refuseDeclarationsOnly(host.declarationsOnly || standardLibrary != nil && standardLibrary.declarationsOnly,
		"build a call index over", "the index would hold none of that graph's units' calls, and the verdicts it reads "+
			"(CallIndex.Dependent) would rest on declared objects alone")
	index := &CallIndex{host: host.resolve()}
	if standardLibrary == nil {
		index.calls = index.host.calls
	} else {
		index.standardLibrary = standardLibrary.resolve()
		// Each graph's units fit an int32 (resolveUnits), and the two together are checked to as well, so no index
		// here, a host unit's or hostUnits plus a standard library unit's, wraps.
		units := narrowed[int32](index.host.unitCount()+index.standardLibrary.unitCount(), "the call index's units")
		hostUnits := int32(index.host.unitCount())
		index.calls = newPackedRows[int32](int(units),
			len(index.host.calls.entries)+len(index.host.externalCalls.entries)+
				len(index.standardLibrary.calls.entries))
		for unit := range index.host.unitCount() {
			index.calls.entries = append(index.calls.entries, index.host.calls.row(unit)...)
			for _, callee := range index.host.externalCalls.row(unit) {
				named := &index.host.externalObjects[callee]
				declaring, isDeclared := index.standardLibrary.unitDeclaringName(named.path, named.local)
				if isDeclared && !slices.Contains(index.calls.open(), hostUnits+int32(declaring)) {
					index.calls.entries = append(index.calls.entries, hostUnits+int32(declaring))
				}
			}
			index.calls.closeRow()
		}
		for unit := range index.standardLibrary.unitCount() {
			for _, callee := range index.standardLibrary.calls.row(unit) {
				index.calls.entries = append(index.calls.entries, hostUnits+callee)
			}
			index.calls.closeRow()
		}
	}
	callers := make([]rowEntry, 0, len(index.calls.entries))
	for unit := range index.calls.rowCount() {
		for _, callee := range index.calls.row(unit) {
			callers = append(callers, rowEntry{row: callee, entry: int32(unit)})
		}
	}
	index.callers = groupedRows(index.calls.rowCount(), callers)
	return index
}

// UnitCount counts the index's units, which its indexes run below.
func (index *CallIndex) UnitCount() int {
	return index.calls.rowCount()
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
func (index *CallIndex) Calls(unit int) []int32 {
	return index.calls.row(unit)
}

// Callers are the units that call unit statically, by their index here, each once, in the order of their indexes.
// The slice is the index's, read and never written.
func (index *CallIndex) Callers(unit int) []int32 {
	return index.callers.row(unit)
}

// Dependent reports whether a build makes unit dependent, read from the build's judgment of each graph: host is its
// judgment of the host's graph, and standardLibrary of the standard library's, nil when the index has no such graph.
// A declarations-only graph's judgment, either of the two, panics (refuseDeclarationsOnly), having judged no unit.
func (index *CallIndex) Dependent(host *Dependence, standardLibrary *Dependence, unit int) bool {
	refuseDeclarationsOnly(host.declarationsOnly || standardLibrary != nil && standardLibrary.declarationsOnly,
		"read a unit's verdict through a call index from", "the verdict would rest on the unit's declared objects "+
			"alone")
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
