package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// placedKind is the kind placedProbe asks, a boolean registered nowhere, so an entry's Answers computed whole never
// hold it and only its placed answers, and those recorded by objectpath, keep it.
var placedKind = &fact_resolver.Kind{Name: "placed_probe"}

// placedProbe asks placedKind of each static call's callee, every answer true, and reports the callee with its answer.
var placedProbe = &analysis.Analyzer{
	Name:     "placedProbe",
	Doc:      "reports each static call's callee with the placed_probe answer about it, every one computed true",
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		resolver, resolverError := fact_resolver.Of(pass)
		if resolverError != nil {
			return nil, resolverError
		}
		reader := resolver.Package(pass.Pkg)
		if reader == nil {
			return nil, fmt.Errorf("the resolver never learned %s, which ctrlflow ran on", pass.Pkg.Path())
		}
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if callee := typeutil.StaticCallee(pass.TypesInfo, call); callee != nil && callee.Pkg() != nil {
					answer := fact_resolver.Ask(resolver, reader, placedKind, callee,
						func(*fact_resolver.Package) bool { return true }, func() bool { return false })
					pass.Reportf(call.Pos(), "%s placed %t", callee.FullName(), answer)
				}
				return true
			})
		}
		return nil, nil
	},
}

// placedBottom is bottom's file in placedModule: Double calls the unexported twice, which no objectpath names.
const placedBottom = "package bottom\n\nfunc Double(value int) int {\n\treturn twice(value)\n}\n\n" +
	"func twice(value int) int {\n\treturn value * 2\n}\n"

// placedModule writes a module of two packages to directory: bottom, placedBottom, and top, whose body is topBody,
// and loads it.
func placedModule(test *testing.T, directory string, topBody string) []*packages.Package {
	test.Helper()
	files := map[string]string{
		"go.mod":           "module example.com/placed\n\ngo 1.24\n",
		"bottom/bottom.go": placedBottom,
		"top/top.go":       "package top\n\nimport \"example.com/placed/bottom\"\n\n" + topBody,
	}
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the module: %v %+v", loadError, loaded)
	}
	return loaded.Packages
}

// placedProbeAnswers is entry's placed answers of placedKind, each as its file, the function its offset in bottom's
// file opens, its parameter and its answer, for a test to compare.
func placedProbeAnswers(test *testing.T, entry cacheEntry, bottomFile string) []string {
	test.Helper()
	placed, placedError := entry.Placed.placed()
	if placedError != nil {
		test.Fatal(placedError)
	}
	var named []string
	for _, answer := range placed {
		if answer.Kind != placedKind.Name {
			continue
		}
		at := "elsewhere"
		for _, function := range []string{"Double", "twice"} {
			if answer.File == bottomFile && answer.Offset == strings.Index(placedBottom, "func "+function+"(") {
				at = function
			}
		}
		named = append(named, fmt.Sprintf("%s %d %s", at, answer.Parameter, answer.Encoded))
	}
	return named
}

// bottomFileOf is the name bottom's one file has in loaded, as its syntax names it.
func bottomFileOf(test *testing.T, loaded []*packages.Package) string {
	test.Helper()
	for _, each := range loaded {
		if each.PkgPath == "example.com/placed/bottom" && len(each.CompiledGoFiles) == 1 {
			return each.CompiledGoFiles[0]
		}
	}
	test.Fatal("the load holds no bottom with one file")
	return ""
}

// A cold run with a store writes bottom's entry with its answers computed whole, every kind they hold complete, and
// beside them, by place, the answer bottom's own analysis asked of twice: unexported, so no objectpath names it, and of
// placed_probe, which no whole computes, so only its place keeps it.
func TestAWholeEntryHoldsAnUnexportedFunctionsAnswerOfAnUnregisteredKindByPlace(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	loaded := placedModule(test, directory, "var _ = bottom.Double\n")
	cold := runWithSession(test, directory, caching, rules)
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, "example.com/placed/bottom"))
	if !stored || len(entry.Answers) == 0 {
		test.Fatalf("want bottom's entry written with its answers computed whole, got %+v (stored %t)", entry.Answers,
			stored)
	}
	for _, kind := range entry.Answers {
		if !kind.Complete || kind.Kind == placedKind.Name {
			test.Fatalf("want every kind of bottom's answers a registered one, complete, got %+v", entry.Answers)
		}
	}
	want := []string{"twice -1 true"}
	if placed := placedProbeAnswers(test, entry, bottomFileOf(test, loaded)); !slices.Equal(placed, want) {
		test.Errorf("want bottom's entry to hold %q by place, got %q", want, placed)
	}
}

// A run that reloads bottom to answer top's question of Double writes bottom's entry again with Double's placed answer
// beside twice's, which the cold run wrote: bottom is not analyzed this time, so nothing asks of twice, and only the
// entry held before keeps its answer, which, of bottom's one key, still holds.
func TestAReloadedEntryKeepsThePlacedAnswersItHeld(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	runCached(test, placedModule(test, directory, "var _ = bottom.Double\n"), caching, rules)
	session := NewCacheSession(caching, rules)
	var bottomFile string
	pass := func() *Result {
		loaded := placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
		bottomFile = bottomFileOf(test, loaded)
		var analyzed []*packages.Package
		for _, each := range loaded {
			if each.PkgPath == "example.com/placed/top" {
				analyzed = append(analyzed, each)
			}
		}
		var reachable []*packages.Package
		packages.Visit(analyzed, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
		session.Restore(load.LoaderSource, LoadedSizes(reachable), reachable, analyzed)
		result, runError := Run(analyzed, rules, Options{Cache: session})
		if runError != nil {
			test.Fatalf("running the probe: %v", runError)
		}
		return result
	}
	first := pass()
	if !slices.Equal(first.Reloads, []string{"example.com/placed/bottom"}) || !session.Reload(first.Reloads) {
		test.Fatalf("want the first pass to ask for bottom to be loaded again, got %v", first.Reloads)
	}
	again := pass()
	if len(again.Reloads) > 0 || len(again.Crashes) > 0 {
		test.Fatalf("want bottom answered from source, got reloads %v and crashes %+v", again.Reloads, again.Crashes)
	}
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, session, "example.com/placed/bottom"))
	want := []string{"Double -1 true", "twice -1 true"}
	if placed := placedProbeAnswers(test, entry, bottomFile); !stored || !slices.Equal(placed, want) {
		test.Errorf("want bottom's entry written again to hold %q by place, got %q (stored %t)", want, placed, stored)
	}
}

// The first wave writes bottom's entry with twice's placed answer, which bottom's own analysis asked, and the second,
// whose top asks Double, writes it again with Double's beside it (rewriteEarlier): its answers, computed whole, and its
// reads are as they were, so only its placed answers grew.
func TestAnEntryWhosePlacedAnswersGrewAfterTheFirstWaveIsWrittenAgain(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	loaded := placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	cold := runWithSession(test, directory, caching, rules)
	if cold.result.Cache.Waves != 2 {
		test.Fatalf("want bottom analyzed in a first wave and top in a second, got %+v", cold.result.Cache)
	}
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, "example.com/placed/bottom"))
	want := []string{"Double -1 true", "twice -1 true"}
	if placed := placedProbeAnswers(test, entry, bottomFileOf(test, loaded)); !stored || !slices.Equal(placed, want) {
		test.Errorf("want bottom's entry written again to hold %q by place, got %q (stored %t)", want, placed, stored)
	}
}

// placedDisagreeing is every place, its kind, file, offset and parameter, at which left and right both hold an answer
// and the two answers differ, and how many places both hold an answer at, which a caller holds above zero, so two sets
// with no place in common never pass as agreeing.
func placedDisagreeing(left []fact_resolver.PlacedAnswer, right []fact_resolver.PlacedAnswer) ([]string, int) {
	held := make(map[placedAt][]byte, len(left))
	for _, answer := range left {
		held[placedAt{kind: answer.Kind, file: answer.File, digest: string(answer.FileDigest), offset: answer.Offset,
			parameter: answer.Parameter}] = answer.Encoded
	}
	var disagreeing []string
	compared := 0
	for _, answer := range right {
		at := placedAt{kind: answer.Kind, file: answer.File, digest: string(answer.FileDigest), offset: answer.Offset,
			parameter: answer.Parameter}
		encoded, isHeld := held[at]
		if !isHeld {
			continue
		}
		compared++
		if !bytes.Equal(encoded, answer.Encoded) {
			disagreeing = append(disagreeing, fmt.Sprintf("%s %s:%d %d", at.kind, at.file, at.offset, at.parameter))
		}
	}
	return disagreeing, compared
}

// An entry holds its placed answers by column, each file's and kind's name once, and reads back exactly what it was
// given, in PlacedIn's order whatever order they came in: answers in three files, two of one name parsed from other
// bytes and one with no digest, of three kinds, a function's own and a parameter's, two at one offset. Columns of
// different lengths, a kind or a file the table does not hold, or an empty answer, are a miss, never a panic.
func TestAnEntrysPlacedAnswersReadBackExactly(test *testing.T) {
	test.Parallel()
	answers := []fact_resolver.PlacedAnswer{
		{Kind: "return_nil_ness", File: "/module/b.go", Offset: 40, Parameter: -1, Encoded: []byte("[]")},
		{Kind: "return_nil_ness", File: "/module/a.go", FileDigest: []byte("generated"), Offset: 130, Parameter: -1,
			Encoded: []byte("[]")},
		{Kind: "function_purity", File: "/module/a.go", FileDigest: []byte("written"), Offset: 900, Parameter: -1,
			Encoded: []byte("true")},
		{Kind: "odd_slice_argument", File: "/module/a.go", FileDigest: []byte("written"), Offset: 120, Parameter: 1,
			Encoded: []byte("true")},
		{Kind: "function_purity", File: "/module/a.go", FileDigest: []byte("written"), Offset: 120, Parameter: -1,
			Encoded: []byte("false")},
	}
	stored := storedPlacedOf(answers)
	if len(stored.Kinds) != 3 || len(stored.Files) != 3 || len(stored.Values) != 5 {
		test.Fatalf("want three kinds and three files, each once, and five rows, got %+v", stored)
	}
	placed, placedError := stored.placed()
	want := slices.SortedFunc(slices.Values(answers), fact_resolver.ComparePlaced)
	if placedError != nil || !reflect.DeepEqual(placed, want) {
		test.Errorf("want %+v read back, got %+v (%v)", want, placed, placedError)
	}
	if empty, emptyError := storedPlacedOf(nil).placed(); emptyError != nil || len(empty) != 0 {
		test.Errorf("want nothing read back of nothing, got %+v (%v)", empty, emptyError)
	}
	short := storedPlacedOf(answers)
	short.Values = short.Values[1:]
	unknown := storedPlacedOf(answers)
	unknown.KindIndexes[0] = len(unknown.Kinds)
	negativeKind := storedPlacedOf(answers)
	negativeKind.KindIndexes[0] = -1
	unknownFile := storedPlacedOf(answers)
	unknownFile.FileIndexes[0] = len(unknownFile.Files)
	negativeFile := storedPlacedOf(answers)
	negativeFile.FileIndexes[0] = -1
	// An empty answer is no answer: decoded, it would read as the zero value, no fact, never what the run held.
	empty := storedPlacedOf(answers)
	empty.Values[0] = nil
	for name, broken := range map[string]storedPlaced{"a column short": short, "a kind past the table": unknown,
		"a kind before the table": negativeKind, "a file past the table": unknownFile,
		"a file before the table": negativeFile, "an empty answer": empty} {
		if _, brokenError := broken.placed(); brokenError == nil {
			test.Errorf("%s: want a miss, read back without an error", name)
		}
	}
}

// gobLength is how many bytes gob encodes value in, a gob of its own.
func gobLength[Value any](test *testing.T, value Value) int {
	test.Helper()
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(value); encodeError != nil {
		test.Fatal(encodeError)
	}
	return encoded.Len()
}

// A package's placed answers by column cost fewer bytes an answer than a list naming each one's kind and file: 400
// answers of three kinds over ten files, by the bytes each form adds to a gob that holds none, which is what an entry
// adds, since every entry's gob describes the type once whether it holds answers or not. The test logs both.
func TestPlacedAnswersByColumnCostFewerBytesAnAnswer(test *testing.T) {
	test.Parallel()
	kinds := []string{"function_purity", "return_nil_ness", "odd_slice_argument"}
	var answers []fact_resolver.PlacedAnswer
	for index := range 400 {
		// Not a machine path: a name only encoded, as long as a real one so the bytes compare fairly; nothing reads it.
		answers = append(answers, fact_resolver.PlacedAnswer{Kind: kinds[index%3],
			File:   fmt.Sprintf("/Users/someone/Projects/module/internal/package/file_%d.go", index%10),
			Offset: 120 * index, Parameter: index%4 - 1, Encoded: []byte("true")})
	}
	flat := gobLength(test, answers) - gobLength(test, []fact_resolver.PlacedAnswer{{}})
	columns := gobLength(test, storedPlacedOf(answers)) - gobLength(test, storedPlaced{})
	test.Logf("400 placed answers add %d bytes as a list naming each one's kind and file, %d by column", flat,
		columns)
	if columns*2 > flat {
		test.Errorf("want the columns under half the list's bytes, got %d and %d", columns, flat)
	}
}

// A cold run's entry reads back exactly the placed answers it was written with: read back and held again, they are the
// same columns, and each is an answer the run held, PlacedIn's at its place (the run asks more as it saves, Double's
// purity when top's answers are computed whole, after bottom's entry is written).
func TestAColdEntryReadsBackThePlacedAnswersItsRunHeld(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	cold := runWithSession(test, directory, caching, rules)
	key := sessionKey(test, cold.session, "example.com/placed/bottom")
	entry, _, stored := storedEntryAt(caching.Store, key)
	if !stored {
		test.Fatal("want bottom's entry written")
	}
	var bottom *packages.Package
	for keyed := range cold.session.keys {
		if keyed.PkgPath == "example.com/placed/bottom" {
			bottom = keyed
		}
	}
	home := cold.session.resolver.Package(bottom.Types)
	if home == nil {
		test.Fatal("the run's resolver never learned bottom")
	}
	placed, placedError := entry.Placed.placed()
	if placedError != nil || len(placed) == 0 || !reflect.DeepEqual(storedPlacedOf(placed), entry.Placed) {
		test.Fatalf("want bottom's entry to read back the columns it holds, got %+v of %+v (%v)", placed,
			entry.Placed, placedError)
	}
	held := cold.session.resolver.PlacedIn(home, digestsOf(cold.session.files))
	for _, answer := range placed {
		if !slices.ContainsFunc(held, func(each fact_resolver.PlacedAnswer) bool {
			return reflect.DeepEqual(each, answer)
		}) {
			test.Errorf("want every answer read back one the run held, got %+v, which is not among %+v", answer,
				held)
		}
	}
}

// An entry whose placed answers do not read back is a miss as a whole, as one gob refuses is: restoredOf, which every
// read of an entry goes through, refuses it, and reads one whose answers do read with them.
func TestAnEntryWhosePlacedAnswersDoNotReadIsAMiss(test *testing.T) {
	test.Parallel()
	answers := []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: "/module/a.go", FileDigest: []byte("written"), Offset: 120, Parameter: -1,
			Encoded: []byte("true")},
	}
	restoring := &packages.Package{ID: "example.com/placed/bottom"}
	whole := cacheEntry{Version: CacheFormatVersion, Placed: storedPlacedOf(answers)}
	restored, restoreError := restoredOf(&cacheAnalyzers{}, restoring, whole, nil)
	if restoreError != nil || !reflect.DeepEqual(restored.placed, answers) {
		test.Fatalf("want the entry read with its placed answers, got %v (%v)", restored, restoreError)
	}
	broken := whole
	broken.Placed = storedPlacedOf(answers)
	broken.Placed.Values = nil
	if _, brokenError := restoredOf(&cacheAnalyzers{}, restoring, broken, nil); brokenError == nil {
		test.Error("want an entry whose placed answers' columns differ in length refused, read without an error")
	}
}
