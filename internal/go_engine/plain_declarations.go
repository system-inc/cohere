package go_engine

import (
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
)

// plainDeclarations is a plain run's declarations-only graph of the host's packages for the must-check census
// (#anfjtr3), built by its lint pass as each package's type-read walk runs, while its syntax and type information are
// whole (check.Options.Declarations), and so tied to that pass: a pass dropped, beside the load or for a reload, drops
// its graph with it. inScope says which packages are the workspace's, and withheldOf what the load withheld of a
// package (load.PackageWithheld.Declarations), read once the package is final. Each package is added once, from the
// findings store's declarations-only record of it before any walk starts (addRecord), or else by the first walk that
// hands it on, every other caller waiting for that add (added), and held records the IDs of the packages added.
type plainDeclarations struct {
	graph      *build_dependence.Graph
	inScope    func(*packages.Package) bool
	withheldOf func(*packages.Package) []string
	added      sync.Map
	held       sync.Map
}

// newPlainDeclarations is an empty declarations-only graph, its packages' workspace said by inScope and what the load
// withheld of each by withheldOf.
func newPlainDeclarations(
	inScope func(*packages.Package) bool,
	withheldOf func(*packages.Package) []string,
) *plainDeclarations {
	return &plainDeclarations{graph: build_dependence.NewDeclarationsGraph(), inScope: inScope,
		withheldOf: withheldOf}
}

// add adds loaded to the graph once, from its live syntax and type information, and returns only once it is added: a
// walk that hands loaded on after another started its add waits for it, so neither lets the syntax go while it is read.
// A package with no type information, one restored from export data, is added by no one, and its objects are answered
// wide wherever a unit reads them. It is check.Options.Declarations.
func (declarations *plainDeclarations) add(loaded *packages.Package) {
	stored, _ := declarations.added.LoadOrStore(loaded, &sync.Once{})
	once, isOnce := stored.(*sync.Once)
	if !isOnce {
		// Invariant: add alone stores into added, and only a *sync.Once.
		panic("go_engine: the plain declarations' adds hold something other than a *sync.Once")
	}
	once.Do(func() {
		// A package added from its record needs no walk of its syntax.
		if loaded.TypesInfo == nil || declarations.holds(loaded.ID) {
			return
		}
		declarations.graph.AddPackage(loaded, declarations.inScope(loaded), declarations.withheldOf(loaded))
		declarations.held.Store(loaded.ID, true)
	})
}

// addRecord adds the package of identifier from record, the findings store's declarations-only record of it, which
// stands for its syntax whether the host restored it or checks it, the record being keyed by every input it reads
// (check.DeclarationsRecordKey). It is called before the pass's walks start, so no walk adds the package again.
func (declarations *plainDeclarations) addRecord(identifier string, record *build_dependence.Record) {
	declarations.graph.AddDecoded(record)
	declarations.held.Store(identifier, true)
}

// holds reports whether the package of identifier was added to the graph.
func (declarations *plainDeclarations) holds(identifier string) bool {
	_, isHeld := declarations.held.Load(identifier)
	return isHeld
}
