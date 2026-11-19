package go_engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// factReachFixture is one package of the fact-reach fixture, checked from its source by the test itself, with no
// engine run: its file set, its types, stage 1's record of it, and the place of each function declared with a body, by
// its name, a method's as `Type.Method`, read from the syntax and never from the record under test.
type factReachFixture struct {
	fileSet *token.FileSet
	own     *types.Package
	record  *build_reads.FunctionUses
	places  map[string]fact_reach.Place
}

// checkedFactReachFixture is the package of path in directory, a directory of the fact-reach fixture, empty for its
// root. The package imports nothing, so it is checked with no importer, and go/types takes a function without a body
// as the compiler takes one beside assembly.
func checkedFactReachFixture(test *testing.T, directory string, path string) *factReachFixture {
	test.Helper()

	names, globError := filepath.Glob(filepath.Join("testdata", "fact-reach", directory, "*.go"))
	if globError != nil || len(names) == 0 {
		test.Fatalf("the fixture's files in %q: %v (%v)", directory, names, globError)
	}
	fixture := &factReachFixture{fileSet: token.NewFileSet(), places: map[string]fact_reach.Place{}}
	var files []*ast.File
	for _, name := range names {
		file, parseError := parser.ParseFile(fixture.fileSet, name, nil,
			parser.SkipObjectResolution|parser.ParseComments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		files = append(files, file)
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			declared := function.Name.Name
			if function.Recv != nil {
				receiver, isNamed := function.Recv.List[0].Type.(*ast.Ident)
				if !isNamed {
					test.Fatalf("%s's receiver is no type's name, which this fixture's reader does not read", declared)
				}
				declared = receiver.Name + "." + declared
			}
			fixture.places[declared] = fact_reach.Place{File: name,
				Offset: fixture.fileSet.Position(function.Pos()).Offset}
		}
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	own, checkError := (&types.Config{}).Check(path, fixture.fileSet, files, info)
	if checkError != nil {
		test.Fatalf("type-checking %s: %v", path, checkError)
	}
	fixture.own = own
	reads := &build_reads.Reads{}
	reads.RecordTypes(own, info, files)
	record, isRecorded := reads.FunctionUses()
	if !isRecorded {
		test.Fatalf("want %s's function uses recorded", path)
	}
	fixture.record = record
	return fixture
}

// function is the function of name, a method's as `Type.Method`.
func (fixture *factReachFixture) function(test *testing.T, name string) *types.Func {
	test.Helper()

	var found types.Object
	if receiver, method, isMethod := strings.Cut(name, "."); isMethod {
		found, _, _ = types.LookupFieldOrMethod(fixture.own.Scope().Lookup(receiver).Type(), false, fixture.own, method)
	} else {
		found = fixture.own.Scope().Lookup(name)
	}
	function, isFunction := found.(*types.Func)
	if !isFunction {
		test.Fatalf("the fixture declares no function %s", name)
	}
	return function
}

// key is the key of object as a renderer of the fixture's renders it.
func (fixture *factReachFixture) key(test *testing.T, object types.Object) fact_reach.Object {
	test.Helper()

	key, isKnown := factReachObject(build_reads.NewIdentityRenderer(fixture.fileSet, nil).Object(object))
	if !isKnown {
		test.Fatalf("want %s's identity known", object.Name())
	}
	return key
}

// hosted is the fixture's host side, with answers the host's.
func (fixture *factReachFixture) hosted(answers []check.ExportedFact) *factReachPackage {
	return newFactReachPackage(fixture.fileSet, fixture.record, answers,
		build_reads.NewIdentityRenderer(fixture.fileSet, nil))
}

// placesOf are the places of names, in their order.
func (fixture *factReachFixture) placesOf(test *testing.T, names ...string) []fact_reach.Place {
	test.Helper()

	var places []fact_reach.Place
	for _, name := range names {
		place, isDeclared := fixture.places[name]
		if !isDeclared {
			test.Fatalf("the fixture declares no body %s", name)
		}
		places = append(places, place)
	}
	return places
}

// purity is a hand-made answer of function_purity about object, encoded as written.
func purity(object types.Object, encoded string) check.ExportedFact {
	return check.ExportedFact{Analyzer: "function_purity", Object: object, Answer: true, Encoded: []byte(encoded)}
}

// An object's users are the bodies that name or select it, each once, in the order they are declared: a function
// called, a method called and one taken as a value, each by the method's own key, a generic function called by an
// instance, by the generic's, and a function declared without a body, which is used though it is no place. A value's
// initializer is no body's place, so started, which calls size, is no user of it, and a type only a signature names
// has no user, a signature's uses being no body's.
func TestAnObjectsUsersAreTheBodiesThatNameOrSelectIt(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "", "example.com/facts")
	hosted := fixture.hosted(nil)
	for _, each := range []struct {
		object types.Object
		users  []string
	}{
		{fixture.function(test, "size"), []string{"Warm"}},
		{fixture.function(test, "platformSize"), []string{"size"}},
		{fixture.function(test, "Box.Open"), []string{"Use"}},
		{fixture.function(test, "Box.Close"), []string{"Closer"}},
		{fixture.function(test, "First"), []string{"Leading"}},
		{fixture.function(test, "declared"), []string{"Sized"}},
		{fixture.function(test, "Open"), nil},
		{fixture.own.Scope().Lookup("Label"), nil},
	} {
		got := hosted.Users(fixture.key(test, each.object))
		if want := fixture.placesOf(test, each.users...); !slices.Equal(got, want) {
			test.Errorf("the users of %s are %v, want %v, the bodies of %v", each.object, got, want, each.users)
		}
	}
	if hosted.unplaced != 0 {
		test.Errorf("want nothing unplaced with no answer, got %d", hosted.unplaced)
	}
	// Each function with a body stands at its place, by its key, and one without stands nowhere.
	for name, want := range fixture.places {
		if got, isPlaced := hosted.placeOf(fixture.key(test, fixture.function(test, name))); !isPlaced || got != want {
			test.Errorf("%s is placed at %v (%t), want %v", name, got, isPlaced, want)
		}
	}
	if place, isPlaced := hosted.placeOf(fixture.key(test, fixture.function(test, "declared"))); isPlaced {
		test.Errorf("declared, which has no body, is placed at %v, want nowhere", place)
	}
}

// An answer is recorded as a question under its function's place and about its function, so the function's users are
// who a differing answer reaches: one of the function itself with no parameter, one of a parameter with its index and
// still about the function declaring it. A place's questions come in the order of their keys whatever order the
// answers came in, and an answer handed twice is one question. What a build is asked of each is kept by its key: the
// kind, the function's identity and the host's answer.
func TestAnAnswerIsRecordedUnderItsFunctionsPlaceAndAboutItsFunction(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "", "example.com/facts")
	size, pair := fixture.function(test, "size"), fixture.function(test, "Pair")
	oddSlice := check.ExportedFact{Analyzer: "odd_slice_argument", Object: pair.Signature().Params().At(0),
		Answer: true, Encoded: []byte(`{"odd":true}`)}
	hosted := fixture.hosted([]check.ExportedFact{oddSlice, purity(size, "true"), purity(pair, "false"),
		purity(size, "true")})
	if hosted.unplaced != 0 {
		test.Errorf("want every answer placed, got %d unplaced", hosted.unplaced)
	}
	renderer := build_reads.NewIdentityRenderer(fixture.fileSet, nil)

	ofSize := hosted.Recorded(fixture.places["size"])
	if len(ofSize) != 1 || ofSize[0].Kind != "function_purity" || ofSize[0].About != fixture.key(test, size) {
		test.Fatalf("size's place holds %+v, want one question of function_purity about size", ofSize)
	}
	asked, isRecorded := hosted.question(ofSize[0].Key)
	if !isRecorded || asked.kind != "function_purity" || !asked.function.Equal(renderer.Object(size)) ||
		asked.parameter != -1 || string(asked.encoded) != "true" {
		test.Errorf("size's question asks %+v (%t), want function_purity of size itself, answered true", asked,
			isRecorded)
	}

	ofPair := hosted.Recorded(fixture.places["Pair"])
	if len(ofPair) != 2 || ofPair[0].Kind != "function_purity" || ofPair[1].Kind != "odd_slice_argument" ||
		ofPair[0].Key == ofPair[1].Key {
		test.Fatalf("Pair's place holds %+v, want its purity's question, then its parameter's", ofPair)
	}
	for _, question := range ofPair {
		if question.About != fixture.key(test, pair) {
			test.Errorf("the question of %s under Pair's place is about %q, want Pair's key %q", question.Kind,
				question.About, fixture.key(test, pair))
		}
	}
	asked, isRecorded = hosted.question(ofPair[1].Key)
	if !isRecorded || asked.kind != "odd_slice_argument" || !asked.function.Equal(renderer.Object(pair)) ||
		asked.parameter != 0 || string(asked.encoded) != `{"odd":true}` {
		test.Errorf("the parameter's question asks %+v (%t), want odd_slice_argument of Pair's parameter 0", asked,
			isRecorded)
	}
	if asked, isRecorded = hosted.question(ofPair[0].Key); !isRecorded || asked.parameter != -1 {
		test.Errorf("Pair's own question asks %+v (%t), want no parameter", asked, isRecorded)
	}
	if _, isRecorded = hosted.question("function_purity"); isRecorded {
		test.Error("a key no answer made names a question")
	}
	if got := hosted.Recorded(fixture.places["Warm"]); len(got) != 0 {
		test.Errorf("Warm's place, which no answer is about, holds %+v", got)
	}
}

// An answer no body's place can take is counted unplaced, each kind of one by itself: about the package, a result, a
// receiver, a package-level value, and a function no declaration of the record declares. An answer about a function
// declared without a body is neither recorded nor counted, its answer being its types' alone.
func TestAnAnswerNoPlaceCanTakeIsCountedUnplaced(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "", "example.com/facts")
	use, open := fixture.function(test, "Use"), fixture.function(test, "Box.Open")
	elsewhere := types.NewFunc(token.NoPos, fixture.own, "elsewhere", types.NewSignatureType(nil, nil, nil, nil, nil,
		false))
	for _, each := range []struct {
		name   string
		object types.Object
	}{
		{"the package", nil},
		{"a result", use.Signature().Results().At(0)},
		{"a receiver", open.Signature().Recv()},
		{"a package-level value", fixture.own.Scope().Lookup("started")},
		{"a function no declaration declares", elsewhere},
	} {
		hosted := fixture.hosted([]check.ExportedFact{purity(each.object, "true")})
		if hosted.unplaced != 1 || len(hosted.questions) != 0 {
			test.Errorf("an answer about %s: %d unplaced and %d questions, want 1 and none", each.name,
				hosted.unplaced, len(hosted.questions))
		}
	}
	hosted := fixture.hosted([]check.ExportedFact{purity(fixture.function(test, "declared"), "false")})
	if hosted.unplaced != 0 || len(hosted.questions) != 0 {
		test.Errorf("an answer about declared, which has no body: %d unplaced and %d questions, want neither",
			hosted.unplaced, len(hosted.questions))
	}
}

// Two keys are equal exactly when their identities are (build_reads.Identity.Equal): two renders of one object give
// one key, a function and a method of one name give two, an unknown identity has no key, and an identity that differs
// in any one field Equal compares has another key, a field added to Equal and left out of the key failing here. The
// parts are joined so that no two splits of the same letters meet.
func TestTwoKeysAreEqualExactlyWhenTheirIdentitiesAre(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "", "example.com/facts")
	size := fixture.function(test, "size")
	first, second := build_reads.NewIdentityRenderer(fixture.fileSet, nil).Object(size),
		build_reads.NewIdentityRenderer(fixture.fileSet, nil).Object(size)
	firstKey, firstKnown := factReachObject(first)
	secondKey, secondKnown := factReachObject(second)
	if !firstKnown || !secondKnown || firstKey != secondKey || !first.Equal(second) {
		test.Errorf("two renders of size give the keys %q and %q, want one key", firstKey, secondKey)
	}
	if function, method := fixture.key(test, fixture.function(test, "Open")),
		fixture.key(test, fixture.function(test, "Box.Open")); function == method {
		test.Errorf("the function Open and the method Open share the key %q", function)
	}
	if key, isKnown := factReachObject(build_reads.Identity{Kind: build_reads.IdentityUnknown}); isKnown || key != "" {
		test.Errorf("an unknown identity has the key %q, want none", key)
	}

	base := build_reads.Identity{Kind: build_reads.IdentityMethod, PackagePath: "example.com/facts", Path: "Box",
		Method: "Open", File: "facts.go", Ordinal: 1, Digest: "digest"}
	baseKey, _ := factReachObject(base)
	fields := reflect.TypeFor[build_reads.Identity]()
	for index := range fields.NumField() {
		if fields.Field(index).Name == "_" {
			continue
		}
		varied := base
		field := reflect.ValueOf(&varied).Elem().Field(index)
		switch field.Kind() {
		case reflect.String:
			field.SetString(field.String() + "x")
		case reflect.Int:
			field.SetInt(field.Int() + 1)
		default:
			test.Fatalf("Identity's field %s is of a kind this test does not vary", fields.Field(index).Name)
		}
		variedKey, _ := factReachObject(varied)
		if (baseKey == variedKey) != base.Equal(varied) {
			test.Errorf("identities differing in %s: equal keys %t, Equal %t", fields.Field(index).Name,
				baseKey == variedKey, base.Equal(varied))
		}
	}

	shorter := build_reads.Identity{Kind: build_reads.IdentityObject, PackagePath: "example.com/a", Path: "bc"}
	longer := build_reads.Identity{Kind: build_reads.IdentityObject, PackagePath: "example.com/ab", Path: "c"}
	shorterKey, _ := factReachObject(shorter)
	longerKey, _ := factReachObject(longer)
	if shorterKey == longerKey || shorter.Equal(longer) {
		test.Errorf("%v and %v, whose parts written in a row read alike, share the key %q", shorter, longer, shorterKey)
	}
}

// A body with a use no identity names is a user of every object, one nothing names included, after the object's own
// users and never twice, though it calls helper itself.
func TestABodyWithAUseNoIdentityNamesUsesEveryObject(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "unfollowed", "example.com/facts/unfollowed")
	hosted := fixture.hosted(nil)
	for _, each := range []struct {
		name  string
		users []string
	}{
		{"helper", []string{"Caller", "Configured"}},
		{"alone", []string{"Configured"}},
	} {
		got := hosted.Users(fixture.key(test, fixture.function(test, each.name)))
		if want := fixture.placesOf(test, each.users...); !slices.Equal(got, want) {
			test.Errorf("the users of %s are %v, want %v, the bodies of %v", each.name, got, want, each.users)
		}
	}
	got := hosted.Users("an object of another package")
	if want := fixture.placesOf(test, "Configured"); !slices.Equal(got, want) {
		test.Errorf("the users of an object the package never names are %v, want Configured's body alone, %v", got,
			want)
	}
}

// The fixed point over the host's side (fact_reach.Settle): size is reached, so its recorded purity is asked in the
// build, and where that differs Warm, its one user, leaves the standing set, with size named as what took it out.
// Warm's own purity reads equal, so nothing above it moves and every other body stands.
func TestADifferingAnswerAboutSizeTakesWarmOutAndNothingElse(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "", "example.com/facts")
	size, warm := fixture.function(test, "size"), fixture.function(test, "Warm")
	hosted := fixture.hosted([]check.ExportedFact{purity(size, "true"), purity(warm, "false")})
	standing := map[fact_reach.Place]bool{}
	for name, place := range fixture.places {
		if name != "size" {
			standing[place] = true
		}
	}
	bodies := len(standing)
	var asked []string
	settlement := fact_reach.Settle(standing, fixture.placesOf(test, "size"), nil, hosted,
		func(question fact_reach.Question) fact_reach.Comparison {
			asked = append(asked, question.Kind+" of "+hosted.questions[question.Key].function.String())
			if question.About == fixture.key(test, size) {
				return fact_reach.Differs
			}
			return fact_reach.Equal
		})
	wantRemoved := []fact_reach.Removal{{Place: fixture.places["Warm"], Object: fixture.key(test, size),
		Reason: "answer function_purity"}}
	if !reflect.DeepEqual(settlement.Removed, wantRemoved) {
		test.Errorf("removed %v, want Warm alone, %v", settlement.Removed, wantRemoved)
	}
	if standing[fixture.places["Warm"]] || len(standing) != bodies-1 {
		test.Errorf("%d of %d bodies stand, Warm among them %t, want every one but Warm: %v", len(standing), bodies,
			standing[fixture.places["Warm"]], slices.Collect(maps.Keys(standing)))
	}
	if settlement.Asked != 2 || len(asked) != 2 || settlement.Differing["function_purity"] != 1 {
		test.Errorf("asked %v with %v differing, want size's purity then Warm's, one differing", asked,
			settlement.Differing)
	}
}
