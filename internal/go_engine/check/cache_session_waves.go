package check

import (
	"fmt"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// closureDigest is one package's closure digest, memoized, and whether it is known: it is not while the output of
// the package, or of anything below it, is.
type closureDigest struct {
	digest cache.Key
	known  bool
}

// firstWave is the early cutoff's first wave. A package's key holds its dependencies' keys, so an edit misses every
// package above the one edited, though most of them would come out of an analysis exactly as before. So when a miss
// waits on another, the misses with no miss below them, whose own inputs moved, are analyzed first and saved; then
// each package that waited is restored by its second key, its own inputs and the output digests of everything below
// it, when that key holds an entry (decideBySecondKey). Whatever still misses is analyzed together by the run's one
// more wave, so a run analyzes at most twice however deep the misses go.
//
// The packages to analyze among the first wave get analyzers; the rest get the analyzers that learn facts, as a run
// gives a dependency, which is all an entry of theirs holds. A package with a load error waits for the second wave:
// it gets no entry, so nothing above it can be restored. It returns the graph of the first wave's packages to
// analyze, or nil when the run has one wave.
func (session *CacheSession) firstWave(
	analyzers []*analysis.Analyzer,
	options *checker.Options,
	withheld map[*packages.Package]load.PackageWithheld,
) (*checker.Graph, error) {
	if session.known == nil || session.keys == nil {
		return nil, nil
	}
	missBelow := map[*packages.Package]bool{}
	visited := map[*packages.Package]bool{}
	var below func(current *packages.Package) bool
	below = func(current *packages.Package) bool {
		if visited[current] {
			return missBelow[current]
		}
		visited[current] = true
		for _, imported := range current.Imports {
			if below(imported) || !session.restored(imported) {
				missBelow[current] = true
			}
		}
		return missBelow[current]
	}
	importedByMiss := map[*packages.Package]bool{}
	for keyed := range session.keys {
		if !session.restored(keyed) {
			for _, imported := range keyed.Imports {
				importedByMiss[imported] = true
			}
		}
	}
	waiting := false
	var first []*packages.Package
	var firstGiven []*packages.Package
	var firstOthers []*packages.Package
	for keyed := range session.keys {
		if session.restored(keyed) {
			continue
		}
		switch {
		case below(keyed):
			waiting = true
		case len(keyed.Errors) > 0 || keyed.IllTyped:
		case session.given[keyed]:
			first = append(first, keyed)
			firstGiven = append(firstGiven, keyed)
		// A dependency no miss imports is no use to the second key, which asks only what is below a miss.
		case importedByMiss[keyed]:
			first = append(first, keyed)
			firstOthers = append(firstOthers, keyed)
		}
	}
	if !waiting || len(first) == 0 {
		return nil, nil
	}
	// The first wave's packages keep their syntax, since one whose entry does not encode is analyzed again by the
	// second wave, and its walk is read from the syntax below.
	firstOptions := *options
	firstOptions.Finished = nil
	session.counts.Waves++
	var graphs []*checker.Graph
	var givenGraph *checker.Graph
	if len(firstGiven) > 0 {
		graph, analyzeError := checker.Analyze(analyzers, firstGiven, &firstOptions)
		if analyzeError != nil {
			return nil, fmt.Errorf("analyzing %d packages: %w", len(firstGiven), analyzeError)
		}
		givenGraph = graph
		graphs = append(graphs, graph)
	}
	if len(firstOthers) > 0 {
		graph, analyzeError := checker.Analyze(session.known.facts, firstOthers, &firstOptions)
		if analyzeError != nil {
			return nil, fmt.Errorf("analyzing %d dependencies: %w", len(firstOthers), analyzeError)
		}
		graphs = append(graphs, graph)
	}
	// The first wave's walks let go of the packages they walked as each ends, which must not overlap the second wave's
	// actions on the same packages, so this wave's walks end here, on a run with the store alone.
	options.Walks.Wait()
	files := map[*packages.Package][]storedFile{}
	for _, analyzedPackage := range firstGiven {
		files[analyzedPackage] = storedFiles(analyzedPackage)
	}
	// A package whose entry did not encode, or on which an action failed, is analyzed again by the second wave, which
	// is then what reports it, as a run with one wave would.
	for saved, entry := range session.saveMisses(graphs, first, withheld, files) {
		restored, decodeError := decodeEntry(session.known, saved, entry.body, entry.record, nil)
		if decodeError != nil {
			session.problem(decodeError.Error())
			continue
		}
		session.earlier[saved] = restored
	}
	session.decideBySecondKey()
	return givenGraph, nil
}

// decideBySecondKey restores each package that still misses by its second key, in dependency order, so a package
// restored here gives its output to the packages above it. The entry stands only while every answer its package read
// of another package still holds (see readsHold): each owner is below the package, so decided before it, restored, or
// analyzed by the first wave, whose answers the resolver computes now, or still a miss, which fails the read. A hit
// that falls short of what the run can ask of it is a
// miss again (dropShortHits). Each package restored gets an alias under its first key, so the next run with the same
// inputs restores it with no first wave.
func (session *CacheSession) decideBySecondKey() {
	visited := map[*packages.Package]bool{}
	var decide func(current *packages.Package)
	decide = func(current *packages.Package) {
		if visited[current] {
			return
		}
		visited[current] = true
		for _, imported := range current.Imports {
			decide(imported)
		}
		// A package the run reloads to answer a question is checked from source whatever its second key holds, or
		// the run would restore it again with no answer, as it did before the reload (see Reload).
		if session.restoredInWave(current) || len(current.Errors) > 0 || session.reloading(current) {
			return
		}
		second, known := session.secondKey(current)
		if !known || !session.hitAt(current, second) {
			return
		}
		// Every package below current is decided by now, restored, analyzed by the first wave, or still a miss, so
		// each read's owner can answer for it, or the read fails and current misses (see readsHold).
		if !session.holdsBySecondKey(current) {
			delete(session.hits, current)
			delete(session.stored, current)
			return
		}
		session.secondKeyHits[current] = true
	}
	reachable := make([]*packages.Package, 0, len(session.keys))
	for keyed := range session.keys {
		reachable = append(reachable, keyed)
		decide(keyed)
	}
	dropping := true
	for dropping {
		dropping = session.dropShortHits(reachable)
	}
	aliases := map[cache.Key]cache.Key{}
	for restored := range session.secondKeyHits {
		aliases[session.keys[restored]] = session.stored[restored]
	}
	session.alias(aliases)
}

// secondKey is a package's second key, its own digest and the closure digest of each package it imports, by import
// path, and whether it is known: it is not while an output below it is not.
func (session *CacheSession) secondKey(keyed *packages.Package) (cache.Key, bool) {
	own, hasOwn := session.own[keyed]
	if !hasOwn {
		return cache.Key{}, false
	}
	parts, known := session.importedClosures(keyed)
	if !known {
		return cache.Key{}, false
	}
	return cache.DigestOf(append([][]byte{[]byte("second key"), own[:]}, parts...)...), true
}

// closureOf is a package's closure digest: its output digest and the closure digest of each package it imports, by
// import path. It changes exactly when the output of the package or of anything below it does, which is what a
// package above reads: its types come from its imports' types, and its facts from every package below it, which
// checker hands up the whole graph, so an output that changed two levels down reaches it though the package between
// came out the same.
func (session *CacheSession) closureOf(keyed *packages.Package) closureDigest {
	if memoized, done := session.closures[keyed]; done {
		return memoized
	}
	computed := closureDigest{}
	output, hasOutput := session.outputOf(keyed)
	if hasOutput {
		if parts, known := session.importedClosures(keyed); known {
			computed = closureDigest{
				digest: cache.DigestOf(append([][]byte{[]byte("closure"), output[:]}, parts...)...),
				known:  true,
			}
		}
	}
	session.closures[keyed] = computed
	return computed
}

// importedClosures are the import paths of a package and the closure digest of each, in order, and whether every one
// is known.
func (session *CacheSession) importedClosures(keyed *packages.Package) ([][]byte, bool) {
	var parts [][]byte
	for _, importPath := range sortedImports(keyed) {
		imported := session.closureOf(keyed.Imports[importPath])
		if !imported.known {
			return nil, false
		}
		parts = append(parts, []byte(importPath), imported.digest[:])
	}
	return parts, true
}

// outputOf is a package's output digest: a hit's from its entry, or that of a package this run analyzed and saved.
func (session *CacheSession) outputOf(keyed *packages.Package) (cache.Key, bool) {
	if restored, hit := session.hits[keyed]; hit {
		return restored.output, restored.hasOutput
	}
	output, known := session.outputs[keyed]
	return output, known
}

// sortedImports are a package's import paths, sorted.
func sortedImports(keyed *packages.Package) []string {
	return slices.Sorted(maps.Keys(keyed.Imports))
}
