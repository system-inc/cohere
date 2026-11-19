package go_engine

import (
	"go/token"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// Block 1's bits on a plain run, as a census only (#anfjtr3, stage 6's first unit). A run with no `--build-dependence`
// asked for the must-check census (pipeline_must_check_census.go) has its host's lint pass build a declarations-only
// graph of every package it checks from source, in each package's type-read walk (plainDeclarations), and judges each
// extra build on it, writing the same census file `--build-dependence`'s whole graph writes, so the two can be held to
// each other. It decides nothing. For now each build is judged after its first load, from its listing and its
// build-only files alone; a package the host restored has no record yet, so it is in no graph, named in the wide file,
// and each of its objects a unit reads is answered dependent on both bits.

// plainBits is what a plain run judges its extra builds on for the must-check census: the host's pass's declarations
// graph, whole once the pass's walks have ended, and the host's listing each build's is compared with. notHeld are the
// IDs of the packages of the host's listing the graph does not hold, and lineDirectives is each scanned file's answer,
// by path as written (holdsLineDirectiveOnce), since the builds compare the same files at once.
type plainBits struct {
	declarations   *plainDeclarations
	host           hostListing
	notHeld        []string
	lineDirectives sync.Map
}

// newPlainBits is the plain bits of declarations, the graph of the lint pass over loaded, and of loaded's listing. It
// is made once the pass's walks have ended, so the graph holds every package it will.
func newPlainBits(declarations *plainDeclarations, loaded *load.Loaded) *plainBits {
	bits := &plainBits{declarations: declarations, host: hostListing{files: map[string][]string{},
		cgoFiles: map[string][]string{}, paths: map[string]string{}}}
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		bits.host.files[each.ID] = each.GoFiles
		if cgoFiles := build_dependence.CgoFiles(each); len(cgoFiles) > 0 {
			bits.host.cgoFiles[each.ID] = cgoFiles
		}
		bits.host.paths[each.ID] = each.PkgPath
		if !declarations.holds(each.ID) {
			bits.notHeld = append(bits.notHeld, each.ID)
		}
	})
	return bits
}

// plainMustCheckOf is the must-check set of the build loaded is the first load of, judged on the plain bits from its
// listing and its build-only files alone, nil when the run keeps no plain bits. A package the graph does not hold is
// answered wide, both bits set for every object of it a unit reads, by live object or by name.
func (engine *pipeline) plainMustCheckOf(loaded *load.Loaded) *mustCheckSet {
	bits := engine.plainBits
	if bits == nil {
		return nil
	}
	built := map[string]*packages.Package{}
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) { built[each.ID] = each })
	read := engine.sourceReader(nil, nil)
	// The judgment's shared files and packages are the facts census's, which a plain run keeps none of.
	judged := &buildJudgment{shared: map[sharedFile]bool{}, sharedPackages: map[string]bool{},
		filesDiffer: map[string]bool{}}
	difference := engine.listingDifference(bits.host, built, token.NewFileSet(), read,
		func(file string) bool { return holdsLineDirectiveOnce(&bits.lineDirectives, file, read) }, judged)
	difference.DependentObject = func(types.Object) (bool, bool) { return true, true }
	difference.DependentName = func(string, string) (bool, bool, bool) { return true, true, false }
	dependence := bits.declarations.graph.Against(difference)
	return &mustCheckSet{
		lines: mustCheckLines([]build_dependence.Difference{difference}, []*build_dependence.Dependence{dependence}),
		wide:  mustCheckWide(bits.notHeld, 0),
	}
}
