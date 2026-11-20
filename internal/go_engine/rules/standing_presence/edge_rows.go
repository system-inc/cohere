package standing_presence

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// presenceEdge is one read of a function's body that a rule makes from another's, in a package's syntax: caller, the
// declaration the read starts in, nil for a package-level initializer, and callee, the declaration whose body is read,
// with the reason of the row whose rule reads it.
type presenceEdge struct {
	caller *ast.FuncDecl
	callee *ast.FuncDecl
	reason fact_reach.Reason
}

// edgesLeaving are the bodies the edge rows take out of standing, by both directions of each edge a rule reads from
// one body into another's:
//   - the callee direction, in the build's own syntax: a body the build holds present that starts a function of the
//     package with go, or hands one to (*testing.T).Run by name, has that function's body read by the rule, so the
//     function's body leaves;
//   - the caller direction, in the host's record (fact_reach.Host.Presence): a standing body that starts a function
//     whose body does not stand, one that left or never stood, is a body the rule reads that function's from, and
//     once stripped the rule would read nothing, so it leaves too.
//
// Present is what does not stand, what an earlier row takes out, the users of each object named and each body
// leaving, and, to a fixed point, each body this takes out, whose own edges are then read too. A package-level
// initializer never stands, so an edge it starts is always read. Each round takes a body out of standing or ends, so
// the rounds end within as many as standing holds.
func edgesLeaving(
	syntax packageSyntax,
	host fact_reach.Host,
	records map[fact_reach.Place]fact_reach.PresenceRecord,
	standing map[fact_reach.Place]bool,
	named []fact_reach.Named,
	leaving []Leaving,
) []Leaving {
	declarations := functionDeclarations(syntax)
	edges := slices.Concat(testingGoroutineEdges(syntax, declarations), requireParallelEdges(syntax, declarations))
	var callers []fact_reach.Place
	for place, record := range records {
		if len(record.GoStarts)+len(record.RunStarts) > 0 {
			callers = append(callers, place)
		}
	}
	slices.SortFunc(callers, comparePlace)
	if len(edges) == 0 && len(callers) == 0 {
		return nil
	}
	left := map[fact_reach.Place]bool{}
	for _, body := range leaving {
		left[body.Place] = true
	}
	for _, object := range named {
		for _, user := range host.Users(object.Object) {
			left[user] = true
		}
	}
	isStanding := func(place fact_reach.Place) bool { return standing[place] && !left[place] }
	// The body of each function the host declares, by its key, which the caller direction asks whether it stands.
	bodies := map[fact_reach.Object]fact_reach.Place{}
	if len(callers) > 0 {
		for _, place := range host.Places() {
			for _, key := range host.Declared(place) {
				bodies[key] = place
			}
		}
	}
	// startsAway is whether one of started's functions has no body that stands.
	startsAway := func(started []fact_reach.Object) bool {
		return slices.ContainsFunc(started, func(key fact_reach.Object) bool {
			place, isDeclared := bodies[key]
			return !isDeclared || !isStanding(place)
		})
	}
	var taken []Leaving
	take := func(place fact_reach.Place, reason fact_reach.Reason) {
		left[place] = true
		taken = append(taken, Leaving{Place: place, Reason: reason})
	}
	for changed := true; changed; {
		changed = false
		for _, each := range edges {
			callee := placeOf(syntax, each.callee)
			if (each.caller != nil && isStanding(placeOf(syntax, each.caller))) || !isStanding(callee) {
				continue
			}
			take(callee, each.reason)
			changed = true
		}
		for _, caller := range callers {
			if !isStanding(caller) {
				continue
			}
			switch {
			case startsAway(records[caller].GoStarts):
				take(caller, TestingGoroutine.Reason())
			case startsAway(records[caller].RunStarts):
				take(caller, RequireParallel.Reason())
			default:
				continue
			}
			changed = true
		}
	}
	return taken
}

// Reread is what the edge rows take out of standing, what still stands once the facts have settled
// (fact_reach.Settle), read in both directions again: a body that left only for a differing fact may start a function
// that still stands, or be started by one. The caller settles again with what this takes out as reached, and asks
// again until this takes nothing.
func Reread(pass *analysis.Pass, host fact_reach.Host, standing map[fact_reach.Place]bool) []Leaving {
	return edgesLeaving(syntaxOf(pass), host, presenceRecords(host), standing, nil, nil)
}

// presenceRecords are the host's records of the bodies that hold anything presence reads, by place.
func presenceRecords(host fact_reach.Host) map[fact_reach.Place]fact_reach.PresenceRecord {
	records := map[fact_reach.Place]fact_reach.PresenceRecord{}
	for _, place := range host.Places() {
		record := host.Presence(place)
		if len(record.GoStarts)+len(record.RunStarts)+len(record.LocalMarkers) > 0 || record.OwnAny {
			records[place] = record
		}
	}
	return records
}

// functionDeclarations are the declarations of syntax's files of each function and method they declare, by its
// object, as both edge rules look a callee's up (rules/vet/testing_goroutine/functions.go:11-30,
// rules/testing_require_parallel/analyzer.go:75-84).
func functionDeclarations(syntax packageSyntax) map[*types.Func]*ast.FuncDecl {
	declarations := map[*types.Func]*ast.FuncDecl{}
	for _, file := range syntax.files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			if declared, isDeclared := syntax.information.Defs[function.Name].(*types.Func); isDeclared {
				declarations[declared] = function
			}
		}
	}
	return declarations
}

// placeOf is declaration's place as the standing set keys one (fact_reach.Place): the file a reader sees, past any
// `//line` directive, and the offset of its `func` in the file as parsed.
func placeOf(syntax packageSyntax, declaration *ast.FuncDecl) fact_reach.Place {
	position := syntax.fileSet.Position(declaration.Pos())
	return fact_reach.Place{File: position.Filename, Offset: position.Offset}
}
