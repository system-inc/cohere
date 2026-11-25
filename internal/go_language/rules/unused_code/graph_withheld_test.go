package unused_code

import (
	"fmt"
	"go/ast"
	"slices"
	"testing"
)

// withheldCase is a package unit 2 of #xeafx79 is built against: a build that skips walking a body that stands and
// splices the host's record of it instead, and, once bodies are stripped, a build that holds no such body at all. For
// each of its bodies the package is built whole, as the rule builds it today; with the body withheld, neither walked
// nor spliced, as a build that strips it with nothing in its place would see it; and with the body's own record
// spliced in its place. Each body is chosen so withholding it loses a finding or invents one, which the case states,
// so the splice is all that keeps the verdict whole.
type withheldCase struct {
	name    string
	sources []recordedSource
	bodies  []withheldBody
}

// withheldBody is one body of a withheldCase, by its function's name, and what withholding it does to the findings:
// each finding the whole package gives and the withheld one does not, lost, and each the withheld one gives and the
// whole does not, invented, each worded as its kind and its name. A body the recorder keeps present is flagged, and
// no build can splice it: it is walked wherever it stands.
type withheldBody struct {
	function string
	lost     []string
	invented []string
	flagged  bool
}

// withheldCases are the fixtures, each a shape a missing body changes the verdict over.
var withheldCases = []withheldCase{
	{
		// Adamic's loss from stage 3's free row: a field never read of a package-level type that only a keyed literal
		// in one body builds, the field left out of the literal. Whole reports the field; withheld, the type has lost
		// its one use, so it is reported and its fields go quiet under it.
		name: "a field of a type only one body's literal builds",
		sources: []recordedSource{{"shape.go", `package shape

type compilerAgreementCase struct {
	path, key, rule string
	group           int
}

func Answer() string {
	cases := []compilerAgreementCase{{path: "a", key: "b", rule: "all"}}
	return cases[0].path
}
`}},
		bodies: []withheldBody{{
			function: "Answer",
			lost:     []string{"field group"},
			invented: []string{"type compilerAgreementCase"},
		}},
	},
	{
		// Loom's loss from stage 3's free row: a field never read of a package-level type whose one use is the map
		// type of a package-level variable that only a test function's body reads. Withheld, the variable and the
		// type are reported, and the field goes quiet under its type.
		name: "a field of a type a variable read only in a test body holds",
		sources: []recordedSource{
			{"shape.go", `package shape
`},
			{"coverage_test.go", `package shape

type namedCallExclusion struct {
	count  int
	reason string
}

var namedCallExclusions = map[string]namedCallExclusion{}

func TestExclusions() int {
	total := 0
	for _, exclusion := range namedCallExclusions {
		total += exclusion.count
	}
	return total
}
`},
		},
		bodies: []withheldBody{{
			function: "TestExclusions",
			lost:     []string{"field reason"},
			invented: []string{"var namedCallExclusions", "type namedCallExclusion"},
		}},
	},
	{
		// What only the withheld body calls, and what that calls in turn: withheld, both are reported.
		name: "a helper and what it calls, used only from one body",
		sources: []recordedSource{{"shape.go", `package shape

func Use() { helper() }

func helper() { called() }

func called() {}
`}},
		bodies: []withheldBody{{
			function: "Use",
			invented: []string{"func helper", "func called"},
		}},
	},
	{
		// A `//lint:ignore U1000` on a package-level object roots it whatever else reaches it, so withholding the
		// body that uses it changes nothing of the verdict; the directives that change it are the body's own. One is
		// stale, on a local the body reads anyway, and one is not, on a local nothing reads. Withheld, both locals are
		// gone and the constants they read are reported. Spliced, the ignored local is used by the root alone, from
		// the record's rooted locals, so a splice that drops those reports it and what it reads.
		name: "an ignore directive in one body, stale on one local and needed on another",
		sources: []recordedSource{{"shape.go", `package shape

const read = 1

const kept = 2

func Use() int {
	//lint:ignore U1000 stale, since the body reads it as well
	const stale = read
	//lint:ignore U1000 kept for the test
	const ignored = kept
	return stale
}
`}},
		bodies: []withheldBody{{
			function: "Use",
			invented: []string{"const read", "const kept"},
		}},
	},
	{
		// The owner chain into a body. The function is called only from an init body: withhold that, and the function
		// is reported where its unused locals were, which go quiet under it. Withhold the function's own body, and
		// its unused locals are lost. The local type's field is quiet under the type, by an own edge the record
		// holds, so a splice that drops own edges reports the field.
		name: "a function's unused locals, the function called only from an init body",
		sources: []recordedSource{{"shape.go", `package shape

func init() { called() }

func called() {
	const counted = 1
	type unread struct{ field int }
}
`}},
		bodies: []withheldBody{
			{
				function: "init",
				lost:     []string{"const counted", "type unread"},
				invented: []string{"func called"},
			},
			{
				function: "called",
				lost:     []string{"const counted", "type unread"},
			},
		},
	},
	{
		// A struct conversion in the body between a local struct type and a package-level struct, whose fields use
		// each other both ways. The local's forward field is read in the body, which reaches the package-level
		// forward field; the package-level backward field is read in another function, which reaches the local's.
		// Withheld, the package-level forward field is reported. Spliced, the edge from the package-level field to
		// the local one is all that keeps the local's backward field from being reported.
		name: "a struct conversion between a local struct and a package-level one",
		sources: []recordedSource{{"shape.go", `package shape

type outer struct{ forward, backward int }

var Value outer

func Convert() int {
	type inner struct{ forward, backward int }
	converted := inner(Value)
	return converted.forward
}

func Backward() int { return Value.backward }
`}},
		bodies: []withheldBody{{
			function: "Convert",
			invented: []string{"field forward"},
		}},
	},
	{
		// A helper called only from an init body in another file of the package, as a build-only file's init would
		// call it: withheld, the helper is reported.
		name: "a helper called only from another file's init body",
		sources: []recordedSource{
			{"shape.go", `package shape

func helper() {}
`},
			{"extra.go", `package shape

func init() { helper() }
`},
		},
		bodies: []withheldBody{{
			function: "init",
			invented: []string{"func helper"},
		}},
	},
	{
		// The test variant, the package compiled with its `_test.go` files: a body in one writes a package-level
		// variable that file declares, which is a use only because the file's name ends `_test.go`, and calls a
		// helper of the package's own files. Withheld, both are reported.
		name: "a body in a test file of the package",
		sources: []recordedSource{
			{"shape.go", `package shape

func compute() int { return 1 }
`},
			{"shape_test.go", `package shape

var sink int

func BenchmarkCompute() { sink = compute() }
`},
		},
		bodies: []withheldBody{{
			function: "BenchmarkCompute",
			invented: []string{"var sink", "func compute"},
		}},
	},
	{
		// The reader's must from the design page: converting a named pointer type to unsafe.Pointer uses every field
		// of the struct it points to, and of the struct that holds, which the body names none of. The recorder keeps
		// the body present, so it is walked wherever it stands and never spliced. Withheld, the struct's field and
		// the type only that field reaches are reported.
		name: "an unsafe.Pointer conversion of a named pointer type",
		sources: []recordedSource{{"shape.go", `package shape

import "unsafe"

type inner struct{ value int }

type target struct{ held inner }

type pointer *target

func address(value pointer) unsafe.Pointer { return unsafe.Pointer(value) }

func Address(value pointer) unsafe.Pointer { return address(value) }
`}},
		bodies: []withheldBody{{
			function: "address",
			invented: []string{"field held", "type inner"},
			flagged:  true,
		}},
	},
}

// TestWithheldBodyChangesTheVerdictAndItsRecordRestoresIt holds each fixture to the claim unit 2 of #xeafx79 rests
// on, at the rule's own level, with no pipeline: a body withheld changes the findings exactly as its case says, so the
// rule cannot simply go without it, and the body's own record spliced in its place gives the whole package's nodes,
// edges and verdict, so the record is enough. A flagged body is the recorder's to keep present: its record says so,
// and no graph takes it.
func TestWithheldBodyChangesTheVerdictAndItsRecordRestoresIt(test *testing.T) {
	test.Parallel()
	for _, each := range withheldCases {
		for _, body := range each.bodies {
			test.Run(each.name+", "+body.function, func(test *testing.T) {
				test.Parallel()
				if len(body.lost) == 0 && len(body.invented) == 0 {
					test.Fatal("the case states no finding withholding the body changes, so it is no fixture")
				}
				checked := checkShapes(test, each.name, each.sources, nil)
				key := functionBodyKey(test, checked, body.function)
				whole, wholeError := checked.build(false, nil)
				if wholeError != nil {
					test.Fatal(wholeError)
				}

				withheld, withheldError := checked.build(false, []bodyRecord{withheldRecord(key)})
				if withheldError != nil {
					test.Fatal(withheldError)
				}
				lost, invented := findingDifferences(whole.results().unused, withheld.results().unused)
				if !slices.Equal(lost, sortedWords(body.lost)) || !slices.Equal(invented, sortedWords(body.invented)) {
					test.Errorf("withheld, the body loses %v and invents %v, not %v and %v", lost, invented,
						sortedWords(body.lost), sortedWords(body.invented))
				}

				recording, recordError := checked.build(true, nil)
				if recordError != nil {
					test.Fatal(recordError)
				}
				record, isRecorded := recordKeyedBy(recording.bodyRecords(), key)
				if !isRecorded {
					test.Fatalf("the recorder wrote no one record of the body at %s", placeText(key))
				}
				if body.flagged {
					if !record.Present.AllFieldsUsed {
						test.Errorf("the record of the body is kept present by %+v, not by its walk using every "+
							"field", record.Present)
					}
					if taken, takeError := checked.build(false, []bodyRecord{record}); takeError == nil {
						test.Errorf("a graph took the flagged record, and holds %d nodes", len(taken.nodes))
					}
					return
				}
				if record.Present != (recordedPresence{}) {
					test.Fatalf("the record of the body is kept present, %+v, so it proves nothing of the splice",
						record.Present)
				}
				spliced, spliceError := checked.build(false, []bodyRecord{record})
				if spliceError != nil {
					test.Fatal(spliceError)
				}
				for _, difference := range spliceDifferences(selfSpliced{plain: whole, spliced: spliced}) {
					test.Errorf("spliced: %s", difference)
				}
			})
		}
	}
}

// functionBodyKey is the key of the body of the one function declaration in checked named function, as a standing
// body is keyed (keyOf): the file its `func` is in, past any `//line` directive, and the offset of that `func` in the
// file as parsed.
func functionBodyKey(test *testing.T, checked checkedPackage, function string) recordedPlace {
	test.Helper()
	found := []recordedPlace{}
	for _, file := range checked.files {
		tokenFile := checked.fileSet.File(file.FileStart)
		for _, declaration := range file.Decls {
			declared, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || declared.Body == nil || declared.Name.Name != function {
				continue
			}
			found = append(found, recordedPlace{
				File:   tokenFile.Position(declared.Pos()).Filename,
				Offset: tokenFile.Offset(declared.Pos()),
			})
		}
	}
	if len(found) != 1 {
		test.Fatalf("%d function bodies of the package are named %s, not one", len(found), function)
	}
	return found[0]
}

// withheldRecord is a record of the body at key that adds nothing: a graph that takes it skips the body's walk and
// its locals in the function type's scope, as it does for any record it takes, and then splices nothing. That is a
// body stripped with nothing in its place, which needs no seam of its own in the graph.
func withheldRecord(key recordedPlace) bodyRecord {
	return bodyRecord{Body: key}
}

// recordKeyedBy is the one record of records keyed by key, and whether there is exactly one.
func recordKeyedBy(records []bodyRecord, key recordedPlace) (bodyRecord, bool) {
	found := []bodyRecord{}
	for _, record := range records {
		if record.Body == key {
			found = append(found, record)
		}
	}
	if len(found) != 1 {
		return bodyRecord{}, false
	}
	return found[0], true
}

// findingDifferences is each finding of whole not among withheld's, and each of withheld's not among whole's, each
// worded as its kind and its name, counting repeats and sorted.
func findingDifferences(whole []object, withheld []object) ([]string, []string) {
	remaining := map[string]int{}
	for _, each := range withheld {
		remaining[fmt.Sprintf("%s %s", each.kind, each.name)]++
	}
	lost := []string{}
	for _, each := range whole {
		worded := fmt.Sprintf("%s %s", each.kind, each.name)
		if remaining[worded] > 0 {
			remaining[worded]--
			continue
		}
		lost = append(lost, worded)
	}
	invented := []string{}
	for worded, count := range remaining {
		for range count {
			invented = append(invented, worded)
		}
	}
	return sortedWords(lost), sortedWords(invented)
}

// sortedWords is a sorted copy of words, never nil, so two lists of the same words compare equal.
func sortedWords(words []string) []string {
	sorted := append([]string{}, words...)
	slices.Sort(sorted)
	return sorted
}
