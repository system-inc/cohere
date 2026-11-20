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
// findings store's or the system cache's declarations-only record of it before any walk starts (addRecord), or else by
// the first walk that hands it on, every other caller waiting for that add (added), or by the standard library's side
// load (addLoaded). held records the IDs of the packages added, recorded those of them added from a record.
type plainDeclarations struct {
	graph      *build_dependence.Graph
	inScope    func(*packages.Package) bool
	withheldOf func(*packages.Package) []string
	added      sync.Map
	held       sync.Map
	recorded   sync.Map
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
// wide wherever a unit reads them, and so is one the type checker found errors in, its own or an import's
// (IllTyped): a lint beside the load hands one on before the load's end shows them (#jzzdvcf), whose syntax need not
// match its types, a field named twice declared once, and the graph holds only syntax its types match. Its load then
// ends with diagnostics, so the pass is abandoned with the graph. It is check.Options.Declarations.
func (declarations *plainDeclarations) add(loaded *packages.Package) {
	stored, _ := declarations.added.LoadOrStore(loaded, &sync.Once{})
	once, isOnce := stored.(*sync.Once)
	if !isOnce {
		// Invariant: add alone stores into added, and only a *sync.Once.
		panic("go_engine: the plain declarations' adds hold something other than a *sync.Once")
	}
	once.Do(func() {
		// A package added from its record needs no walk of its syntax.
		if loaded.TypesInfo == nil || loaded.IllTyped || declarations.holds(loaded.ID) {
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
	declarations.recorded.Store(identifier, true)
}

// addLoaded adds loaded, a package of the side load of the standard library (sideLoadStandardLibrary), from its live
// syntax and type information, which no walk of the host's hands on, with withheld what its load withheld of it.
func (declarations *plainDeclarations) addLoaded(loaded *packages.Package, withheld []string) {
	declarations.graph.AddPackage(loaded, false, withheld)
	declarations.held.Store(loaded.ID, true)
}

// fromRecord reports whether the package of identifier was added from a record (addRecord).
func (declarations *plainDeclarations) fromRecord(identifier string) bool {
	_, isRecorded := declarations.recorded.Load(identifier)
	return isRecorded
}

// holds reports whether the package of identifier was added to the graph.
func (declarations *plainDeclarations) holds(identifier string) bool {
	_, isHeld := declarations.held.Load(identifier)
	return isHeld
}
