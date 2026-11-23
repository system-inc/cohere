package check

import (
	"go/types"
	"maps"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// walkedRestoredBelow is restoredBelow as it was before its memo: a walk of every import from candidate, each package
// reached that the load restored from the host's run, and that is closure's at its path, by its host types.
func walkedRestoredBelow(
	state *hostState,
	candidate *packages.Package,
	closure map[string]*types.Package,
) map[*types.Package]*types.Package {
	visited := map[*packages.Package]bool{}
	var reached []*packages.Package
	var collect func(importer *packages.Package)
	collect = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if visited[imported] {
				continue
			}
			visited[imported] = true
			reached = append(reached, imported)
			collect(imported)
		}
	}
	collect(candidate)
	restored := map[*types.Package]*types.Package{}
	for _, imported := range reached {
		hosted, judged := state.restoring[imported]
		if !judged {
			hosted = state.untouched[imported]
		}
		if hosted != nil && imported.Types != nil && closure[imported.PkgPath] == imported.Types {
			restored[imported.Types] = hosted.kept.types
		}
	}
	return restored
}

// The restores below a package, made once a load from its imports' own (restoredReach), are what a walk of its imports
// finds, for every package of a graph that forks and joins, through a chain of single imports, past a package checked
// from source, and with two packages of one path below one candidate, as a package and its test variant are, under a
// closure that names either of them. Asked again, the memo answers the same.
func TestTheRestoresBelowAPackageAreWhatAWalkOfItsImportsFinds(test *testing.T) {
	test.Parallel()

	byName := map[string]*packages.Package{}
	for _, name := range []string{"top", "left", "right", "chain", "link", "shared", "variant", "checked", "leaf"} {
		path := "example.com/" + name
		// The variant is the shared package's test variant, of its path.
		if name == "variant" {
			path = "example.com/shared"
		}
		byName[name] = &packages.Package{ID: name, PkgPath: path, Types: types.NewPackage(path, name),
			Imports: map[string]*packages.Package{}}
	}
	imports := map[string][]string{
		"top":     {"left", "right", "chain"},
		"left":    {"shared", "checked"},
		"right":   {"variant", "checked"},
		"chain":   {"link"},
		"link":    {"leaf"},
		"shared":  {"leaf"},
		"variant": {"leaf"},
		"checked": {"leaf"},
	}
	for importer, imported := range imports {
		for _, name := range imported {
			byName[importer].Imports[name] = byName[name]
		}
	}
	hostOf := func(name string) *hostRestore {
		return &hostRestore{kept: &hostEntry{types: types.NewPackage(byName[name].PkgPath, name)}}
	}
	state := &hostState{below: &sync.Map{}, restoring: map[*packages.Package]*hostRestore{},
		untouched: map[*packages.Package]*hostRestore{}}
	for _, name := range []string{"left", "chain", "link", "shared", "variant", "leaf"} {
		state.restoring[byName[name]] = hostOf(name)
	}
	state.untouched[byName["right"]] = hostOf("right")
	session := &CacheSession{host: state}
	closureNaming := func(candidate *packages.Package, sharedBy string) map[string]*types.Package {
		closure := map[string]*types.Package{}
		var collect func(importer *packages.Package)
		collect = func(importer *packages.Package) {
			for _, imported := range importer.Imports {
				closure[imported.PkgPath] = imported.Types
				collect(imported)
			}
		}
		collect(candidate)
		if _, reaches := closure["example.com/shared"]; reaches {
			closure["example.com/shared"] = byName[sharedBy].Types
		}
		return closure
	}
	for range 2 {
		for name, candidate := range byName {
			for _, sharedBy := range []string{"shared", "variant"} {
				closure := closureNaming(candidate, sharedBy)
				want := walkedRestoredBelow(state, candidate, closure)
				if got := session.restoredBelow(candidate, closure); !maps.Equal(got, want) {
					test.Errorf("below %s, its closure naming the %s: want %d restored, got %d: %v, want %v", name,
						sharedBy, len(want), len(got), got, want)
				}
			}
		}
	}
}
