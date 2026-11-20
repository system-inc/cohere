package go_engine

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// Block 1's bits on a plain run, as a census only (#anfjtr3, stage 6's first unit). A run with no `--build-dependence`
// asked for the must-check census (pipeline_must_check_census.go) has its host's lint pass build a declarations-only
// graph of every package it checks from source, in each package's type-read walk (plainDeclarations), and judges each
// extra build on it, writing the same census file `--build-dependence`'s whole graph writes, so the two can be held to
// each other. It decides nothing. Each build is judged at its restore (load.Caching.Listed), from its listing and its
// build-only files' syntax alone, once the graph is whole. A package the host restored from its findings store is added
// from the declarations-only record kept beside its entry, and a standard-library package it restored from the system
// cache from the one kept beside its entry there (check.KeepDeclarationsRecords); one of those with no record is loaded
// from source once, beside the builds (sideLoadStandardLibrary). Once the walks have ended, each package with no record
// has its record written (completePlainBits). A package still in no graph, one a failed side load left out or a store
// hit by an alias whose target has no record, is named in the wide file, and each of its objects a unit reads is
// answered dependent on both bits.

// declarationsStep is a step of a plain run's declarations graph, which a test's hook is told of as it is reached
// (pipeline.declarationsSteps): the host's walks whole, so the graph holds every package it will; its completion
// started; the graph whole, about to let the builds' judgments run; and, for each build, its listing waiting for the
// graph, then its judgment. The hook is called from the pipeline's goroutine, the completion's and each build's.
type declarationsStep string

const (
	declarationsWalksWhole     declarationsStep = "WalksWhole"
	declarationsRecordsWriting declarationsStep = "RecordsWriting"
	declarationsReady          declarationsStep = "Ready"
	declarationsListedWaiting  declarationsStep = "ListedWaiting"
	declarationsJudging        declarationsStep = "Judging"
)

// plainBits is what a plain run judges its extra builds on for the must-check census: the host's pass's declarations
// graph, whole once the pass's walks have ended and its side load has run, and the host's listing each build's is
// compared with. ready is closed once the graph is whole (completePlainBits): notHeld are then the IDs of the packages
// of the host's listing it does not hold, sideLoaded those its side load added, and sideLoadProblem why the side load
// failed, nil when it did not. lineDirectives is each scanned file's answer, by path as written
// (holdsLineDirectiveOnce), since the builds compare the same files at once.
type plainBits struct {
	declarations    *plainDeclarations
	host            hostListing
	ready           chan struct{}
	notHeld         []string
	sideLoaded      []string
	sideLoadProblem error
	lineDirectives  sync.Map
}

// newPlainBits is the plain bits of declarations, the graph of the lint pass over loaded, and of loaded's listing, not
// ready until completePlainBits has made the graph whole. It is made once the pass's walks have ended.
func newPlainBits(declarations *plainDeclarations, loaded *load.Loaded) *plainBits {
	bits := &plainBits{declarations: declarations, ready: make(chan struct{}), host: hostListing{
		files: map[string][]string{}, cgoFiles: map[string][]string{}, paths: map[string]string{}}}
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		bits.host.files[each.ID] = each.GoFiles
		if cgoFiles := build_dependence.CgoFiles(each); len(cgoFiles) > 0 {
			bits.host.cgoFiles[each.ID] = cgoFiles
		}
		bits.host.paths[each.ID] = each.PkgPath
	})
	return bits
}

// plainMustCheckOf is the must-check set of the build whose load listed reachable, every package it reaches, judged
// on the plain bits at the build's restore (load.Caching.Listed), from its listing and its build-only files' syntax
// alone, once the graph is whole, nil when the run keeps no plain bits. A package the graph does not hold is answered
// wide, both bits set for every object of it a unit reads, by live object or by name. The set is stamped with how long
// the judgment waited for the graph and how long it took after.
func (engine *pipeline) plainMustCheckOf(reachable []*packages.Package) *mustCheckSet {
	bits := engine.plainBits
	if bits == nil {
		return nil
	}
	waitStarted := time.Now()
	engine.declarationsStepped(declarationsListedWaiting)
	<-bits.ready
	started := time.Now()
	engine.declarationsStepped(declarationsJudging)
	built := make(map[string]*packages.Package, len(reachable))
	for _, each := range reachable {
		built[each.ID] = each
	}
	read := engine.sourceReader(nil, nil)
	// The judgment's shared files and packages, and those without syntax, are the facts census's, which a plain run
	// keeps none of.
	judged := &buildJudgment{shared: map[sharedFile]bool{}, sharedPackages: map[string]bool{},
		withoutSyntax: map[string]bool{}, filesDiffer: map[string]bool{}}
	difference := engine.listingDifference(bits.host, built, token.NewFileSet(), read,
		func(file string) bool { return holdsLineDirectiveOnce(&bits.lineDirectives, file, read) }, judged)
	difference.DependentObject = func(types.Object) (bool, bool) { return true, true }
	difference.DependentName = func(string, string) (bool, bool, bool) { return true, true, false }
	dependence := bits.declarations.graph.Against(difference)
	return &mustCheckSet{
		lines: mustCheckLines([]build_dependence.Difference{difference}, []*build_dependence.Dependence{dependence}),
		wide:  mustCheckWide(bits.notHeld, 0),
		at:    mustCheckAtListing,

		judged: time.Since(started),
		waited: started.Sub(waitStarted),
	}
}

// declarationsStepped tells a test's declarationsSteps hook, when one is set, that the graph reached step.
func (engine *pipeline) declarationsStepped(step declarationsStep) {
	if engine.declarationsSteps != nil {
		engine.declarationsSteps(step)
	}
}

// completePlainBits makes bits whole and writes their graph's records, on a goroutine of its own beside the builds, and
// returns the join, which waits for both and says on stderr when the side load or a write failed. It first loads from
// source every standard-library package of loaded's listing, the host's, that the graph holds neither live nor from a
// record (sideLoadStandardLibrary), then reads what the graph does not hold and lets the builds' judgments run (ready),
// then writes the record of each package with none in its store (check.CacheSession.WriteDeclarationsRecords), the
// side-loaded ones among them. A run with no session writes none.
//
// Mutates: bits gains what its side load added, why it failed and what the graph does not hold, and its ready closes
func (engine *pipeline) completePlainBits(bits *plainBits, loaded *load.Loaded) func() {
	session := engine.session
	engine.declarationsStepped(declarationsRecordsWriting)
	var failed int
	var firstFailure error
	done := make(chan struct{})
	go func() {
		defer close(done)
		bits.sideLoaded, bits.sideLoadProblem = engine.sideLoadStandardLibrary(bits.declarations, loaded)
		for identifier := range bits.host.files {
			if !bits.declarations.holds(identifier) {
				bits.notHeld = append(bits.notHeld, identifier)
			}
		}
		engine.declarationsStepped(declarationsReady)
		close(bits.ready)
		if session != nil {
			failed, firstFailure = session.WriteDeclarationsRecords(func(saved *packages.Package) ([]byte, bool) {
				return bits.declarations.graph.EncodedRecord(saved.ID)
			})
		}
	}()
	return func() {
		<-done
		if bits.sideLoadProblem != nil {
			engine.note("the must-check census's standard-library side load failed, so every object of what it would "+
				"have loaded is called dependent: %v", bits.sideLoadProblem)
		}
		if failed > 0 {
			engine.note("the findings cache: %d declarations records not written, the first: %v", failed, firstFailure)
		}
	}
}

// sideLoadStandardLibrary loads from source, under the host's environment, without tests, every standard-library
// package of loaded's listing that declarations holds neither live nor from a record: one the system cache restored
// with no record beside its entry, or with one that did not decode. Every package the side load reaches that the graph
// does not hold yet is added to it, as the standard library's own graph under `--build-dependence` adds them
// (standardLibraryGraph), so its record can be written. It returns the IDs it added, sorted, and why the load failed,
// when it did, every package it would have added then left out. The standard library is the only package in no
// module that a module's load reaches, and none of it is a test variant.
func (engine *pipeline) sideLoadStandardLibrary(
	declarations *plainDeclarations,
	loaded *load.Loaded,
) ([]string, error) {
	var paths []string
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.Module == nil && each.ForTest == "" && !declarations.holds(each.ID) {
			paths = append(paths, each.PkgPath)
		}
	})
	if len(paths) == 0 {
		return nil, nil
	}
	sideLoaded, loadError := engine.loadPackages(loadRequest{
		environment: engine.described.workspace.Environment(),
		caching:     &load.Caching{GoEnvironment: engine.described.goEnvironment, WithoutTests: true},
		patterns:    paths,
	})
	switch {
	case loadError != nil:
		return nil, loadError
	case len(sideLoaded.Diagnostics) > 0:
		return nil, fmt.Errorf("%d errors, the first %s", len(sideLoaded.Diagnostics),
			sideLoaded.Diagnostics[0].Message)
	}
	var adding []*packages.Package
	packages.Visit(sideLoaded.Roots, nil, func(each *packages.Package) {
		if !declarations.holds(each.ID) {
			adding = append(adding, each)
		}
	})
	engine.workPool().ForEach(len(adding), 0, func(index int) {
		declarations.addLoaded(adding[index], sideLoaded.Withheld[adding[index]].Declarations)
	})
	added := make([]string, 0, len(adding))
	for _, each := range adding {
		added = append(added, each.ID)
	}
	slices.Sort(added)
	return added, nil
}

// decodeDeclarationsRecord is the declarations-only record encoded holds (build_dependence.DecodeRecord), or why it
// holds none: bytes that do not decode, or a whole graph's record, which no declarations record's key holds.
func decodeDeclarationsRecord(encoded []byte) (*build_dependence.Record, error) {
	record, decodeError := build_dependence.DecodeRecord(encoded)
	if decodeError != nil {
		return nil, decodeError
	}
	if !record.DeclarationsOnly() {
		return nil, errors.New("decoding a declarations record: it is a whole graph's record")
	}
	return record, nil
}
