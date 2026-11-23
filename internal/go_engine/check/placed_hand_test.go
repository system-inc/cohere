package check

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// diskDigests is the sha256 of each file's bytes on disk, as a build that parsed them now would hold them.
func diskDigests(test *testing.T) func(parsed string) ([]byte, bool) {
	return func(parsed string) ([]byte, bool) {
		contents, readError := os.ReadFile(parsed)
		if readError != nil {
			test.Errorf("reading %s for its digest: %v", parsed, readError)
			return nil, false
		}
		digest := sha256.Sum256(contents)
		return digest[:], true
	}
}

// The placed hand answers a question about a function of a package it holds by the function's place in the package's
// own syntax, only for a function standing: bottom's Double, standing, is answered its own answer and its parameter
// its own, and twice, not standing, is left to the resolver, as is a kind the host never answered of Double and an
// object of top, a package the hand was not handed.
func TestThePlacedHandAnswersAStandingFunctionByItsPlace(test *testing.T) {
	test.Parallel()
	loaded := placedModule(test, test.TempDir(), "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	var bottom *packages.Package
	var top *packages.Package
	for _, each := range loaded {
		switch each.PkgPath {
		case "example.com/placed/bottom":
			bottom = each
		case "example.com/placed/top":
			top = each
		}
	}
	if bottom == nil || top == nil {
		test.Fatal("the module holds no bottom and top")
	}
	file := bottomFileOf(test, loaded)
	doubleAt, twiceAt := strings.Index(placedBottom, "func Double("), strings.Index(placedBottom, "func twice(")
	digest := sha256.Sum256([]byte(placedBottom))
	hand := newPlacedHand()
	hand.hold(bottom, []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: -1,
			Encoded: []byte("true")},
		{Kind: "odd_slice_argument", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: 0,
			Encoded: []byte("false")},
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: twiceAt, Parameter: -1,
			Encoded: []byte("true")},
	}, func(function fact_resolver.Place) bool {
		// Asked of a function, never of a parameter: a parameter's answer stands with its function.
		return function.Offset == doubleAt && function.Parameter == -1
	}, diskDigests(test))
	function := func(scope *types.Scope, name string) *types.Func {
		declared, isFunction := scope.Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("no function %s", name)
		}
		return declared
	}
	double := function(bottom.Types.Scope(), "Double")
	values := double.Signature().Params().At(0)
	for name, asked := range map[string]struct {
		kind   string
		object types.Object
		want   string
	}{
		"Double, standing":               {kind: "function_purity", object: double, want: "true"},
		"Double's parameter":             {kind: "odd_slice_argument", object: values, want: "false"},
		"twice, not standing":            {kind: "function_purity", object: function(bottom.Types.Scope(), "twice")},
		"a kind the host never answered": {kind: "return_nil_ness", object: double},
		"a package not handed":           {kind: "function_purity", object: function(top.Types.Scope(), "Use")},
	} {
		encoded, answered := hand.Answer(asked.kind, asked.object)
		if answered != (asked.want != "") || string(encoded) != asked.want {
			test.Errorf("%s: want %q answered (%t), got %q (%t)", name, asked.want, asked.want != "", encoded,
				answered)
		}
	}
}

// The placed hand refuses every place in a file whose bytes differ in the build from the host's, though an offset still
// lines up (#k7k3wwc): bottom's Double, answered by the host at its `func`, is answered so while the build parses the
// host's bytes, and left to the resolver once the build's bottom.go doubles by three, with Double where it was.
func TestThePlacedHandRefusesAFileWhoseBytesDiffer(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	hostLoaded := placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	file := bottomFileOf(test, hostLoaded)
	doubleAt := strings.Index(placedBottom, "func Double(")
	digest := sha256.Sum256([]byte(placedBottom))
	placed := []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: -1,
			Encoded: []byte("true")},
	}
	var hostBottom *packages.Package
	for _, each := range hostLoaded {
		if each.PkgPath == "example.com/placed/bottom" {
			hostBottom = each
		}
	}
	same := newPlacedHand()
	same.hold(hostBottom, placed, func(fact_resolver.Place) bool { return true }, diskDigests(test))
	if encoded, answered := same.Answer("function_purity", hostBottom.Types.Scope().Lookup("Double")); !answered ||
		string(encoded) != "true" {
		test.Fatalf("want Double answered true while the build parses the host's bytes, got %q (%t)", encoded,
			answered)
	}
	edited := strings.Replace(placedBottom, "value * 2", "value * 3", 1)
	if writeError := os.WriteFile(file, []byte(edited), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	buildLoaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	var bottom *packages.Package
	for _, each := range buildLoaded.Packages {
		if each.PkgPath == "example.com/placed/bottom" {
			bottom = each
		}
	}
	hand := newPlacedHand()
	hand.hold(bottom, placed, func(fact_resolver.Place) bool { return true }, diskDigests(test))
	if encoded, answered := hand.Answer("function_purity", bottom.Types.Scope().Lookup("Double")); answered {
		test.Errorf("want Double, in a file whose bytes differ, left to the resolver, got %q", encoded)
	}
}

// sharedPlacedFiles is a package whose b.go, by a `//line` directive naming a.go, as cmd/cgo writes one into
// _cgo_gotypes.go, declares Second at the very offset and file name a.go declares First at, so neither has a place
// (DeclarationPlaces), and Third after it, placed in a.go's name though parsed from b.go. Value is a variable, which
// has no place either.
var sharedPlacedFiles = map[string]string{
	"go.mod":      "module example.com/shared\n\ngo 1.24\n",
	"shared/a.go": "package shared\n// abcdefghij\nfunc First() {}\n\n// Value has no place.\nvar Value int\n",
	"shared/b.go": "package shared\n//line a.go:3\nfunc Second() {}\n\nfunc Third() {}\n",
}

// sharedPlaced is the loaded shared package, with the names its a.go and b.go were parsed under, failing the test
// unless First and Second share a place, so are not placed, and Third is placed, under a.go's name.
func sharedPlaced(test *testing.T) (shared *packages.Package, aFile string, bFile string) {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, sharedPlacedFiles)
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 || len(loaded.Packages) != 1 {
		test.Fatalf("loading the shared package: %v %+v", loadError, loaded)
	}
	shared = loaded.Packages[0]
	for _, file := range shared.Syntax {
		name := shared.Fset.File(file.FileStart).Name()
		switch filepath.Base(name) {
		case "a.go":
			aFile = name
		case "b.go":
			bFile = name
		}
	}
	places := fact_resolver.DeclarationPlaces(shared.Fset, shared.Syntax, shared.TypesInfo)
	scope := shared.Types.Scope()
	third, thirdPlaced := places[scope.Lookup("Third")]
	_, firstPlaced := places[scope.Lookup("First")]
	_, secondPlaced := places[scope.Lookup("Second")]
	if aFile == "" || bFile == "" || firstPlaced || secondPlaced || !thirdPlaced || third.File != aFile {
		test.Fatalf("want First and Second unplaced and Third placed in %s, got %t, %t and %+v (%t)", aFile,
			firstPlaced, secondPlaced, third, thirdPlaced)
	}
	return shared, aFile, bFile
}

// sharedOffset is where function's `func` opens in the shared package's file named name.
func sharedOffset(test *testing.T, name string, function string) int {
	test.Helper()
	offset := strings.Index(sharedPlacedFiles["shared/"+name], "func "+function+"(")
	if offset < 0 {
		test.Fatalf("%s declares no %s", name, function)
	}
	return offset
}

// fileDigest is the sha256 of the bytes on disk of the file named parsed.
func fileDigest(test *testing.T, parsed string) []byte {
	test.Helper()
	digest, _ := diskDigests(test)(parsed)
	return digest
}

// The placed hand leaves to the resolver every object it holds no place of, never answering one with no bytes, which
// the resolver would take as the zero answer: First and Second, which share a place as a cgo function and its user's
// can, though an answer stands at that place, and Value, a variable, though a malformed answer stands at the zero
// place, the one an unplaced object would look up.
func TestThePlacedHandLeavesAnUnplacedObjectToTheResolver(test *testing.T) {
	test.Parallel()
	shared, aFile, _ := sharedPlaced(test)
	sharedAt := sharedOffset(test, "a.go", "First")
	hand := newPlacedHand()
	hand.hold(shared, []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: aFile, FileDigest: fileDigest(test, aFile), Offset: sharedAt, Parameter: -1,
			Encoded: []byte("true")},
		{Kind: "function_purity", FileDigest: fileDigest(test, aFile), Encoded: []byte("false")},
	}, func(fact_resolver.Place) bool { return true }, diskDigests(test))
	for _, name := range []string{"First", "Second", "Value"} {
		if encoded, answered := hand.Answer("function_purity", shared.Types.Scope().Lookup(name)); answered {
			test.Errorf("%s, which has no place, is answered %q, want it left to the resolver", name, encoded)
		}
	}
}

// The placed hand answers a function only by the digest of the file it was parsed from, by the name it was parsed
// under: Third, parsed from b.go though its `//line` places it in a.go's name, is answered by b.go's digest while the
// build holds one, and left to the resolver when the build holds none, though the digests hand back b.go's bytes.
func TestThePlacedHandAnswersOnlyByTheParsedFilesDigest(test *testing.T) {
	test.Parallel()
	shared, aFile, bFile := sharedPlaced(test)
	placed := []fact_resolver.PlacedAnswer{{Kind: "function_purity", File: aFile, FileDigest: fileDigest(test, bFile),
		Offset: sharedOffset(test, "b.go", "Third"), Parameter: -1, Encoded: []byte("true")}}
	for name, digests := range map[string]func(parsed string) ([]byte, bool){
		"hashed": diskDigests(test),
		"unhashed": func(parsed string) ([]byte, bool) {
			// The bytes are the file's own, so only the word that the build hashed none tells this from hashed.
			digest, _ := diskDigests(test)(parsed)
			return digest, false
		},
	} {
		hand := newPlacedHand()
		hand.hold(shared, placed, func(fact_resolver.Place) bool { return true }, digests)
		encoded, answered := hand.Answer("function_purity", shared.Types.Scope().Lookup("Third"))
		if want := name == "hashed"; answered != want || want && string(encoded) != "true" {
			test.Errorf("%s: Third is answered %q (%t), want answered %t", name, encoded, answered, want)
		}
	}
}

// The placed hand keeps every standing answer however many it rejects before it, as PlacedIn's order, by file and then
// digest, nil first, can put a rejected one first: Double, standing, is answered after an answer with no digest and an
// answer about twice, not standing.
func TestThePlacedHandKeepsAStandingAnswerAfterARejectedOne(test *testing.T) {
	test.Parallel()
	loaded := placedModule(test, test.TempDir(), "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	var bottom *packages.Package
	for _, each := range loaded {
		if each.PkgPath == "example.com/placed/bottom" {
			bottom = each
		}
	}
	if bottom == nil {
		test.Fatal("the module holds no bottom")
	}
	file := bottomFileOf(test, loaded)
	doubleAt, twiceAt := strings.Index(placedBottom, "func Double("), strings.Index(placedBottom, "func twice(")
	digest := sha256.Sum256([]byte(placedBottom))
	hand := newPlacedHand()
	hand.hold(bottom, []fact_resolver.PlacedAnswer{
		{Kind: "return_nil_ness", File: file, Offset: doubleAt, Parameter: -1, Encoded: []byte("false")},
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: twiceAt, Parameter: -1,
			Encoded: []byte("true")},
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: -1,
			Encoded: []byte("true")},
	}, func(function fact_resolver.Place) bool { return function.Offset == doubleAt }, diskDigests(test))
	if encoded, answered := hand.Answer("function_purity", bottom.Types.Scope().Lookup("Double")); !answered ||
		string(encoded) != "true" {
		test.Errorf("Double, standing, is answered %q (%t), want true", encoded, answered)
	}
}

// storedPlacedAt holds the placed answers of a whole entry, and nothing, as a miss, for a key the store holds nothing
// under or an entry whose placed columns do not read, so a caller falls back as it would with no store.
func TestStoredPlacedAtReadsOnlyAWholeEntrysPlacedAnswers(test *testing.T) {
	test.Parallel()
	store := cache.Open(test.TempDir(), nil)
	answers := []fact_resolver.PlacedAnswer{{Kind: "function_purity", File: "bottom.go", FileDigest: []byte("digest"),
		Offset: 16, Parameter: -1, Encoded: []byte("true")}}
	put := func(key cache.Key, placed storedPlaced) {
		body, encodeError := encodeBody(cacheEntry{Version: CacheFormatVersion, Placed: placed})
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		record, recordError := encodeRecord(entryRecord{Version: CacheFormatVersion, Body: contentDigest(body),
			Inline: body})
		if recordError != nil {
			test.Fatal(recordError)
		}
		if putError := store.Put(key, record); putError != nil {
			test.Fatal(putError)
		}
	}
	whole, malformed, absent := cache.DigestOf([]byte("whole")), cache.DigestOf([]byte("malformed")),
		cache.DigestOf([]byte("absent"))
	put(whole, storedPlacedOf(answers))
	broken := storedPlacedOf(answers)
	broken.Values = nil
	put(malformed, broken)
	if placed, stored := storedPlacedAt(store, whole); !stored || !reflect.DeepEqual(placed, answers) {
		test.Errorf("the whole entry holds %+v (%t), want %+v", placed, stored, answers)
	}
	for name, key := range map[string]cache.Key{"malformed": malformed, "absent": absent} {
		if placed, stored := storedPlacedAt(store, key); stored || placed != nil {
			test.Errorf("the %s entry holds %+v (%t), want a miss", name, placed, stored)
		}
	}
}
