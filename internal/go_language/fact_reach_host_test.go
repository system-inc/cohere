package go_language

import (
	"fmt"
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

	"github.com/system-inc/cohere/internal/go_language/build_reads"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// factReachFixture is one package of the fact-reach fixture, checked from its source by the test itself, with no
// cohere-go run: its file set, its types, its type information, which alone names an `init` function's object, stage
// 1's record of it, and the place of each function declared with a body, by its name, a method's as `Type.Method`, and
// in the order declared, file by file in the order of their names, read from the syntax and never from the record under
// test.
type factReachFixture struct {
	fileSet  *token.FileSet
	own      *types.Package
	info     *types.Info
	record   *build_reads.FunctionUses
	places   map[string]fact_reach.Place
	declared []fact_reach.Place
}

// checkedFactReachFixture is the package of path in directory, a directory of the fact-reach fixture, empty for its
// root, checked in the file set of below, the packages it imports, so one renderer places the objects of them all,
// and in a file set of its own with none. go/types takes a function without a body as the compiler takes one beside
// assembly.
func checkedFactReachFixture(
	test *testing.T,
	directory string,
	path string,
	below ...*factReachFixture,
) *factReachFixture {
	test.Helper()

	names, globError := filepath.Glob(filepath.Join("testdata", "fact-reach", directory, "*.go"))
	if globError != nil || len(names) == 0 {
		test.Fatalf("the fixture's files in %q: %v (%v)", directory, names, globError)
	}
	fixture := &factReachFixture{fileSet: token.NewFileSet(), places: map[string]fact_reach.Place{}}
	checked := map[string]*types.Package{}
	for _, imported := range below {
		fixture.fileSet = imported.fileSet
		checked[imported.own.Path()] = imported.own
	}
	importer := importerByPath(func(path string) (*types.Package, error) {
		if imported, isChecked := checked[path]; isChecked {
			return imported, nil
		}
		return nil, fmt.Errorf("the fixture has no package %s checked", path)
	})
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
			// A place is in the file a //line directive names, at the offset in the file as parsed: a position's
			// Filename follows the directive, and its Offset never does.
			position := fixture.fileSet.Position(function.Pos())
			place := fact_reach.Place{File: position.Filename, Offset: position.Offset}
			fixture.places[declared] = place
			fixture.declared = append(fixture.declared, place)
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
	own, checkError := (&types.Config{Importer: importer}).Check(path, fixture.fileSet, files, info)
	if checkError != nil {
		test.Fatalf("type-checking %s: %v", path, checkError)
	}
	fixture.own, fixture.info = own, info
	reads := &build_reads.Reads{}
	reads.RecordFunctionUses()
	reads.RecordTypes(own, info, files)
	record, isRecorded := reads.FunctionUses()
	if !isRecorded {
		test.Fatalf("want %s's function uses recorded", path)
	}
	fixture.record = record
	return fixture
}

// checkedFactsFixture is the fixture's root package, facts, and lower, the package it imports.
func checkedFactsFixture(test *testing.T) (*factReachFixture, *factReachFixture) {
	test.Helper()

	lower := checkedFactReachFixture(test, "lower", "example.com/facts/lower")
	return checkedFactReachFixture(test, "", "example.com/facts", lower), lower
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

// inputs are what the fixture's host side is made from with no answer of its own or imported, no object bound to the
// order, no function holding the no-return fact and no body holding anything presence reads, its bodies read, for a
// test to add its own to.
func (fixture *factReachFixture) inputs() factReachInputs {
	return factReachInputs{own: fixture.own, fileSet: fixture.fileSet, record: fixture.record,
		renderer: build_reads.NewIdentityRenderer(fixture.fileSet, nil),
		noReturn: func(*types.Func) bool { return false }, presenceRead: true}
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

// standing is every body of the fixture but those of names, as a reach that reached those alone leaves them.
func (fixture *factReachFixture) standing(names ...string) map[fact_reach.Place]bool {
	standing := map[fact_reach.Place]bool{}
	for name, place := range fixture.places {
		if !slices.Contains(names, name) {
			standing[place] = true
		}
	}
	return standing
}

// purity is a hand-made answer of function_purity about object, encoded as written.
func purity(object types.Object, encoded string) check.ExportedFact {
	return check.ExportedFact{Analyzer: "function_purity", Object: object, Answer: true, Encoded: []byte(encoded)}
}

// An object's users are the bodies that name or select it, each once, in the order they are declared: a function
// called, a method called and one taken as a value, each by the method's own key, a generic function called by an
// instance, by the generic's, a function declared without a body, which is used though it is no place, an
// interface's method and another package's function. A value's initializer is no body's place, so started, which
// calls size, is no user of it, and a type only a signature names has no user, a signature's uses being no body's.
func TestAnObjectsUsersAreTheBodiesThatNameOrSelectIt(test *testing.T) {
	test.Parallel()

	fixture, lower := checkedFactsFixture(test)
	hosted := newFactReachPackage(fixture.inputs())
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
		{fixture.function(test, "Opener.Open"), []string{"Through"}},
		{lower.function(test, "Depth"), []string{"Deep"}},
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
}

// A package's variable initializers are one asker of the side's (#bed0mnk): started, whose initializer calls size, is
// its one place, in no object's users and no place of the package's, so it never stands, and it leaves for a
// settlement that named size, for whatever reason, and for none that named only what no initializer uses, or nothing.
func TestAPackagesVariableInitializersAreOneAskerThatIsNoPlace(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	hosted := newFactReachPackage(fixture.inputs())
	size, warm := fixture.key(test, fixture.function(test, "size")), fixture.key(test, fixture.function(test, "Warm"))
	if len(hosted.initializers.places) != 1 || hosted.initializers.everything || !hosted.initializers.uses[size] ||
		len(hosted.initializers.uses) != 1 {
		test.Fatalf("the initializers are %+v, want started's one place and its one use, size", hosted.initializers)
	}
	started := hosted.initializers.places[0]
	// started's name is at byte 1066 of facts.go, where the census places a value spec.
	want := fact_reach.Place{File: filepath.Join("testdata", "fact-reach", "facts.go"), Offset: 1066}
	if started != want {
		test.Errorf("started is placed at %v, want %v", started, want)
	}
	if slices.Contains(hosted.Places(), started) || slices.Contains(hosted.Users(size), started) {
		test.Errorf("started's place is among the package's places %t and size's users %t, want neither",
			slices.Contains(hosted.Places(), started), slices.Contains(hosted.Users(size), started))
	}
	// A build settles the package for its initializers where the reach leaves started unreached, and for no other
	// place's sake.
	if !hosted.initializers.anyAmong(map[fact_reach.Place]bool{started: true}) ||
		hosted.initializers.anyAmong(map[fact_reach.Place]bool{hosted.Places()[0]: true}) ||
		hosted.initializers.anyAmong(nil) {
		test.Errorf("the initializers are among the unreached places with started %t, with a function's body alone "+
			"%t and with none %t, want only the first",
			hosted.initializers.anyAmong(map[fact_reach.Place]bool{started: true}),
			hosted.initializers.anyAmong(map[fact_reach.Place]bool{hosted.Places()[0]: true}),
			hosted.initializers.anyAmong(nil))
	}
	for _, each := range []struct {
		name  string
		named []fact_reach.Named
		want  bool
	}{
		{"size named", []fact_reach.Named{{Object: warm, Reason: "order"}, {Object: size, Reason: "no-return"}}, true},
		{"Warm alone named", []fact_reach.Named{{Object: warm, Reason: "no-return"}}, false},
		{"nothing named", nil, false},
	} {
		if got := hosted.initializers.leaveFor(each.named); got != each.want {
			test.Errorf("with %s the initializers leave %t, want %t", each.name, got, each.want)
		}
	}
	// An initializer with a use no identity names, as the unfollowed fixture's initial reads a field of a struct
	// type with no name, uses every object, and leaves for anything named.
	unfollowed := newFactReachPackage(
		checkedFactReachFixture(test, "unfollowed", "example.com/facts/unfollowed").inputs()).initializers
	if !unfollowed.everything || len(unfollowed.places) != 1 ||
		!unfollowed.leaveFor([]fact_reach.Named{{Object: warm, Reason: "no-return"}}) || unfollowed.leaveFor(nil) {
		test.Errorf("the unfollowed fixture's initializers are %+v, want its one place, a user of every object, "+
			"which leaves for anything named and for nothing else", unfollowed)
	}
}

// The package's used objects are every object a body uses, each once, in the order of their keys, with the identity
// that key is made of (#xeafx79): the keys Users is asked by, so a build that names one takes that object's users out.
// A type only a signature names is no body's use, and none of them.
func TestAPackagesUsedObjectsAreEveryObjectItsBodiesUseByKey(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	hosted := newFactReachPackage(fixture.inputs())
	used := hosted.Used()
	keys := make([]fact_reach.Object, 0, len(used))
	for _, object := range used {
		keys = append(keys, object.Object)
		if key, isKnown := factReachObject(object.Identity); !isKnown || key != object.Object {
			test.Errorf("the used object %q has the identity %s, whose key is %q", object.Object, object.Identity, key)
		}
	}
	if want := slices.Sorted(maps.Keys(hosted.users)); !slices.Equal(keys, want) {
		test.Errorf("the used objects are %q, want the keys of the users, in order, %q", keys, want)
	}
	if !slices.Contains(keys, fixture.key(test, fixture.function(test, "size"))) {
		test.Errorf("size, which Warm calls, is no used object")
	}
	if slices.Contains(keys, fixture.key(test, fixture.own.Scope().Lookup("Label"))) {
		test.Errorf("Label, which only a signature names, is a used object")
	}
}

// A package whose bodies' presence the host never read fails closed (#xeafx79): no record would read as no body
// holding anything, so a standing body that starts a goroutine or writes its own any would stand unproven. Its side
// counts it unplaced, and every build lints it whole, while the same package read, with nothing in its bodies,
// stands as the reach leaves it.
func TestAPackageWhosePresenceWasNeverReadIsLintedWhole(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	read := newFactReachPackage(fixture.inputs())
	if _, partial := read.inBuild(fixture.standing("size"), true).Standing(); !partial || read.unplaced != 0 {
		test.Fatalf("read, the package is partial %t with %d unplaced, want partial and none", partial, read.unplaced)
	}
	inputs := fixture.inputs()
	inputs.presenceRead = false
	unread := newFactReachPackage(inputs)
	standing, partial := unread.inBuild(fixture.standing("size"), true).Standing()
	if partial || standing != nil || unread.unplaced != 1 {
		test.Errorf("never read, the package is partial %t, standing %v, with %d unplaced, want linted whole and one "+
			"unplaced", partial, standing, unread.unplaced)
	}
}

// A package's places are its function bodies in the order its files declare them, a function declared without a body
// not among them.
func TestAPackagesPlacesAreItsFunctionBodiesInTheOrderDeclared(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	if got := newFactReachPackage(fixture.inputs()).Places(); !slices.Equal(got, fixture.declared) {
		test.Errorf("the places are %v, want the %d bodies in the order declared, %v", got, len(fixture.declared),
			fixture.declared)
	}
}

// A body declared after a //line directive is placed in the file the directive names, at its offset in the file as
// parsed, as stage 1 places its unit (build_dependence's newUnit), and an answer about its function is recorded under
// that place. Renamed's `func` is at byte 207 of renamed.go, after a directive naming renamed_as_written.go.
func TestABodyAfterALineDirectiveIsPlacedInTheFileItNames(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	inputs := fixture.inputs()
	inputs.answers = []check.ExportedFact{purity(fixture.function(test, "Renamed"), "true")}
	hosted := newFactReachPackage(inputs)
	want := fact_reach.Place{File: filepath.Join("testdata", "fact-reach", "renamed_as_written.go"), Offset: 207}
	if places := hosted.Places(); len(places) == 0 || places[len(places)-1] != want {
		test.Errorf("the places are %v, want the last, Renamed's, %v", places, want)
	}
	if recorded := hosted.Recorded(want); len(recorded) != 1 || recorded[0].Kind != "function_purity" {
		test.Errorf("Renamed's place holds %+v, want its one question of function_purity", recorded)
	}
}

// The functions a no-return fact is compared on are each function the package declares with a body, where it is
// declared, and each function a body uses, where it is first used: a method, a generic function's origin, a function
// declared without a body and another package's functions among them, each once however many bodies use it, with the
// fact the host holds of it and the identity a build finds its own by. An interface's method, called in Through's
// body, is none of them, there being no body for a fact to be about.
func TestTheFunctionsAFactIsComparedOnAreEachDeclaredOrUsedOnce(test *testing.T) {
	test.Parallel()

	fixture, lower := checkedFactsFixture(test)
	want := []string{"platformSize", "size", "Warm", "Pair", "Box.Open", "Box.Close", "Open", "Use", "Closer", "First",
		"Leading", "Sized", "declared", "Labelled", "Through", "Deep", "lower.Depth", "Halved", "lower.Halve",
		"Joined", "Renamed"}
	objects := map[string]*types.Func{}
	names := map[fact_reach.Object]string{}
	for _, name := range want {
		function := fixture.function
		lookup := name
		if inLower, isLower := strings.CutPrefix(name, "lower."); isLower {
			function, lookup = lower.function, inLower
		}
		objects[name] = function(test, lookup)
		names[fixture.key(test, objects[name])] = name
	}
	noReturn := map[*types.Func]bool{objects["declared"]: true, objects["lower.Depth"]: true}
	inputs := fixture.inputs()
	inputs.noReturn = func(function *types.Func) bool { return noReturn[function] }
	functions := newFactReachPackage(inputs).Functions()

	var got []string
	renderer := build_reads.NewIdentityRenderer(fixture.fileSet, nil)
	for _, function := range functions {
		name, isExpected := names[function.Object]
		if !isExpected {
			name = "one unexpected, " + function.Identity.String()
		}
		got = append(got, name)
		if !isExpected {
			continue
		}
		if function.NoReturn != noReturn[objects[name]] {
			test.Errorf("%s holds the no-return fact %t, want the host's, %t", name, function.NoReturn,
				noReturn[objects[name]])
		}
		if !function.Identity.Equal(renderer.Object(objects[name])) {
			test.Errorf("%s has the identity %v, want its own", name, function.Identity)
		}
	}
	if !slices.Equal(got, want) {
		test.Errorf("the functions are\n%v\nwant, in the order first met,\n%v", got, want)
	}
	interfaceMethod := fixture.key(test, fixture.function(test, "Opener.Open"))
	if slices.ContainsFunc(functions, func(function fact_reach.Function) bool {
		return function.Object == interfaceMethod
	}) {
		test.Error("Opener.Open, an interface's method, is among the functions, want it left out")
	}
}

// An answer is recorded as a question under its function's place and about its function, so the function's users are
// who a differing answer reaches: one of the function itself with no parameter, one of a parameter with its index and
// still about the function declaring it. A place's questions come in the order of their keys whatever order the
// answers came in, and an answer handed twice is one question. What a build is asked of each is kept by its key: the
// kind, the function's identity and the host's answer.
func TestAnAnswerIsRecordedUnderItsFunctionsPlaceAndAboutItsFunction(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	size, pair := fixture.function(test, "size"), fixture.function(test, "Pair")
	oddSlice := check.ExportedFact{Analyzer: "odd_slice_argument", Object: pair.Signature().Params().At(0),
		Answer: true, Encoded: []byte(`{"odd":true}`)}
	inputs := fixture.inputs()
	inputs.answers = []check.ExportedFact{oddSlice, purity(size, "true"), purity(pair, "false"), purity(size, "true")}
	hosted := newFactReachPackage(inputs)
	if hosted.unplaced != 0 {
		test.Errorf("want every answer placed, got %d unplaced", hosted.unplaced)
	}
	renderer := build_reads.NewIdentityRenderer(fixture.fileSet, nil)

	ofSize := hosted.Recorded(fixture.places["size"])
	if len(ofSize) != 1 || ofSize[0].Kind != "function_purity" || ofSize[0].About != fixture.key(test, size) {
		test.Fatalf("size's place holds %+v, want one question of function_purity about size", ofSize)
	}
	asking, isAsked := hosted.Asking(ofSize[0].Key)
	if !isAsked || asking.Kind != "function_purity" || !asking.Function.Equal(renderer.Object(size)) ||
		asking.Parameter != -1 || string(asking.Encoded) != "true" {
		test.Errorf("size's question asks %+v (%t), want function_purity of size itself, answered true", asking,
			isAsked)
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
	asking, isAsked = hosted.Asking(ofPair[1].Key)
	if !isAsked || asking.Kind != "odd_slice_argument" || !asking.Function.Equal(renderer.Object(pair)) ||
		asking.Parameter != 0 || string(asking.Encoded) != `{"odd":true}` {
		test.Errorf("the parameter's question asks %+v (%t), want odd_slice_argument of Pair's parameter 0", asking,
			isAsked)
	}
	if asking, isAsked = hosted.Asking(ofPair[0].Key); !isAsked || asking.Parameter != -1 {
		test.Errorf("Pair's own question asks %+v (%t), want no parameter", asking, isAsked)
	}
	if _, isAsked = hosted.Asking("function_purity"); isAsked {
		test.Error("a key no answer made names a question")
	}
	if got := hosted.Recorded(fixture.places["Warm"]); len(got) != 0 {
		test.Errorf("Warm's place, which no answer is about, holds %+v", got)
	}
	if got := hosted.Imported(); len(got) != 0 {
		test.Errorf("with no imported answer, the imported questions are %+v", got)
	}
}

// An answer no body's place can take is counted unplaced, each kind of one by itself: about the package, and about a
// function no declaration of the record declares. An answer about a variable that is no declared function's parameter
// is left out and counted nowhere, since the host hands a mark for a function literal's parameter too and one closure
// must not make its package lint whole: a literal's parameter, a result, a receiver and a package-level value. An
// answer about a function declared without a body is neither recorded nor counted, its answer being its types' alone.
// So is one about a function no declaration declares that its kind marks as its types' alone, as purity's about an
// `init` function is, while the same answer unmarked is counted, and a mark never excuses an answer about the package.
func TestAnAnswerNoPlaceCanTakeIsCountedUnplaced(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	use, open := fixture.function(test, "Use"), fixture.function(test, "Box.Open")
	elsewhere := types.NewFunc(token.NoPos, fixture.own, "elsewhere", types.NewSignatureType(nil, nil, nil, nil, nil,
		false))
	literal, isSignature := fixture.function(test, "Joined").Scope().Lookup("count").Type().(*types.Signature)
	if !isSignature {
		test.Fatal("want count, in Joined's body, of a function literal's type")
	}
	for _, each := range []struct {
		name       string
		object     types.Object
		typesAlone bool
		unplaced   int
	}{
		{"the package", nil, false, 1},
		{"the package, marked as its types' alone", nil, true, 1},
		{"a function no declaration declares", elsewhere, false, 1},
		{"a function no declaration declares, marked as its types' alone", elsewhere, true, 0},
		{"a function literal's parameter", literal.Params().At(0), false, 0},
		{"a result", use.Signature().Results().At(0), false, 0},
		{"a receiver", open.Signature().Recv(), false, 0},
		{"a package-level value", fixture.own.Scope().Lookup("started"), false, 0},
		{"a function declared without a body", fixture.function(test, "declared"), false, 0},
	} {
		inputs := fixture.inputs()
		answer := purity(each.object, "true")
		answer.TypesAlone = each.typesAlone
		inputs.answers = []check.ExportedFact{answer}
		hosted := newFactReachPackage(inputs)
		if hosted.unplaced != each.unplaced || len(hosted.askings) != 0 {
			test.Errorf("an answer about %s: %d unplaced and %d questions, want %d and none", each.name,
				hosted.unplaced, len(hosted.askings), each.unplaced)
		}
	}
	// A mark leaves out only what no place takes: a marked answer about Use, whose body is placed, is recorded and
	// asked as any other, so a body the build lints still holds its record of the kind.
	inputs := fixture.inputs()
	placed := purity(use, "false")
	placed.TypesAlone = true
	inputs.answers = []check.ExportedFact{placed}
	hosted := newFactReachPackage(inputs)
	if recorded := hosted.Recorded(fixture.places["Use"]); hosted.unplaced != 0 || len(recorded) != 1 {
		test.Errorf("a marked answer about Use: %d unplaced and %+v recorded, want none and its one question",
			hosted.unplaced, recorded)
	}
}

// An answer about an `init` function is left out by its mark alone (#30dtg3p). The record declares no object for an
// `init`, so no answer about one finds a declaration, and one the capture did not mark, as every answer of a kind that
// declares no hook is (fact_resolver.Kind.TypesAlone, check's TestTheCaptureMarksNoAnswerOfAKindThatDeclaresNoHook), is
// counted unplaced, which has every build lint the package whole. The same answer marked is left out, uncounted.
// Neither is recorded at the body's place or asked, and either way the body is a place, the user of what it names.
func TestAnAnswerAboutAnInitIsLeftOutByItsMarkAlone(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "initialized", "example.com/facts/initialized")
	var initializer *types.Func
	for identifier, object := range fixture.info.Defs {
		if function, isFunction := object.(*types.Func); isFunction && identifier.Name == "init" {
			initializer = function
		}
	}
	if initializer == nil {
		test.Fatal("the fixture declares no init function")
	}
	for _, each := range []struct {
		name       string
		typesAlone bool
		unplaced   int
	}{
		{"unmarked, as every answer of a kind with no hook is", false, 1},
		{"marked as its types' alone", true, 0},
	} {
		inputs := fixture.inputs()
		inputs.answers = []check.ExportedFact{{Analyzer: "init_probe", Object: initializer, Answer: true,
			Encoded: []byte("true"), TypesAlone: each.typesAlone}}
		hosted := newFactReachPackage(inputs)
		recorded := hosted.Recorded(fixture.places["init"])
		if hosted.unplaced != each.unplaced || len(hosted.askings) != 0 || len(recorded) != 0 {
			test.Errorf("an answer about an init function, %s: %d unplaced, %d questions and %+v recorded, want %d "+
				"and none", each.name, hosted.unplaced, len(hosted.askings), recorded, each.unplaced)
		}
		users := hosted.Users(fixture.key(test, fixture.function(test, "count")))
		if want := fixture.placesOf(test, "init"); !slices.Equal(users, want) {
			test.Errorf("an answer about an init function, %s: the users of count are %v, want %v, init's body",
				each.name, users, want)
		}
	}
}

// An answer the host holds about another package's function is a question asked before any body leaves, when a body
// of the package uses the function: one of the function itself, and one of its parameter with the parameter's index,
// still about the function. The questions come in the order of their keys whatever order the answers came in, an
// answer handed twice is one question, and what a build is asked of each is kept by its key beside the recorded ones.
// The host hands every answer it holds, so one about a function no body uses, about that function's parameter, about
// a value, about the package, or about one of the package's own functions, answered among its own or not, is left
// out and counted nowhere.
func TestAnImportedAnswerAboutAUsedFunctionIsAQuestionAboutIt(test *testing.T) {
	test.Parallel()

	fixture, lower := checkedFactsFixture(test)
	size := fixture.function(test, "size")
	depth, halve := lower.function(test, "Depth"), lower.function(test, "Halve")
	apply := lower.function(test, "Scale.Apply")
	oddSlice := func(parameter *types.Var, encoded string) check.ExportedFact {
		return check.ExportedFact{Analyzer: "odd_slice_argument", Object: parameter, Answer: true,
			Encoded: []byte(encoded)}
	}
	inputs := fixture.inputs()
	inputs.answers = []check.ExportedFact{purity(size, "true")}
	inputs.imported = []check.ExportedFact{
		oddSlice(halve.Signature().Params().At(1), `{"odd":true}`),
		purity(apply, "true"),
		oddSlice(apply.Signature().Params().At(0), `{"odd":false}`),
		purity(lower.own.Scope().Lookup("Limit"), "true"),
		purity(nil, "true"),
		purity(size, "false"),
		purity(fixture.function(test, "platformSize"), "true"),
		purity(depth, "true"),
		purity(depth, "true"),
	}
	hosted := newFactReachPackage(inputs)
	if hosted.unplaced != 0 {
		test.Errorf("want nothing unplaced, an imported answer left out being counted nowhere, got %d", hosted.unplaced)
	}
	imported := hosted.Imported()
	renderer := build_reads.NewIdentityRenderer(fixture.fileSet, nil)
	want := []struct {
		kind      string
		function  *types.Func
		parameter int
		encoded   string
	}{
		{"function_purity", depth, -1, "true"},
		{"odd_slice_argument", halve, 1, `{"odd":true}`},
	}
	if len(imported) != len(want) {
		var asked []string
		for _, question := range imported {
			asking, _ := hosted.Asking(question.Key)
			asked = append(asked, fmt.Sprintf("%s of %s, parameter %d", asking.Kind, asking.Function, asking.Parameter))
		}
		test.Fatalf("the imported questions are %q, want Depth's purity and Halve's parameter 1 alone", asked)
	}
	for index, each := range want {
		question := imported[index]
		if question.Kind != each.kind || question.About != fixture.key(test, each.function) {
			test.Errorf("imported question %d is of %s about %q, want %s about %s, key %q", index, question.Kind,
				question.About, each.kind, each.function.Name(), fixture.key(test, each.function))
		}
		asking, isAsked := hosted.Asking(question.Key)
		if !isAsked || asking.Kind != each.kind || !asking.Function.Equal(renderer.Object(each.function)) ||
			asking.Parameter != each.parameter || string(asking.Encoded) != each.encoded {
			test.Errorf("imported question %d asks %+v (%t), want %s of %s, parameter %d, answered %s", index, asking,
				isAsked, each.kind, each.function.Name(), each.parameter, each.encoded)
		}
	}
	// The recorded question answers by its key beside the imported ones, with the answer the package's own answers
	// gave, and neither list holds the other's.
	recorded := hosted.Recorded(fixture.places["size"])
	if len(recorded) != 1 {
		test.Fatalf("size's place holds %+v, want its one question", recorded)
	}
	if asking, isAsked := hosted.Asking(recorded[0].Key); !isAsked || asking.Parameter != -1 ||
		string(asking.Encoded) != "true" {
		test.Errorf("size's recorded question asks %+v (%t), want its purity, answered true", asking, isAsked)
	}
	if slices.Contains(imported, recorded[0]) {
		test.Error("size's recorded question is among the imported ones")
	}
	if len(hosted.askings) != len(want)+1 {
		test.Errorf("%d questions have something to ask, want the %d imported and size's", len(hosted.askings),
			len(want))
	}
}

// The objects bound to the host's order are named by their keys, each for the order, in the order given, and one no
// identity names is counted unplaced, since no build could take its users out.
func TestTheOrderBoundObjectsAreNamedByTheirKeys(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	size, open := fixture.function(test, "size"), fixture.function(test, "Box.Open")
	inputs := fixture.inputs()
	inputs.orderBound = []types.Object{size, open}
	hosted := newFactReachPackage(inputs)
	want := []fact_reach.Named{{Object: fixture.key(test, size), Reason: "order"},
		{Object: fixture.key(test, open), Reason: "order"}}
	if got := hosted.OrderBound(); !slices.Equal(got, want) || hosted.unplaced != 0 {
		test.Errorf("the order-bound objects are %q with %d unplaced, want %q and none", got, hosted.unplaced, want)
	}

	unfollowed := checkedFactReachFixture(test, "unfollowed", "example.com/facts/unfollowed")
	settings, isStruct := unfollowed.own.Scope().Lookup("settings").Type().(*types.Struct)
	if !isStruct {
		test.Fatal("want settings of a struct type with no name")
	}
	inputs = unfollowed.inputs()
	inputs.orderBound = []types.Object{settings.Field(0)}
	hosted = newFactReachPackage(inputs)
	if got := hosted.OrderBound(); len(got) != 0 || hosted.unplaced != 1 {
		test.Errorf("an order-bound field no identity names: %q named and %d unplaced, want none and 1", got,
			hosted.unplaced)
	}
}

// Two keys are equal exactly when their identities are (build_reads.Identity.Equal): two renders of one object give
// one key, a function and a method of one name give two, an unknown identity has no key, and an identity that differs
// in any one field Equal compares has another key, a field added to Equal and left out of the key failing here. The
// parts are joined so that no two splits of the same letters meet.
func TestTwoKeysAreEqualExactlyWhenTheirIdentitiesAre(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
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
	hosted := newFactReachPackage(fixture.inputs())
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

// A build's standing set is handed out as a copy at each call, since its caller takes bodies out of it: taking one
// out of the first leaves the second, and the build's own, whole. A package the build lints whole has none, whether
// the build does not lint it in part or the host's side holds anything unplaced, where a body left standing would
// rest on an answer no build was asked.
func TestABuildsStandingSetIsACopyAndNoneForAPackageLintedWhole(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	standing := fixture.standing("size")
	bodies := len(standing)
	warm := fixture.places["Warm"]
	hosted := newFactReachPackage(fixture.inputs())

	inPart := hosted.inBuild(standing, true)
	first, isPartial := inPart.Standing()
	if !isPartial || !maps.Equal(first, standing) {
		test.Fatalf("a build that lints in part stands on %v (%t), want its %d bodies", first, isPartial, bodies)
	}
	delete(first, warm)
	second, _ := inPart.Standing()
	if !second[warm] || len(second) != bodies || !standing[warm] || len(standing) != bodies {
		test.Errorf("after Warm left one copy, the next holds %d bodies and the build's own %d, want %d in both",
			len(second), len(standing), bodies)
	}

	if whole, isPartial := hosted.inBuild(standing, false).Standing(); isPartial || whole != nil {
		test.Errorf("a build that lints the package whole stands on %v (%t), want nothing", whole, isPartial)
	}

	inputs := fixture.inputs()
	inputs.answers = []check.ExportedFact{purity(nil, "true")}
	unplaced := newFactReachPackage(inputs)
	if whole, isPartial := unplaced.inBuild(standing, true).Standing(); unplaced.unplaced != 1 || isPartial ||
		whole != nil {
		test.Errorf("a package with %d unplaced stands on %v (%t), want 1 unplaced and nothing standing",
			unplaced.unplaced, whole, isPartial)
	}
}

// The fixed point over the host's side in a build (fact_reach.Settle over a fact_reach.Host): size is reached, so its
// recorded purity is asked in the build, and where that differs Warm, its one user, leaves the standing set, with size
// named as what took it out. Warm's own purity reads equal, so nothing above it moves and every other body stands, in
// the settlement's own set, the one the host handed out left as it was.
func TestADifferingAnswerAboutSizeTakesWarmOutAndNothingElse(test *testing.T) {
	test.Parallel()

	fixture, _ := checkedFactsFixture(test)
	size, warm := fixture.function(test, "size"), fixture.function(test, "Warm")
	inputs := fixture.inputs()
	inputs.answers = []check.ExportedFact{purity(size, "true"), purity(warm, "false")}
	host := newFactReachPackage(inputs).inBuild(fixture.standing("size"), true)
	standing, isPartial := host.Standing()
	if !isPartial {
		test.Fatal("want the build to lint the package in part")
	}
	bodies := len(standing)
	var asked []string
	settlement := fact_reach.Settle(standing, fixture.placesOf(test, "size"), nil, nil, host,
		func(question fact_reach.Question) fact_reach.Comparison {
			asking, isAsked := host.Asking(question.Key)
			if !isAsked {
				test.Errorf("the question %q has nothing to ask a build", question.Key)
			}
			asked = append(asked, asking.Kind+" of "+asking.Function.String())
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
	stillStanding := settlement.Standing
	if stillStanding[fixture.places["Warm"]] || len(stillStanding) != bodies-1 {
		test.Errorf("%d of %d bodies stand, Warm among them %t, want every one but Warm: %v", len(stillStanding),
			bodies, stillStanding[fixture.places["Warm"]], slices.Collect(maps.Keys(stillStanding)))
	}
	// What still stands is the settlement's own: the set the host handed out is as it was.
	if !standing[fixture.places["Warm"]] || len(standing) != bodies {
		test.Errorf("the set handed to the fixed point holds %d of its %d bodies after it, want it unwritten",
			len(standing), bodies)
	}
	if settlement.Asked != 2 || len(asked) != 2 || settlement.Differing["function_purity"] != 1 {
		test.Errorf("asked %v with %v differing, want size's purity then Warm's, one differing", asked,
			settlement.Differing)
	}
}

// Two parsed files whose line directives name one file put a body each at one place, a name and an offset, so a place
// cannot say which body it is (#e4rd5dw, must 6): First's and Other's are one. Such a name fails closed: no body
// placed in it stands, whatever the reach left, while a body of the package placed in a name one file alone holds
// would.
func TestABodyPlacedInANameTwoParsedFilesShareNeverStands(test *testing.T) {
	test.Parallel()

	fixture := checkedFactReachFixture(test, "shared", "example.com/facts/shared")
	first, other := fixture.places["First"], fixture.places["Other"]
	if first != other || filepath.Base(first.File) != "shared_as_written.go" {
		test.Fatalf("First is placed at %v and Other at %v, want one place in shared_as_written.go", first, other)
	}
	standing, partial := newFactReachPackage(fixture.inputs()).inBuild(map[fact_reach.Place]bool{first: true},
		true).Standing()
	if !partial || standing[first] {
		test.Errorf("the build lints the package in part %t, with %v standing, want in part and the shared place "+
			"not standing", partial, standing)
	}
}
