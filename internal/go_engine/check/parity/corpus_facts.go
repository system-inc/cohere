package parity

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// RequireIdenticalCorpusFacts runs original and port in one checker over every package of the tree at directory, as
// the engine loads it, tests included, and over every package those import, the standard library's among them, and
// requires the two to export the same facts on every package: each fact keyed by its package, its object's objectpath
// and how the fact prints, since the two sides' fact types differ. It is the fact differential a port of a fact pass
// passes before the engine reads it in the original's place (#w7zmvat), RequireIdenticalFacts over a whole tree rather
// than fixtures, and unlike that one it compares every fact a side exports on its own package's objects, those no
// importer inherits as well, since a pass's result is built from them all.
//
// An object with no objectpath, which objectpath gives only the objects another package can name, as an unexported
// function or one named `_`, is keyed by its position instead, the same on both sides of one load. A package either
// side did not run on, since it did not type-check or an analyzer it requires failed there, compares the two sides'
// errors instead. The test fails on the first difference, in the order of package and key, naming the package and the
// object, and fails too when the original exported nothing, which would prove nothing. Each side's count is logged.
func RequireIdenticalCorpusFacts(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	directory string,
) {
	test.Helper()
	defer Target{}.held(test)()
	workspace, workspaceError := project.ReadWorkspace(directory)
	if workspaceError != nil {
		test.Fatalf("reading the modules of %s: %v", directory, workspaceError)
	}
	loaded, loadError := load.Packages(workspace.Root, []string{"./..."}, workspace.Environment(), nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", workspace.Root, loadError)
	}
	if len(loaded.Packages) == 0 {
		test.Fatalf("loading %s found no package, so the comparison would prove nothing", workspace.Root)
	}
	sides, analyzeError := corpusFactsOf(original, port, loaded.Packages)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", workspace.Root, analyzeError)
	}
	originalSide, portSide := sides[original], sides[port]
	test.Logf("%s: %d packages analyzed, %d in the graph; the original exported %d facts on %d packages and did not "+
		"run on %d, the port exported %d facts on %d packages and did not run on %d", workspace.Root,
		len(loaded.Packages), loaded.PackagesLoaded, originalSide.count, len(originalSide.facts),
		len(originalSide.failures), portSide.count, len(portSide.facts), len(portSide.failures))
	if originalSide.count == 0 {
		test.Fatalf("the original exported no fact over %s, so the comparison proves nothing", workspace.Root)
	}
	if difference, differs := firstCorpusDifference(originalSide, portSide); differs {
		test.Fatalf("%s", difference)
	}
}

// corpusSide is what one analyzer of a pair exported over a tree: each package's facts, by the package's ID and then
// by the object's key, the packages it did not run on, by ID, with the checker's error, and how many facts in all.
type corpusSide struct {
	facts    map[string]map[string]corpusFact
	failures map[string]string
	count    int
}

// corpusFact is one fact as the differential compares it: the object it is about, named for a reader, and the fact as
// `%v` prints it.
type corpusFact struct {
	object string
	fact   string
}

// corpusFactsOf runs original and port in one checker over roots and returns each side's facts, keyed by its
// analyzer. Each root lets go of its syntax once its last action has finished, as each dependency does, so a large tree
// never holds every package's syntax at once; the facts the comparison reads stay on the actions.
func corpusFactsOf(
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	roots []*packages.Package,
) (map[*analysis.Analyzer]*corpusSide, error) {
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{original, port}, roots,
		&checker.Options{Finished: func(*packages.Package) {
			// Setting Finished is what lets a root's syntax go; the comparison reads nothing from it first.
		}})
	if analyzeError != nil {
		return nil, fmt.Errorf("analyzing %d packages: %w", len(roots), analyzeError)
	}
	sides := map[*analysis.Analyzer]*corpusSide{}
	for _, analyzer := range []*analysis.Analyzer{original, port} {
		sides[analyzer] = &corpusSide{facts: map[string]map[string]corpusFact{}, failures: map[string]string{}}
	}
	for action := range graph.All() {
		// The graph holds the actions of the analyzers the two require as well, whose facts are neither side's.
		side, isSide := sides[action.Analyzer]
		if !isSide {
			continue
		}
		if action.Err != nil {
			side.failures[action.Package.ID] = action.Err.Error()
			continue
		}
		own := map[string]corpusFact{}
		// AllObjectFacts holds what the action inherited from the packages below too, so only the facts on its own
		// package's objects are its own.
		for _, objectFact := range action.AllObjectFacts() {
			if objectFact.Object.Pkg() != action.Package.Types {
				continue
			}
			own[corpusObjectKey(action.Package, objectFact.Object)] = corpusFact{
				object: corpusObjectName(objectFact.Object),
				fact:   fmt.Sprint(objectFact.Fact),
			}
		}
		for _, packageFact := range action.AllPackageFacts() {
			if packageFact.Package == action.Package.Types {
				own["package"] = corpusFact{object: "package " + packageFact.Package.Path(),
					fact: fmt.Sprint(packageFact.Fact)}
			}
		}
		side.facts[action.Package.ID] = own
		side.count += len(own)
	}
	return sides, nil
}

// corpusObjectKey is object's key among its package's facts: its objectpath, or, for an object with none, its
// position in the load, the same for both sides.
func corpusObjectKey(owner *packages.Package, object types.Object) string {
	path, pathError := objectpath.For(object)
	if pathError == nil {
		return "objectpath " + string(path)
	}
	return fmt.Sprintf("declared at %s", owner.Fset.Position(object.Pos()))
}

// corpusObjectName is object as a failure names it: a function's full name, as `(*net/http.Request).WithContext`,
// and any other object as go/types prints it.
func corpusObjectName(object types.Object) string {
	if function, isFunction := object.(*types.Func); isFunction {
		return function.FullName()
	}
	return object.String()
}

// firstCorpusDifference is the first place the two sides part, by package ID and then by key, described, and whether
// they part at all.
func firstCorpusDifference(original *corpusSide, port *corpusSide) (string, bool) {
	packageIdentifiers := slices.Sorted(maps.Keys(original.facts))
	packageIdentifiers = append(packageIdentifiers, slices.Sorted(maps.Keys(original.failures))...)
	packageIdentifiers = append(packageIdentifiers, slices.Sorted(maps.Keys(port.facts))...)
	packageIdentifiers = append(packageIdentifiers, slices.Sorted(maps.Keys(port.failures))...)
	slices.Sort(packageIdentifiers)
	for _, packageIdentifier := range slices.Compact(packageIdentifiers) {
		originalFailure, originalFailed := original.failures[packageIdentifier]
		portFailure, portFailed := port.failures[packageIdentifier]
		if originalFailed || portFailed {
			if originalFailed && portFailed {
				continue
			}
			return fmt.Sprintf("on %s the original %s, and the port %s", packageIdentifier,
				ranOrFailed(originalFailed, originalFailure), ranOrFailed(portFailed, portFailure)), true
		}
		originalFacts, portFacts := original.facts[packageIdentifier], port.facts[packageIdentifier]
		keys := append(slices.Collect(maps.Keys(originalFacts)), slices.Collect(maps.Keys(portFacts))...)
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			originalFact, originalHolds := originalFacts[key]
			portFact, portHolds := portFacts[key]
			if originalHolds && portHolds && originalFact.fact == portFact.fact {
				continue
			}
			object := originalFact.object
			if !originalHolds {
				object = portFact.object
			}
			return fmt.Sprintf("on %s, %s: the original exported %s, and the port %s (%s)", packageIdentifier, object,
				exportedFact(originalHolds, originalFact), exportedFact(portHolds, portFact), key), true
		}
	}
	return "", false
}

// ranOrFailed is what a side did on a package, for a failure: failed, with why, or ran.
func ranOrFailed(failed bool, failure string) string {
	if failed {
		return "did not run: " + failure
	}
	return "ran"
}

// exportedFact is the fact a side exported on an object, quoted, or that it exported none.
func exportedFact(holds bool, fact corpusFact) string {
	if holds {
		return fmt.Sprintf("%q", fact.fact)
	}
	return "no fact"
}
