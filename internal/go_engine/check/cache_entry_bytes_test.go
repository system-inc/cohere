package check

import (
	"bytes"
	"encoding"
	"encoding/gob"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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

// mapsReachedFrom is where a value of holder holds a map, or what could hide one, an interface or a type that encodes
// itself, each by the path of fields to it: gob writes a map in the order it ranges it, so a map anywhere in what an
// entry encodes gives the same entry other bytes.
func mapsReachedFrom(holder reflect.Type) []string {
	var reached []string
	// A struct type is read once, so a type that holds itself ends the walk.
	seen := map[reflect.Type]bool{}
	var walk func(held reflect.Type, path string)
	walk = func(held reflect.Type, path string) {
		// A type that encodes itself writes whatever its method does, a map among them, where this walk cannot read.
		for _, encodesItself := range []reflect.Type{reflect.TypeFor[gob.GobEncoder](),
			reflect.TypeFor[encoding.BinaryMarshaler]()} {
			if held.Implements(encodesItself) || reflect.PointerTo(held).Implements(encodesItself) {
				reached = append(reached, path+" (encodes itself)")
				return
			}
		}
		switch held.Kind() {
		case reflect.Map:
			reached = append(reached, path)
		case reflect.Interface:
			// An interface holds any type at all, so what it encodes is not read from the entry's own type.
			reached = append(reached, path+" (an interface)")
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
	// The walk itself finds each thing it is for, so a clean read above is one that looked.
	for name, holder := range map[string]reflect.Type{
		"a map":                      reflect.TypeFor[struct{ Held []map[string]int }](),
		"an interface":               reflect.TypeFor[struct{ Held *struct{ Inner any } }](),
		"a type that encodes itself": reflect.TypeFor[struct{ Held [2]time.Time }](),
	} {
		if reached := mapsReachedFrom(holder); len(reached) != 1 {
			test.Errorf("over a type holding %s, the walk reached %v, want the one place", name, reached)
		}
	}
}

// formerEntry is an entry as format 15 held one, as far as gob reads it: its analyses a map by slot.
type formerEntry struct {
	Version   string
	Analyzers map[string]storedAnalysis
}

// A slot the entry does not hold reads as not held, wherever it would sort, and a held one reads as its own
// (#g9kaytv): the search answers only for a name it finds, since a slot found by its place would hand one rule
// another's findings, a hit where a miss was the truth.
func TestAnAbsentSlotReadsAsNotHeldWhereverItSorts(test *testing.T) {
	test.Parallel()

	ordered := storedSlotAnalyses(map[string]storedAnalysis{
		"b": {Report: []byte("b")}, "d": {Report: []byte("d")}, "f": {Report: []byte("f")},
	})
	for _, absent := range []string{"a", "c", "e", "g", "", "bb", "B"} {
		if found, held := storedAnalysisAt(ordered, absent); held {
			test.Errorf("the absent slot %q reads as held, with %q's analysis", absent, found.Report)
		}
	}
	for _, slot := range []string{"b", "d", "f"} {
		if found, held := storedAnalysisAt(ordered, slot); !held || string(found.Report) != slot {
			test.Errorf("the slot %q reads held %t with %q, want its own", slot, held, found.Report)
		}
	}
	if _, held := storedAnalysisAt(nil, "b"); held {
		test.Error("an entry of no analyses holds a slot")
	}
}

// An entry whose analyses are out of order of slot, or hold a slot twice, is refused, a miss, rather than searched
// (#g9kaytv): the search finds a slot only in an ordered list, so a list that is not could lose a rule's findings on a
// package nothing else would recheck. The same entry as encodeEntry wrote it reads.
func TestAnEntryWhoseAnalysesAreOutOfOrderIsRefused(test *testing.T) {
	test.Parallel()

	known, ruleOf, saved, actions := analyzedForEntry(test, eightRules())
	encoded := encodeAnalyzed(test, known, ruleOf, saved, actions)
	written, decodeError := entryOf(encoded.body, encoded.record)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	if _, restoreError := restoredOf(known, saved, written, nil, false); restoreError != nil {
		test.Fatalf("the entry as written is refused: %v", restoreError)
	}
	swapped := written
	swapped.Analyzers = slices.Clone(written.Analyzers)
	swapped.Analyzers[2], swapped.Analyzers[5] = swapped.Analyzers[5], swapped.Analyzers[2]
	if _, restoreError := restoredOf(known, saved, swapped, nil, false); restoreError == nil {
		test.Error("an entry with two analyses out of order is read")
	}
	twice := written
	twice.Analyzers = slices.Clone(written.Analyzers)
	twice.Analyzers[4].Slot = twice.Analyzers[3].Slot
	if _, restoreError := restoredOf(known, saved, twice, nil, false); restoreError == nil {
		test.Error("an entry holding one slot twice is read")
	}
}

// An entry of the format before this one is a miss, by its version and by its shape (#g9kaytv). By version: an entry
// in today's shape that says it is format 15 is refused. By shape: a body whose analyses are a map by slot, as format
// 15 wrote them, does not decode into the list even when it claims today's version, so it can never read as an entry
// that holds no analysis.
func TestAnEntryOfTheFormerFormatIsAMiss(test *testing.T) {
	test.Parallel()

	const former = "go-engine-cache-15"
	if former == CacheFormatVersion {
		test.Fatalf("the format is still %s, which held its analyses as a map", former)
	}
	known, ruleOf, saved, actions := analyzedForEntry(test, eightRules())
	encoded := encodeAnalyzed(test, known, ruleOf, saved, actions)
	written, decodeError := entryOf(encoded.body, encoded.record)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	versioned := written
	versioned.Version = former
	body, encodeError := encodeBody(versioned)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	record := encoded.record
	record.Version = former
	if _, readError := decodeEntry(known, saved, body, record, nil, false); readError == nil {
		test.Errorf("an entry that says it is format %s is read", former)
	}
	analyses := map[string]storedAnalysis{}
	for _, held := range written.Analyzers {
		analyses[held.Slot] = held.Analysis
	}
	for _, version := range []string{former, CacheFormatVersion} {
		var shaped bytes.Buffer
		shapedEntry := formerEntry{Version: version, Analyzers: analyses}
		if gobError := gob.NewEncoder(&shaped).Encode(shapedEntry); gobError != nil {
			test.Fatal(gobError)
		}
		record.Version = version
		if _, readError := decodeEntry(known, saved, shaped.Bytes(), record, nil, false); readError == nil {
			test.Errorf("a body whose analyses are a map by slot, saying it is format %s, is read", version)
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
