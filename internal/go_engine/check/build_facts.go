package check

import (
	"reflect"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// buildFacts are the facts the build's packages exported, by owner, as an importer inherits them, each owner's
// gathered on first use: a restored package's from its entry, the host's or the build's own, and an analyzed one's from
// the actions on it, each of its analyzer's fact types available only when one ran there.
type buildFacts struct {
	session   *CacheSession
	actionsOf map[*packages.Package][]*checker.Action
	byOwner   map[*packages.Package]ownerFacts
}

// ownerFacts are one owner's exported facts by type, then by objectpath, the empty path its package's own, and the
// fact types it has an answer for, found or not.
type ownerFacts struct {
	facts     map[reflect.Type]map[objectpath.Path]analysis.Fact
	available map[reflect.Type]bool
}

// newBuildFacts is the facts of session's packages, as the actions of graphs left them.
func newBuildFacts(session *CacheSession, graphs []*checker.Graph) *buildFacts {
	actionsOf := map[*packages.Package][]*checker.Action{}
	for _, graph := range graphs {
		if graph == nil {
			continue
		}
		for action := range graph.All() {
			actionsOf[action.Package] = append(actionsOf[action.Package], action)
		}
	}
	return &buildFacts{session: session, actionsOf: actionsOf, byOwner: map[*packages.Package]ownerFacts{}}
}

// of is owner's exported facts.
func (exported *buildFacts) of(owner *packages.Package) ownerFacts {
	if known, isKnown := exported.byOwner[owner]; isKnown {
		return known
	}
	gathered := ownerFacts{facts: map[reflect.Type]map[objectpath.Path]analysis.Fact{},
		available: map[reflect.Type]bool{}}
	add := func(path objectpath.Path, fact analysis.Fact) {
		factType := reflect.TypeOf(fact)
		if gathered.facts[factType] == nil {
			gathered.facts[factType] = map[objectpath.Path]analysis.Fact{}
		}
		gathered.facts[factType][path] = fact
	}
	if restored, hit := exported.session.hits[owner]; hit {
		for analyzer, analysisRestored := range restored.analyses {
			for _, factType := range analyzer.FactTypes {
				gathered.available[reflect.TypeOf(factType)] = true
			}
			for _, objectFact := range analysisRestored.objectFacts {
				add(objectFact.path, objectFact.fact)
			}
			for _, packageFact := range analysisRestored.packageFacts {
				add("", packageFact)
			}
		}
	} else {
		var encoder objectpath.Encoder
		for _, action := range exported.actionsOf[owner] {
			if action.Err != nil {
				continue
			}
			for _, factType := range action.Analyzer.FactTypes {
				gathered.available[reflect.TypeOf(factType)] = true
			}
			objectFacts, packageFacts := action.ExportedFacts()
			for _, objectFact := range objectFacts {
				if path, pathError := encoder.For(objectFact.Object); pathError == nil {
					add(path, objectFact.Fact)
				}
			}
			for _, packageFact := range packageFacts {
				add("", packageFact.Fact)
			}
		}
	}
	exported.byOwner[owner] = gathered
	return gathered
}

// factsHold is why candidate's fact reads, restoring's, do not read the same in what the build's packages exported, or
// empty when they do. A read of an owner that exported no facts of the type in this build, since no action of the
// type ran on it, cannot be asked, and fails it.
func (exported *buildFacts) factsHold(candidate *packages.Package, restoring Restoring, state *hostState) string {
	byPath := map[string]*packages.Package{}
	var collect func(importer *packages.Package)
	collect = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if _, collected := byPath[imported.PkgPath]; !collected {
				byPath[imported.PkgPath] = imported
				collect(imported)
			}
		}
	}
	collect(candidate)
	unavailable := ""
	lookup := func(owner string, path objectpath.Path, factType reflect.Type) (analysis.Fact, bool) {
		declaring := byPath[owner]
		if declaring == nil {
			return nil, false
		}
		facts := exported.of(declaring)
		if !facts.available[factType] && unavailable == "" {
			unavailable = "F the build has no " + factType.String() + " facts of " + owner + " to ask"
		}
		fact, found := facts.facts[factType][path]
		return fact, found
	}
	started := time.Now()
	holds, why := FactsHold(restoring, load.ClosureTypes(candidate), state.host.renderings, state.renderings, lookup,
		ReadFactTypes(restoring))
	state.deciding.Add(int64(time.Since(started)))
	switch {
	case unavailable != "":
		return unavailable
	case !holds:
		return why
	}
	return ""
}
