package build_dependence

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// sealCase is one graph a seal test judges: how to build it, the differences it is judged against, and the loads whose
// objects ObjectBits is asked of.
type sealCase struct {
	name        string
	graph       func() *Graph
	differences map[string]Difference
	objects     []types.Object
}

// objectLabel names object by its kind, its name and its position, which tell apart every object of one load: both
// graphs a seal test compares are built from the same cached load, so they hold the same objects.
func objectLabel(object types.Object) string {
	return fmt.Sprintf("%T %s@%d", object, object.Name(), object.Pos())
}

// definedAndUsed are every object loaded defines or uses, each once, in an order the loads alone decide: their
// packages by ID, their files in order, and each file's identifiers by position.
func definedAndUsed(loaded loadedBuild) []types.Object {
	var objects []types.Object
	seen := map[types.Object]bool{}
	for _, identifier := range slices.Sorted(maps.Keys(loaded)) {
		information := loaded[identifier].TypesInfo
		if information == nil {
			continue
		}
		var found []types.Object
		for _, object := range information.Defs {
			if object != nil {
				found = append(found, object)
			}
		}
		for _, object := range information.Uses {
			found = append(found, object)
		}
		slices.SortFunc(found, func(left types.Object, right types.Object) int {
			return strings.Compare(objectLabel(left), objectLabel(right))
		})
		for _, object := range found {
			if !seen[object] {
				seen[object] = true
				objects = append(objects, object)
			}
		}
	}
	return objects
}

// dependenceAnswers are every answer dependence gives, as lines: each unit as Units has it, with its UnitDependent
// and the UnitContaining of its start; the counts; every node's NameBits and UnitDeclaringName by its path and local
// name, and one name the graph does not hold; DependentObjects; and ObjectBits of each of objects.
func dependenceAnswers(dependence *Dependence, objects []types.Object) []string {
	var lines []string
	for index, unit := range dependence.Units() {
		containing, isContained := dependence.UnitContaining(unit.PackageId, unit.File, unit.Start)
		lines = append(lines, fmt.Sprintf("unit %d %+v dependent %t containing %d %t", index, unit,
			dependence.UnitDependent(unit.PackageId, unit.File, unit.Start), containing, isContained))
	}
	lines = append(lines, fmt.Sprintf("counts %+v", dependence.Counts()))
	resolved := dependence.resolved
	for key := range resolved.nodes {
		path := resolved.packages[key.packageIndex].path
		bits, isDeclared := dependence.NameBits(path, key.local)
		declaring, isDeclaring := dependence.UnitDeclaringName(path, key.local)
		lines = append(lines, fmt.Sprintf("name %s %s %+v %t declared by %d %t", path, key.local, bits, isDeclared,
			declaring, isDeclaring))
	}
	bits, isDeclared := dependence.NameBits("example.com/nowhere", "Nothing")
	lines = append(lines, fmt.Sprintf("name nowhere %+v %t", bits, isDeclared))
	for object, bits := range dependence.DependentObjects() {
		lines = append(lines, fmt.Sprintf("dependent object %s %+v", objectLabel(object), bits))
	}
	for _, object := range objects {
		lines = append(lines, fmt.Sprintf("object bits %s %+v", objectLabel(object), dependence.ObjectBits(object)))
	}
	slices.Sort(lines)
	return lines
}

// callIndexAnswers are every answer a call index over host and standardLibrary gives, as lines, with each unit's
// verdict by the builds' judgments of the two.
func callIndexAnswers(index *CallIndex, host *Dependence, standardLibrary *Dependence) []string {
	lines := []string{fmt.Sprintf("units %d", index.UnitCount())}
	units := host.Units()
	if standardLibrary != nil {
		units = append(units, standardLibrary.Units()...)
	}
	for unit := range index.UnitCount() {
		containing, isContained := index.UnitContaining(units[unit].PackageId, units[unit].File, units[unit].Start)
		lines = append(lines, fmt.Sprintf("unit %d nodes %d workspace %t calls %v callers %v dependent %t "+
			"containing %d %t", unit, index.Nodes(unit), index.Workspace(unit), index.Calls(unit), index.Callers(unit),
			index.Dependent(host, standardLibrary, unit), containing, isContained))
	}
	return lines
}

// A sealed graph answers every question exactly as it did before it was sealed: the layout and methods fixtures'
// graphs, and the layout workspace restored from export data, its graph read back from records and answered by name
// by a sealed graph of its dependency, give the same verdicts, counts, bits, names, dependent objects and units under
// linux and under a tag, whether the graph was judged before it was sealed or sealed before it was ever resolved. The
// call index over a host and a dependency's graph is the same too.
func TestASealedGraphAnswersAsBeforeItWasSealed(test *testing.T) {
	test.Parallel()

	for _, fixture := range []string{"layout", "methods"} {
		host := loadFixture(test, fixture, hostBuild)
		differences := map[string]Difference{}
		for _, build := range []fixtureBuild{linuxBuild, taggedBuild} {
			differences[build.name] = differenceOf(host, loadFixture(test, fixture, build))
		}
		expectSealedAlike(test, sealCase{name: fixture, graph: func() *Graph { return graphOf(host, nil) },
			differences: differences, objects: definedAndUsed(host)})
	}

	judged := judge(test, "layout", linuxBuild)
	restored := restoredFromExportData(test, judged.host)
	isWorkspace := func(loaded *packages.Package) bool { return fixtureWorkspace(loaded) }
	answeredBy := func(dependencyGraph *Graph) Difference {
		named := NewCounterparts(dependencyGraph, dependencyGraph.Against(judged.difference))
		byName := judged.difference
		byName.DependentObject, byName.DependentName = named.DependentObject, named.DependentName
		return byName
	}
	open := graphOf(judged.host, isWorkspace)
	sealedDependency := graphOf(judged.host, isWorkspace)
	sealedDependency.Seal()
	live := graphOf(restored, nil)
	restoredObjects := definedAndUsed(restored)
	expectSealedAlike(test, sealCase{name: "layout restored, by name",
		graph:       func() *Graph { return recordedGraphOf(test, live) },
		differences: map[string]Difference{"open dependency": answeredBy(open), "sealed": answeredBy(sealedDependency)},
		objects:     restoredObjects})
	expectAlike(test, "layout restored, answered by a sealed dependency",
		dependenceAnswers(recordedGraphOf(test, live).Against(answeredBy(open)), restoredObjects),
		dependenceAnswers(recordedGraphOf(test, live).Against(answeredBy(sealedDependency)), restoredObjects))

	openHost := recordedGraphOf(test, live)
	openIndex := NewCallIndex(openHost, open)
	before := callIndexAnswers(openIndex, openHost.Against(judged.difference), open.Against(judged.difference))
	sealedHost := recordedGraphOf(test, live)
	sealedHost.Seal()
	sealedIndex := NewCallIndex(sealedHost, sealedDependency)
	after := callIndexAnswers(sealedIndex, sealedHost.Against(judged.difference),
		sealedDependency.Against(judged.difference))
	expectAlike(test, "the call index", before, after)
	if !slices.Equal(before, after) {
		test.Error("the call index: the sealed graphs' lines are the open ones' in another order")
	}
}

// expectSealedAlike judges sealing's case three ways, against each of its differences: an open graph, never sealed;
// the same graph sealed after that judgment and judged again; and a graph sealed before it was ever resolved. Every
// answer of the three must be the same.
func expectSealedAlike(test *testing.T, sealing sealCase) {
	test.Helper()
	open := sealing.graph()
	freshlySealed := sealing.graph()
	freshlySealed.Seal()
	for _, name := range slices.Sorted(maps.Keys(sealing.differences)) {
		difference := sealing.differences[name]
		label := sealing.name + " under " + name
		before := dependenceAnswers(open.Against(difference), sealing.objects)
		if len(before) == 0 {
			test.Fatalf("%s: no answers, so the case proves nothing", label)
		}
		expectAlike(test, label+", sealed before resolving",
			before, dependenceAnswers(freshlySealed.Against(difference), sealing.objects))
	}
	judgedThenSealed := sealing.graph()
	answers := map[string][]string{}
	for name, difference := range sealing.differences {
		answers[name] = dependenceAnswers(judgedThenSealed.Against(difference), sealing.objects)
	}
	judgedThenSealed.Seal()
	judgedThenSealed.Seal()
	for _, name := range slices.Sorted(maps.Keys(sealing.differences)) {
		expectAlike(test, sealing.name+" under "+name+", sealed after judging", answers[name],
			dependenceAnswers(judgedThenSealed.Against(sealing.differences[name]), sealing.objects))
	}
}

// After Seal, adding a package, from its live load, its bytes or its decoded record, and encoding a record each
// panic, saying the graph was sealed, and leave the graph's lock free for the next caller.
func TestASealedGraphRefusesToAddOrEncode(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	live := graphOf(host, nil)
	identifier := slices.Sorted(maps.Keys(live.packages))[0]
	encoded, _ := live.EncodedRecord(identifier)
	decoded, decodeError := DecodeRecord(encoded)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	live.Seal()
	for _, refused := range []struct {
		name string
		call func()
	}{
		{"AddPackage", func() { live.AddPackage(host[identifier], true) }},
		{"AddRecord", func() { _ = live.AddRecord(encoded) }},
		{"AddDecoded", func() { live.AddDecoded(decoded) }},
		{"EncodedRecord", func() { _, _ = live.EncodedRecord(identifier) }},
	} {
		message := panicOf(refused.call)
		if !strings.Contains(message, "sealed") {
			test.Errorf("%s: want a panic saying the graph was sealed, got %q", refused.name, message)
		}
	}
	if unnameable := live.Unnameable(); unnameable != 0 {
		test.Errorf("want the lock free and no unnameable object after the refusals, got %d", unnameable)
	}
}

// panicOf is what call panics with, as text, and empty when it returns.
func panicOf(call func()) (message string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			message = fmt.Sprint(recovered)
		}
	}()
	call()
	return ""
}

// A sealed graph holds nothing only resolution and encoding read: no record holds imports, objects or edges, no unit
// declares, a receiver, reads or callees, the resolution holds no targets or resolved paths, and the graph no
// objectpath encoders. What a judgment reads stays: every unit's file and size, and the resolution itself. A graph of
// live packages and one read back from records are sealed alike.
func TestASealedGraphHoldsNothingOnlyResolutionReads(test *testing.T) {
	test.Parallel()

	live := graphOf(loadFixture(test, "layout", hostBuild), nil)
	recorded := recordedGraphOf(test, live)
	for _, sealed := range []struct {
		name  string
		graph *Graph
	}{{"live", live}, {"recorded", recorded}} {
		sealed.graph.Seal()
		if sealed.graph.resolved == nil || sealed.graph.paths != nil {
			test.Errorf("%s: want the resolution kept and the encoders gone, got resolution %t, encoders %t",
				sealed.name, sealed.graph.resolved != nil, sealed.graph.paths != nil)
		}
		if sealed.graph.resolved != nil &&
			(sealed.graph.resolved.targets != nil || sealed.graph.resolved.resolvedPaths != nil) {
			test.Errorf("%s: want no targets and no resolved paths, got %d and %d", sealed.name,
				len(sealed.graph.resolved.targets), len(sealed.graph.resolved.resolvedPaths))
		}
		units, nodes := 0, 0
		for _, identifier := range slices.Sorted(maps.Keys(sealed.graph.packages)) {
			record := sealed.graph.packages[identifier]
			if record.imports != nil || record.objects != nil || record.edges != nil {
				test.Errorf("%s %s: want no imports, objects or edges, got %d, %d and %d", sealed.name, identifier,
					len(record.imports), len(record.objects), len(record.edges))
			}
			for index := range record.units {
				unit := &record.units[index]
				if unit.declares != nil || unit.receiver != "" || unit.reads != nil || unit.callees != nil {
					test.Errorf("%s %s:%d: want no declares, receiver, reads or callees, got %d, %q, %d and %d",
						sealed.name, unit.file, unit.start, len(unit.declares), unit.receiver, len(unit.reads),
						len(unit.callees))
				}
				if unit.file == "" {
					test.Errorf("%s %s: a unit lost its file", sealed.name, identifier)
				}
				units++
				nodes += unit.nodes
			}
		}
		if units == 0 || nodes == 0 {
			test.Errorf("%s: want the units and their sizes kept, got %d units of %d nodes", sealed.name, units, nodes)
		}
	}
}
