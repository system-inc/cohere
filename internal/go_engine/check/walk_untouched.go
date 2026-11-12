package check

import (
	"golang.org/x/tools/go/packages"
)

// The walk's first stage (#154dr43, #qbvtkfm): an extra build leaves untouched every package the host's run kept whose
// own inputs are the build's and whose dependence set (DependenceSet) misses what changed, never judging or analyzing
// it, and hands it the host's findings. What changed, D, is every package of the build's listing the host kept no entry
// of, whose portable digest is not the host entry's, with list errors, with a reload pending, or that an earlier load's
// check declined. Before the load starts, affected is D, by path, grown to its least fixed point (untouchedOf): a
// package outside D whose set meets affected, or is the whole import closure and that closure meets affected, or reads
// its closure's membership and that closure holds a package of D whose imports or name changed
// (DependenceSet.ClosureMembership), is visited, and its path joins affected. A package the build's own findings cache
// holds is restored by its own key whatever the walk says, and is taken by its whole import closure, since its entry is
// the build's and the host's answers it read are not decoded for it. A visited package the host kept is decided late by
// TypesHold as before, and held or declined; every other one the host kept is untouched.
//
// Soundness, by induction in topological order. Hypothesis: every package outside affected renders in the build as on
// the host, declares the same facts and gives the same answers. Take one, P, and assume the hypothesis for every
// package below it. P is outside D, so its own inputs are the host's, and its set, every package its type reads, fact
// reads and answer reads could consult, misses affected, so every package of the set renders as on the host by the
// hypothesis; and when P reads its closure's membership, no package of its closure changed its imports or its name, so
// the closure holds the packages the host's did, under the same names. Every read of P then reads as on the host, which
// is exactly what the verdict on P would conclude, so the host's findings, facts and answers are P's in the build. The
// base case is a package whose set is empty, which reads nothing of another package. A held package, visited and
// judged, renders as on the host too: the same source, and every read holding.
//
// A package kept with no reads a verdict can judge, never recorded or never persisted, is untouched on another argument
// than that step, since no verdict on it concludes anything: its set is its whole import closure, so when it is
// untouched its own inputs are the host's and its whole import closure misses affected, every package it could reach
// renders as on the host, and its type check, facts and answers are the host's whatever it read. When its closure meets
// affected it is visited, and declined as it was before the walk (unrestorableReads).
//
// An untouched package loses only its root actions: no rule runs on it, and the build has no say on its files. When a
// package the build checks or visits imports it, the analyzers that learn facts run over it as over any import, and
// those actions restore its facts from the host's entry, on its types read from the host's export data.
//
// A superset of the exact rule, "the set meets D and the declines so far", it reads only D and the host's sets, so it
// is the same whatever order the packages are taken in. A decline at check reloads the build with the declined package
// in D, and the next load's fixed point is taken again: every package above it is outside D, and one whose set meets it
// is visited and judged again against it as checked from source, the rest untouched (decideHostRestores).

// walkCandidate is one package outside D the host's run kept: the package, and its dependence set, what its type and
// fact reads could consult with its answers' (Dependence with answersDependence), or its whole import closure for a
// package the build's own findings cache holds.
type walkCandidate struct {
	candidate  *packages.Package
	dependence DependenceSet
}

// untouchedOf are the candidates outside affected at the least fixed point over the build's changes: changed, the
// paths of D, rewritten, those of D whose own files differ from the host's, reshaped, those whose imports or name do,
// and listed, every path of the build's listing. A candidate is visited once its set meets affected: a path of it in
// affected, or one the build does not list, which has no package to render as the host's did; its whole import
// closure through its imports; a path a Closure lookup found nothing at, when its import closure holds that path's
// package and it is affected (DependenceSet.UnfoundPaths); a package whose deprecations it read, when that package's
// own files changed or the build does not list it (DependenceSet.OwnFactPaths); and its closure's membership, when
// the closure holds a reshaped package (DependenceSet.ClosureMembership). A visited candidate's own path joins
// affected. Test variants share their package's path, so a variant in affected affects what reads its package too,
// which only costs a visit.
func untouchedOf(
	candidates []walkCandidate,
	changes walkChanges,
	listed map[string]bool,
) map[*packages.Package]bool {
	affected := make(map[string]bool, len(changes.changed))
	for path := range changes.changed {
		affected[path] = true
	}
	// What changed is fixed, so whether each closure holds a reshaped package, or a package of an unfound path, is
	// asked once for the walk.
	membershipChanged := closureHolds(changes.reshaped)
	holdsPath := map[string]func(importer *packages.Package) bool{}
	inClosure := func(importer *packages.Package, path string) bool {
		holds, known := holdsPath[path]
		if !known {
			holds = closureHolds(map[string]bool{path: true})
			holdsPath[path] = holds
		}
		return holds(importer)
	}
	visited := map[*packages.Package]bool{}
	for growing := true; growing; {
		growing = false
		// Whether each package's closure reaches affected, as affected stands this round: one found false may turn
		// true as the round visits more, which the next round reads.
		reachesAffected := closureHolds(affected)
		for _, each := range candidates {
			if visited[each.candidate] {
				continue
			}
			meets := each.dependence.WholeClosure && reachesAffected(each.candidate) ||
				each.dependence.ClosureMembership && membershipChanged(each.candidate)
			for path := range each.dependence.Paths {
				meets = meets || affected[path] || !listed[path]
			}
			for path := range each.dependence.UnfoundPaths {
				meets = meets || affected[path] && inClosure(each.candidate, path)
			}
			for path := range each.dependence.OwnFactPaths {
				meets = meets || changes.rewritten[path] || !listed[path]
			}
			if !meets {
				continue
			}
			visited[each.candidate] = true
			affected[each.candidate.PkgPath] = true
			growing = true
		}
	}
	untouched := map[*packages.Package]bool{}
	for _, each := range candidates {
		if !visited[each.candidate] {
			untouched[each.candidate] = true
		}
	}
	return untouched
}

// walkChanges are what changed in a build, by path, as the walk reads it (untouchedOf): changed, D, every package with
// no host entry, list errors, a reload pending, or declined at an earlier load's check, or whose own files differ from
// the host's; rewritten, those of them whose own files differ or are not known to be the
// host's; and reshaped, those of them whose imports or name differ or are not known to be the host's.
type walkChanges struct {
	changed   map[string]bool
	rewritten map[string]bool
	reshaped  map[string]bool
}

// closureHolds is whether an importer's import closure holds a package of a path in paths, each importer's answer
// kept once asked, as paths stands then.
func closureHolds(paths map[string]bool) func(importer *packages.Package) bool {
	answers := map[*packages.Package]bool{}
	var holds func(importer *packages.Package) bool
	holds = func(importer *packages.Package) bool {
		if known, isKnown := answers[importer]; isKnown {
			return known
		}
		answers[importer] = false
		for _, imported := range importer.Imports {
			if paths[imported.PkgPath] || holds(imported) {
				answers[importer] = true
				return true
			}
		}
		return false
	}
	return holds
}

// unitedDependence is the union of sets: the whole import closure when any is, and every path of each otherwise, with
// the closure's membership when any reads it, and every unfound and own fact path of each.
func unitedDependence(sets ...DependenceSet) DependenceSet {
	united := DependenceSet{Paths: map[string]bool{}, UnfoundPaths: map[string]bool{}, OwnFactPaths: map[string]bool{}}
	for _, set := range sets {
		if set.WholeClosure {
			return DependenceSet{WholeClosure: true}
		}
		for path := range set.Paths {
			united.Paths[path] = true
		}
		for path := range set.UnfoundPaths {
			united.UnfoundPaths[path] = true
		}
		for path := range set.OwnFactPaths {
			united.OwnFactPaths[path] = true
		}
		united.ClosureMembership = united.ClosureMembership || set.ClosureMembership
	}
	return united
}
