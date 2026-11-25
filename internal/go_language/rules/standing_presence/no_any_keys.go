package standing_presence

import (
	"go/ast"
	"go/types"
	"slices"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/rules/consistency_no_any"
)

// noAnyKeys are the declarations the package's candidates keep, closed under the rule's flow, as the rule keys each,
// by its generic origin: the functions a parameter or result key belongs to, the variables kept, fields, parameters,
// results and package-level variables among them, the names of the interface methods kept, and the function
// literals kept, which have no object.
type noAnyKeys struct {
	functions   map[*types.Func]bool
	variables   map[*types.Var]bool
	methodNames map[string]bool
	literals    map[*ast.FuncLit]bool
}

// named is what keys take out of the standing set (noAnyBound), each with noany's reason.
func (keys noAnyKeys) named(
	host fact_reach.Host,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
) ([]fact_reach.Named, []Leaving) {
	var named []fact_reach.Named
	var leaving []Leaving
	if len(keys.functions) > 0 {
		// The place of each function the host declares with a body, by its key, as buildOrderNamed maps one to the
		// other.
		places := map[fact_reach.Object]fact_reach.Place{}
		for _, place := range host.Places() {
			for _, key := range host.Declared(place) {
				places[key] = place
			}
		}
		for _, function := range host.Functions() {
			found, isFound := find(function.Identity)
			declared, isFunction := found.(*types.Func)
			if !isFound || !isFunction || !keys.functions[declared.Origin()] {
				continue
			}
			named = append(named, fact_reach.Named{Object: function.Object, Reason: NoAny.Reason()})
			if place, isPlaced := places[function.Object]; isPlaced {
				leaving = append(leaving, Leaving{Place: place, Reason: NoAny.Reason()})
			}
		}
	}
	for _, object := range used {
		found, isFound := find(object.Identity)
		if !isFound {
			continue
		}
		isKey := false
		switch declared := found.(type) {
		case *types.Var:
			isKey = keys.variables[declared.Origin()]
		case *types.TypeName:
			isKey = holdsKeyField(declared.Type(), keys.variables)
		}
		if isKey {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: NoAny.Reason()})
		}
	}
	if len(keys.methodNames) > 0 {
		named = append(named, methodsNamed(used, keys.methodNames, find, NoAny.Reason())...)
	}
	return named, leaving
}

// close adds to keys every declaration the rule's flow routes to one, to a fixed point, and is whether any was added:
// one defined from a key, since a place fixing it fixes the key (settle spreads sinkFixed back along definedFrom), and
// one that feeds a key, since a value from elsewhere given to it fixes the key (settle spreads sourceFixed forward
// along feeds).
func (keys noAnyKeys) close(
	edges consistency_no_any.FlowEdges,
	owners map[*types.Var]consistency_no_any.FlowNode,
) bool {
	grew := false
	for changed := true; changed; {
		changed = false
		for _, table := range []map[consistency_no_any.FlowNode][]consistency_no_any.FlowNode{edges.DefinedFrom,
			edges.Feeds} {
			for declaration, reached := range table {
				if keys.holds(declaration) || !slices.ContainsFunc(reached, keys.holds) {
					continue
				}
				keys.add(declaration, owners)
				changed, grew = true, true
			}
		}
	}
	return grew
}

// holds is whether node is a key.
func (keys noAnyKeys) holds(node consistency_no_any.FlowNode) bool {
	if node.Literal != nil {
		return keys.literals[node.Literal]
	}
	switch declared := node.Object.(type) {
	case *types.Var:
		return keys.variables[declared.Origin()]
	case *types.Func:
		if receiver := declared.Signature().Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
			return keys.methodNames[declared.Name()]
		}
		return keys.functions[declared.Origin()]
	}
	return false
}

// add makes node a key, and the function or literal a parameter or result key belongs to one too.
func (keys noAnyKeys) add(node consistency_no_any.FlowNode, owners map[*types.Var]consistency_no_any.FlowNode) {
	if node.Literal != nil {
		keys.literals[node.Literal] = true
		return
	}
	switch declared := node.Object.(type) {
	case *types.Var:
		keys.variables[declared.Origin()] = true
		if owner, isOwned := owners[declared.Origin()]; isOwned {
			keys.add(owner, owners)
		}
	case *types.Func:
		if receiver := declared.Signature().Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
			keys.methodNames[declared.Name()] = true
			return
		}
		keys.functions[declared.Origin()] = true
	}
}
