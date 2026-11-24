package check

import (
	"bytes"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// entryBytesFunctions is how many functions the package of the entry's bytes' tests declares, each of which an
// analyzer of theirs reports and may export facts about.
const entryBytesFunctions = 8

// entryBytesAnalyzer is an analyzer of one test's own, named name and learning the fact types of learned: it reports
// every function its package declares, exports what objectFacts gives for each, by the function's place among them,
// about the function, and exports what packageFacts gives about the package. Either may be nil, for none.
func entryBytesAnalyzer(
	name string,
	learned []analysis.Fact,
	objectFacts func(index int) []analysis.Fact,
	packageFacts func() []analysis.Fact,
) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:      name,
		Doc:       "reports every function, and exports the facts its test gives it",
		FactTypes: learned,
		Run: func(pass *analysis.Pass) (any, error) {
			index := 0
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, isFunction := declaration.(*ast.FuncDecl)
					if !isFunction {
						continue
					}
					pass.Reportf(function.Name.Pos(), "%s met %s", name, function.Name.Name)
					if objectFacts != nil {
						for _, fact := range objectFacts(index) {
							pass.ExportObjectFact(pass.TypesInfo.Defs[function.Name], fact)
						}
					}
					index++
				}
			}
			if packageFacts != nil {
				for _, fact := range packageFacts() {
					pass.ExportPackageFact(fact)
				}
			}
			return nil, nil
		},
	}
}

// analyzedForEntry runs rules over a module of the test's own, one package that declares entryBytesFunctions
// functions and imports nothing, and returns what encodeEntry is given of the package: what a session of rules knows
// of their analyzers, each analyzer's rule, the package, and every action on it.
func analyzedForEntry(
	test *testing.T,
	rules []rule.Rule,
) (*cacheAnalyzers, map[*analysis.Analyzer]rule.Rule, *packages.Package, []*checker.Action) {
	test.Helper()
	directory := test.TempDir()
	source := "package ordered\n"
	for index := range entryBytesFunctions {
		source += fmt.Sprintf("\nfunc Function%d() int { return %d }\n", index, index)
	}
	for name, contents := range map[string]string{
		"go.mod":     "module example.com/ordered\n\ngo 1.24\n",
		"ordered.go": source,
	} {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 || len(loaded.Packages) != 1 {
		test.Fatalf("loading the module: %v %+v", loadError, loaded)
	}
	ruleOf := map[*analysis.Analyzer]rule.Rule{}
	analyzers := make([]*analysis.Analyzer, 0, len(rules))
	for _, enabled := range rules {
		ruleOf[enabled.Analyzer] = enabled
		analyzers = append(analyzers, enabled.Analyzer)
	}
	known, knownError := newCacheAnalyzers(ruleOf, analyzers, nil)
	if knownError != nil {
		test.Fatal(knownError)
	}
	graph, analyzeError := analyzeWrapped(analyzers, loaded.Packages, &checker.Options{})
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	var actions []*checker.Action
	for action := range graph.All() {
		if action.Err != nil {
			test.Fatalf("%s did not finish: %v", action.Analyzer.Name, action.Err)
		}
		actions = append(actions, action)
	}
	return known, ruleOf, loaded.Packages[0], actions
}

// encodeAnalyzed is the entry of saved from actions, as a keep encodes it: without export data or the load's files.
func encodeAnalyzed(
	test *testing.T,
	known *cacheAnalyzers,
	ruleOf map[*analysis.Analyzer]rule.Rule,
	saved *packages.Package,
	actions []*checker.Action,
) encodedEntry {
	test.Helper()
	encoded, encodeError := encodeEntry(known, ruleOf, saved, actions, load.PackageWithheld{}, entryDemand{},
		storedFiles(saved), nil, nil, false)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return encoded
}

// eightRules are eight rules of one test's own, each of which reports every function of its package. Three of them
// learn a fact type each, the types other tests declare, and export one fact about the first function and one about
// the package, so no analysis holds two facts whose order could differ: the order of the eight is all that can.
func eightRules() []rule.Rule {
	learned := map[int]func() analysis.Fact{
		1: func() analysis.Fact { return new(markedFact) },
		4: func() analysis.Fact { return new(plantedFact) },
		6: func() analysis.Fact { return new(listedFact) },
	}
	rules := make([]rule.Rule, 0, 8)
	for index := range 8 {
		name := fmt.Sprintf("entryBytes%d", index)
		newFact, learns := learned[index]
		if !learns {
			rules = append(rules, houseRule(entryBytesAnalyzer(name, nil, nil, nil)))
			continue
		}
		rules = append(rules, houseRule(entryBytesAnalyzer(name, []analysis.Fact{newFact()},
			func(function int) []analysis.Fact {
				if function != 0 {
					return nil
				}
				return []analysis.Fact{newFact()}
			},
			func() []analysis.Fact { return []analysis.Fact{newFact()} })))
	}
	return rules
}

// An entry of eight rules' analyses, each with findings and three with facts, encodes to the same bytes every time
// (#g9kaytv): its analyses are a list in order of slot. As a map by slot, gob wrote them in the order it ranged the
// map, another every time, so the body a record of another build names by the digest of its bytes had a new digest
// whenever its entry was written again, and that build's package was checked from source for nothing.
func TestAnEntryEncodesItsAnalyzersInTheSameOrder(test *testing.T) {
	test.Parallel()

	known, ruleOf, saved, actions := analyzedForEntry(test, eightRules())
	first := encodeAnalyzed(test, known, ruleOf, saved, actions)
	restored, decodeError := decodeEntry(known, saved, first.body, first.record, nil, false)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	if len(restored.slots) != 8 {
		test.Fatalf("want the entry to hold the eight rules' slots, got %v", restored.slots)
	}
	// A map ranged twice can come in one order by chance, so the entry is encoded 64 times more.
	for range 64 {
		if next := encodeAnalyzed(test, known, ruleOf, saved, actions); !bytes.Equal(first.body, next.body) {
			test.Fatal("the same entry of eight analyses, encoded again, has other bytes")
		}
	}
}

// An entry of one analysis that holds two facts about each of eight functions and two about the package encodes to the
// same bytes every time (#g9kaytv): the facts are sorted, the objects' by objectpath and then type, the package's by
// type. An action gives its facts in the order it ranges its maps of them (checker's ExportedFacts), another every
// time, and with one slot the facts' order is all that can differ.
func TestAnEntryEncodesItsFactsInTheSameOrder(test *testing.T) {
	test.Parallel()

	// The fact types are the ones other tests declare, three to this one analyzer.
	analyzer := entryBytesAnalyzer("entryBytesFacts",
		[]analysis.Fact{new(waveFact), new(markFact), new(countedFact)},
		func(function int) []analysis.Fact { return []analysis.Fact{&waveFact{Wave: function}, new(markFact)} },
		func() []analysis.Fact { return []analysis.Fact{&waveFact{Wave: -1}, &countedFact{Counted: true}} })
	known, ruleOf, saved, actions := analyzedForEntry(test, []rule.Rule{houseRule(analyzer)})
	first := encodeAnalyzed(test, known, ruleOf, saved, actions)
	restored, decodeError := decodeEntry(known, saved, first.body, first.record, nil, false)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	held := restored.analyses[analyzer]
	if len(restored.slots) != 1 || len(held.objectFacts) != 2*entryBytesFunctions || len(held.packageFacts) != 2 {
		test.Fatalf("want one slot of %d facts about functions and 2 about the package, got slots %v, %d and %d",
			2*entryBytesFunctions, restored.slots, len(held.objectFacts), len(held.packageFacts))
	}
	// A map ranged twice can come in one order by chance, so the entry is encoded 64 times more.
	for range 64 {
		if next := encodeAnalyzed(test, known, ruleOf, saved, actions); !bytes.Equal(first.body, next.body) {
			test.Fatal("the same entry of one analysis' facts, encoded again, has other bytes")
		}
	}
}

// restoredFactLines is one line for each fact of restored, sorted: what it is about, an objectpath or the package,
// its type and its value.
func restoredFactLines(restored restoredAnalysis) []string {
	var lines []string
	for _, objectFact := range restored.objectFacts {
		lines = append(lines, fmt.Sprintf("object %s %T %+v", objectFact.path, objectFact.fact, objectFact.fact))
	}
	for _, packageFact := range restored.packageFacts {
		lines = append(lines, fmt.Sprintf("package %T %+v", packageFact, packageFact))
	}
	slices.Sort(lines)
	return lines
}

// An entry encoded and decoded restores, for each analyzer, by its slot, what its action held: the action's findings
// in their order, and every fact it exported, about the same objects. The slots it holds are the rules', sorted.
func TestAnEntryRestoresEachAnalysisByItsSlot(test *testing.T) {
	test.Parallel()

	rules := eightRules()
	known, ruleOf, saved, actions := analyzedForEntry(test, rules)
	encoded := encodeAnalyzed(test, known, ruleOf, saved, actions)
	restored, decodeError := decodeEntry(known, saved, encoded.body, encoded.record, nil, false)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	slots := make([]string, 0, len(rules))
	for _, enabled := range rules {
		slots = append(slots, enabled.Name)
	}
	slices.Sort(slots)
	if !slices.Equal(restored.slots, slots) {
		test.Errorf("the entry holds slots %v, want %v", restored.slots, slots)
	}
	facts := 0
	for _, action := range actions {
		held, isHeld := restored.analyses[action.Analyzer]
		if !isHeld {
			test.Errorf("the entry restored nothing of %s", action.Analyzer.Name)
			continue
		}
		if found := actionFindings(ruleOf[action.Analyzer], action); len(found) != entryBytesFunctions ||
			!reflect.DeepEqual(held.findings, found) {
			test.Errorf("%s restored findings\n%+v\nwant its action's\n%+v", action.Analyzer.Name, held.findings, found)
		}
		var exported []string
		objectFacts, packageFacts := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			path, pathError := objectpath.For(objectFact.Object)
			if pathError != nil {
				test.Fatal(pathError)
			}
			exported = append(exported, fmt.Sprintf("object %s %T %+v", path, objectFact.Fact, objectFact.Fact))
		}
		for _, packageFact := range packageFacts {
			exported = append(exported, fmt.Sprintf("package %T %+v", packageFact.Fact, packageFact.Fact))
		}
		slices.Sort(exported)
		if lines := restoredFactLines(held); !slices.Equal(lines, exported) {
			test.Errorf("%s restored facts\n%s\nwant its action's\n%s", action.Analyzer.Name,
				strings.Join(lines, "\n"), strings.Join(exported, "\n"))
		}
		facts += len(exported)
	}
	if facts != 6 {
		test.Errorf("the actions exported %d facts, want the three learners' two each", facts)
	}
}

// mapsReachedFrom is where a value of holder holds a map, each by the path of fields to it: gob writes a map in the
// order it ranges it, so a map anywhere in what an entry encodes gives the same entry other bytes.
func mapsReachedFrom(holder reflect.Type) []string {
	var reached []string
	// A struct type is read once, so a type that holds itself ends the walk.
	seen := map[reflect.Type]bool{}
	var walk func(held reflect.Type, path string)
	walk = func(held reflect.Type, path string) {
		switch held.Kind() {
		case reflect.Map:
			reached = append(reached, path)
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(held.Elem(), path+"[]")
		case reflect.Struct:
			if seen[held] {
				return
			}
			seen[held] = true
			for index := range held.NumField() {
				walk(held.Field(index).Type, path+"."+held.Field(index).Name)
			}
		default:
			// Every other kind an entry holds is a boolean, a number or a string, which holds nothing.
		}
	}
	walk(holder, holder.Name())
	return reached
}

// Neither an entry's body nor its record holds a map anywhere (#g9kaytv): gob writes a map in the order it ranges it,
// another every time, and a body's bytes are what a record of another build names it by. A fact's own bytes and a
// report analyzer's result are encoded apart, as bytes, which this does not read.
func TestAnEntryHoldsNoMap(test *testing.T) {
	test.Parallel()

	for _, holder := range []reflect.Type{reflect.TypeFor[cacheEntry](), reflect.TypeFor[entryRecord]()} {
		if reached := mapsReachedFrom(holder); len(reached) > 0 {
			test.Errorf("%s holds a map, which gob writes in no one order, at %v", holder.Name(), reached)
		}
	}
}

// BenchmarkDecodingAnEntry decodes, as a hit decodes it, the body of each of two entries a run of every registered
// rule wrote of the standard library fixture: the fixture's own package, which holds a slot for every rule, and fmt,
// which holds the slots of the analyzers that learn facts, with fmt's facts. It reports each body's size beside the
// decode's allocations (#g9kaytv). It is skipped under -short.
func BenchmarkDecodingAnEntry(benchmark *testing.B) {
	if testing.Short() {
		benchmark.Skip("runs every rule over a module and the standard library below it")
	}
	directory := benchmark.TempDir()
	for _, name := range []string{"go.mod", "library.go"} {
		contents, readError := os.ReadFile(filepath.Join("..", "testdata", "standard-library", name))
		if readError != nil {
			benchmark.Fatal(readError)
		}
		if writeError := os.WriteFile(filepath.Join(directory, name), contents, 0o644); writeError != nil {
			benchmark.Fatal(writeError)
		}
	}
	every := rules.All()
	caching := cachingAt(benchmark.TempDir(), every)
	session := NewCacheSession(caching, every)
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		benchmark.Fatal(loadError)
	}
	if _, runError := Run(loaded.Packages, every, Options{Cache: session, Withheld: loaded.Withheld}); runError != nil {
		benchmark.Fatal(runError)
	}
	if len(loaded.Packages) != 1 {
		benchmark.Fatalf("want the fixture's one package loaded, got %d", len(loaded.Packages))
	}
	for _, path := range []string{loaded.Packages[0].PkgPath, "fmt"} {
		benchmark.Run(path, func(benchmark *testing.B) {
			var decoding *packages.Package
			for keyed := range session.keys {
				if keyed.PkgPath == path && keyed.ForTest == "" {
					decoding = keyed
				}
			}
			if decoding == nil {
				benchmark.Fatalf("the session keyed no package %s", path)
			}
			contents, stored := caching.Store.Get(session.keys[decoding])
			if !stored {
				benchmark.Fatalf("the store holds no record of %s", path)
			}
			body, record, whole := storedEntry(caching.Store, contents)
			if !whole {
				benchmark.Fatalf("the store holds no body of %s", path)
			}
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				_, decodeError := decodeEntry(session.known, decoding, body, record, nil, false)
				if decodeError != nil {
					benchmark.Fatal(decodeError)
				}
			}
			benchmark.ReportMetric(float64(len(body)), "body-bytes")
		})
	}
}
