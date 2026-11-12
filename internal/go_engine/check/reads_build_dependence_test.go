package check

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// dependencePrefix is the path every package of the dependence fixture is under, by its directory's name.
const dependencePrefix = "example.com/dependence/"

// dependenceFiles are the dependence fixture's packages, each of which but h differs in one build alone: s1 on freebsd,
// where Inner's Value is an int64, x on linux, where Thing's Count is, and g on windows, where Named gains Apple, which
// sorts before every method of an interface that embeds it. s2 holds s1's Inner in Outer, exported, and in Hidden,
// unexported; h returns s2's Outer, x's Thing and an unnamed interface embedding g's Named; m's Sizer embeds e's Base,
// with Size, and o's Other, with nothing; a's Count is an alias of s1's Inner; and hide returns an unnamed struct
// holding s1's Inner unexported. p's interfaces embed q's, one of which embeds r's, each named, taken as a parameter,
// returned in a struct's field, or held in an unexported field's struct, so an objectpath reaches a method of each
// through another package's methods: p's I is the Zed fixture, `interface{ q.J; Zed() }`.
var dependenceFiles = map[string]string{
	"s1/s1_other.go":   "//go:build !freebsd\n\npackage s1\n\ntype Inner struct{ Value int }\n",
	"s1/s1_freebsd.go": "package s1\n\ntype Inner struct{ Value int64 }\n",
	"s2/s2.go": "package s2\n\nimport \"example.com/dependence/s1\"\n\n" +
		"type Outer struct{ Held s1.Inner }\n\ntype Hidden struct{ held s1.Inner }\n",
	"x/x_other.go":   "//go:build !linux\n\npackage x\n\ntype Thing struct{ Count int }\n",
	"x/x_linux.go":   "package x\n\ntype Thing struct{ Count int64 }\n",
	"g/g_other.go":   "//go:build !windows\n\npackage g\n\ntype Named interface{ Name() string }\n",
	"g/g_windows.go": "package g\n\ntype Named interface {\n\tName() string\n\tApple()\n}\n",
	"h/h.go": "package h\n\nimport (\n\t\"example.com/dependence/g\"\n\t\"example.com/dependence/s2\"\n" +
		"\t\"example.com/dependence/x\"\n)\n\n" +
		"func Make() s2.Outer { return s2.Outer{} }\n\n" +
		"func Other() x.Thing { return x.Thing{} }\n\n" +
		"func Count(outer *s2.Outer) int { return 0 }\n\n" +
		"func Get() interface {\n\tg.Named\n\tDone()\n} {\n\treturn nil\n}\n",
	"e/e.go": "package e\n\ntype Base struct{}\n\nfunc (Base) Size() int { return 0 }\n",
	"o/o.go": "package o\n\ntype Other struct{}\n",
	"m/m.go": "package m\n\nimport (\n\t\"example.com/dependence/e\"\n\t\"example.com/dependence/o\"\n)\n\n" +
		"type Sizer struct {\n\te.Base\n\to.Other\n}\n",
	"a/a.go": "package a\n\nimport \"example.com/dependence/s1\"\n\ntype Count = s1.Inner\n",
	"hide/hide.go": "package hide\n\nimport \"example.com/dependence/s1\"\n\n" +
		"func Hide() struct{ inner s1.Inner } { return struct{ inner s1.Inner }{} }\n",
	"r/r.go": "package r\n\ntype L interface{ Alpha() }\n",
	"q/q.go": "package q\n\nimport \"example.com/dependence/r\"\n\n" +
		"type J interface{ Name() string }\n\ntype K interface{ r.L }\n",
	"p/p.go": "package p\n\nimport \"example.com/dependence/q\"\n\n" +
		"type I interface {\n\tq.J\n\tZed()\n}\n\n" +
		"func Take(callback interface {\n\tq.J\n\tRun(value int)\n}) {\n}\n\n" +
		"func Give() struct {\n\tField interface {\n\t\tq.J\n\t\tClose()\n\t}\n} {\n" +
		"\treturn struct {\n\t\tField interface {\n\t\t\tq.J\n\t\t\tClose()\n\t\t}\n\t}{}\n}\n\n" +
		"type Holder struct {\n\tinner struct {\n\t\tDeep interface {\n\t\t\tq.K\n\t\t\tStop()\n\t\t}\n\t}\n}\n",
}

// dependenceOrder is the fixture's packages by directory, each after every one it imports.
var dependenceOrder = []string{"s1", "s2", "x", "g", "h", "e", "o", "m", "a", "hide", "r", "q", "p"}

// dependenceUsers are the user packages, each of which reads the fixture one way, by name.
var dependenceUsers = map[string]string{
	// The Square's fixture: user imports h alone and copies the s2 value h's Make returns.
	"copied": "import \"example.com/dependence/h\"\n\n" +
		"func Copy() any {\n\tvalue := h.Make()\n\tcopied := value\n\treturn copied\n}\n",
	"member": "import \"example.com/dependence/h\"\n\nfunc Use() int { return h.Count(nil) }\n",
	"methodSet": "import \"example.com/dependence/m\"\n\n" +
		"func Hand(sizer *m.Sizer) interface{ Size() int } { return sizer }\n",
	"selection": "import \"example.com/dependence/m\"\n\nfunc Call(sizer *m.Sizer) int { return sizer.Size() }\n",
	"alias":     "import \"example.com/dependence/a\"\n\nfunc Point(count *a.Count) *a.Count { return count }\n",
	// A call whose result is dropped, so nothing of the struct it returns is used but by Hide's own line.
	"observedMember":  "import \"example.com/dependence/hide\"\n\nfunc Use() { hide.Hide() }\n",
	"observedNode":    "import \"example.com/dependence/s2\"\n\nfunc Copy(hidden *s2.Hidden) any { return *hidden }\n",
	"interfaceMethod": "import \"example.com/dependence/h\"\n\nfunc Use() { h.Get().Done() }\n",
	"zed":             "import \"example.com/dependence/p\"\n\nfunc Use(value p.I) { value.Zed() }\n",
	"linknamed":       "import _ \"unsafe\"\n\n//go:linkname now runtime.nanotime\nfunc now() int64\n",
	// A user that imports o and reads nothing of it, which a read added to it is all the set holds of but the import.
	"blank": "import _ \"example.com/dependence/o\"\n",
}

// dependencePath is the path of the fixture's package in directory.
func dependencePath(directory string) string {
	return dependencePrefix + directory
}

// dependenceWorld is one build of the dependence fixture: its packages by path, and a memo of them, measured by
// persistedSizes as a build's memo is.
type dependenceWorld struct {
	packages map[string]*types.Package
	memo     *ReadsBuild
}

// dependenceBuild type-checks the fixture for goos, each package from the files goos's build context matches.
func dependenceBuild(test *testing.T, goos string) dependenceWorld {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, dependenceFiles)
	context := build.Default
	context.GOOS, context.GOARCH, context.CgoEnabled = goos, "amd64", false
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if found, isFound := checked[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	for _, name := range dependenceOrder {
		packageDirectory := filepath.Join(directory, name)
		entries, readError := os.ReadDir(packageDirectory)
		if readError != nil {
			test.Fatal(readError)
		}
		var files []*ast.File
		for _, entry := range entries {
			matched, matchError := context.MatchFile(packageDirectory, entry.Name())
			if matchError != nil {
				test.Fatal(matchError)
			}
			if !matched {
				continue
			}
			parsed, parseError := parser.ParseFile(fileSet, filepath.Join(packageDirectory, entry.Name()), nil,
				parser.SkipObjectResolution)
			if parseError != nil {
				test.Fatal(parseError)
			}
			files = append(files, parsed)
		}
		checkedPackage, checkError := configuration.Check(dependencePath(name), fileSet, files, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s for %s: %v", name, goos, checkError)
		}
		checked[dependencePath(name)] = checkedPackage
	}
	return dependenceWorld{packages: checked, memo: measuredReadsBuild()}
}

// dependenceRestoring is the user package named user checked over host, the darwin build, its reads recorded from its
// type information and syntax, as the run records them.
func dependenceRestoring(test *testing.T, host dependenceWorld, user string) Restoring {
	test.Helper()

	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "user.go", "package user\n\n"+dependenceUsers[user],
		parser.SkipObjectResolution|parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		if found, isFound := host.packages[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	checked, checkError := configuration.Check(dependencePath("user"), fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking the %s user on the host: %v", user, checkError)
	}
	reads := &build_reads.Reads{}
	reads.RecordTypes(checked, info, []*ast.File{file})
	imports := make([]string, 0, len(checked.Imports()))
	for _, imported := range checked.Imports() {
		imports = append(imports, imported.Path())
	}
	return Restoring{Path: checked.Path(), Imports: imports, Reads: reads}
}

// dependenceNames are set's packages by their directory's name, sorted, or "whole" for the whole import closure.
func dependenceNames(set DependenceSet) []string {
	if set.WholeClosure {
		return []string{"whole"}
	}
	names := make([]string, 0, len(set.Paths)+1)
	for path := range set.Paths {
		names = append(names, strings.TrimPrefix(path, dependencePrefix))
	}
	slices.Sort(names)
	unfound := make([]string, 0, len(set.UnfoundPaths))
	for path := range set.UnfoundPaths {
		unfound = append(unfound, "unfound "+strings.TrimPrefix(path, dependencePrefix))
	}
	slices.Sort(unfound)
	names = append(names, unfound...)
	own := make([]string, 0, len(set.OwnFactPaths))
	for path := range set.OwnFactPaths {
		own = append(own, "own "+strings.TrimPrefix(path, dependencePrefix))
	}
	slices.Sort(own)
	names = append(names, own...)
	if set.ClosureMembership {
		names = append(names, "membership")
	}
	return names
}

// The Square's fixture (#154dr43): user imports h alone, and copies the s2 value h's Make returns, whose Outer holds
// s1's Inner. The copy is a use of Outer whole, so the set holds s1 and s2, which user never imports, beside h, whose
// Make it calls, and nothing of h's x or g, which no read of user reaches. A set of the direct imports alone would
// miss both, and the walk would restore user over an s1 that changed.
func TestADependenceSetHoldsWhatAWholeUseReachesPastTheDirectImports(test *testing.T) {
	test.Parallel()

	host := dependenceBuild(test, "darwin")
	restoring := dependenceRestoring(test, host, "copied")
	if !slices.Equal(restoring.Imports, []string{dependencePath("h")}) {
		test.Fatalf("user imports %v, want h alone", restoring.Imports)
	}
	if got, want := dependenceNames(host.memo.Dependence(restoring)), []string{"h", "s1", "s2"}; !slices.Equal(got,
		want) {
		test.Errorf("the set is %v, want %v", got, want)
	}
}

// Each kind of read reaches what its verdict reads, and no more: a member its owner alone, though its signature names
// s2's Outer, and in a package with assembly files, whose every read is judged by its closure, all that Outer reaches;
// a method set and a selection every package of a type embedded in the receiver, o's Other too, which declares nothing
// of the name but could shadow it, or make it ambiguous, in another build; an alias the type it ends at; a member's own
// line the type of an unexported field it renders by its properties, and a node's the same; an interface's method the
// package of the interface it embeds, whose methods decide its objectpath; a fact read its owner, and an object's fact
// its object's counterpart; a Scope lookup its path, its answer's closure and the import closure's membership, a
// Closure lookup the membership and its found answer's closure, or its path as unfound, a PackageName lookup the
// membership alone; a Deprecated lookup its path's own facts. A fact read and a lookup are each added to a user that
// imports o and reads nothing, so the set is what that read adds.
func TestEachKindOfReadReachesWhatItsVerdictReads(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		name     string
		user     string
		assembly bool
		read     func(test *testing.T, host dependenceWorld, reads *build_reads.Reads)
		want     []string
	}{
		{name: "a member", user: "member", want: []string{"h"}},
		{name: "a member of a package with assembly files", user: "member", assembly: true,
			want: []string{"h", "s1", "s2"}},
		{name: "a method set through embedded types", user: "methodSet", want: []string{"e", "m", "o"}},
		{name: "a selection through embedded types", user: "selection", want: []string{"e", "m", "o"}},
		{name: "an alias's name", user: "alias", want: []string{"a", "s1"}},
		{name: "a member's observed field", user: "observedMember", want: []string{"hide", "s1"}},
		{name: "a node's observed field", user: "observedNode", want: []string{"s1", "s2"}},
		{name: "an interface's method by its objectpath", user: "interfaceMethod", want: []string{"g", "h"}},
		{
			name: "a package's fact",
			user: "blank",
			read: func(_ *testing.T, host dependenceWorld, reads *build_reads.Reads) {
				reads.AddFact(build_reads.Fact{Package: host.packages[dependencePath("s1")],
					FactType: reflect.TypeFor[*valueFact]()})
			},
			want: []string{"s1"},
		},
		{
			name: "an object's fact",
			user: "blank",
			read: func(test *testing.T, host dependenceWorld, reads *build_reads.Reads) {
				reads.AddFact(build_reads.Fact{Object: pathObjects(test, host)["h.Done"],
					Package: host.packages[dependencePath("h")], FactType: reflect.TypeFor[*valueFact]()})
			},
			want: []string{"g", "h"},
		},
		{name: "the Zed fixture's selection of Zed", user: "zed", want: []string{"p", "q"}},
		{
			name: "the Zed fixture's Zed by its objectpath alone",
			user: "blank",
			read: func(test *testing.T, host dependenceWorld, reads *build_reads.Reads) {
				reads.AddFact(build_reads.Fact{Object: pathObjects(test, host)["p.Zed"],
					Package: host.packages[dependencePath("p")], FactType: reflect.TypeFor[*valueFact]()})
			},
			want: []string{"p", "q"},
		},
		{
			name: "a Scope lookup",
			user: "blank",
			read: func(_ *testing.T, host dependenceWorld, reads *build_reads.Reads) {
				reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryScope, Path: dependencePath("s2"),
					Name: "Outer", Found: true, Answer: host.packages[dependencePath("s2")].Scope().Lookup("Outer")})
			},
			want: []string{"s1", "s2", "membership"},
		},
		{
			name: "a Scope miss",
			user: "blank",
			read: func(_ *testing.T, _ dependenceWorld, reads *build_reads.Reads) {
				reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryScope, Path: dependencePath("x"),
					Name: "Missing"})
			},
			want: []string{"x", "membership"},
		},
		{
			name: "a Deprecated lookup",
			user: "blank",
			read: func(_ *testing.T, _ dependenceWorld, reads *build_reads.Reads) {
				reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: dependencePath("g")})
			},
			want: []string{"own g"},
		},
		{
			name: "a Closure lookup",
			user: "blank",
			read: func(_ *testing.T, host dependenceWorld, reads *build_reads.Reads) {
				reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryClosure, Path: dependencePath("s2"),
					Name: "Outer", Found: true, Answer: host.packages[dependencePath("s2")].Scope().Lookup("Outer")})
			},
			want: []string{"s1", "s2", "membership"},
		},
		{
			name: "a Closure miss",
			user: "blank",
			read: func(_ *testing.T, _ dependenceWorld, reads *build_reads.Reads) {
				reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryClosure, Path: dependencePath("x"),
					Name: "Missing"})
			},
			want: []string{"unfound x", "membership"},
		},
		{
			name: "a PackageName lookup",
			user: "blank",
			read: func(_ *testing.T, _ dependenceWorld, reads *build_reads.Reads) {
				reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryPackageName, Name: "s1", Found: true})
			},
			want: []string{"membership"},
		},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			host := dependenceBuild(test, "darwin")
			restoring := dependenceRestoring(test, host, step.user)
			restoring.Assembly = step.assembly
			if step.read != nil {
				step.read(test, host, restoring.Reads)
			}
			if got := dependenceNames(host.memo.Dependence(restoring)); !slices.Equal(got, step.want) {
				test.Errorf("the set is %v, want %v", got, step.want)
			}
		})
	}
}

// pathObjects are the fixture's objects whose objectpaths pass through an interface's method set, or through a struct's
// fields, by package and name: h's Done, of the interface Get returns; p's Zed, of I; Run, of the interface Take's
// parameter is, and its parameter value; Field, of the struct Give returns, and Close, of Field's interface; inner, a
// field of Holder, Deep, a field of inner's struct, and Stop, of Deep's interface, which embeds q's K, which embeds r's
// L.
func pathObjects(test *testing.T, host dependenceWorld) map[string]types.Object {
	test.Helper()

	get, take, give := host.packages[dependencePath("h")].Scope().Lookup("Get"),
		host.packages[dependencePath("p")].Scope().Lookup(
			"Take",
		), host.packages[dependencePath("p")].Scope().Lookup("Give")
	run := explicitMethod(test, signatureOf(test, take).Params().At(0).Type(), "Run")
	field := structField(test, signatureOf(test, give).Results().At(0).Type(), "Field")
	inner := structField(test, host.packages[dependencePath("p")].Scope().Lookup("Holder").Type(), "inner")
	deep := structField(test, inner.Type(), "Deep")
	return map[string]types.Object{
		"h.Done":  explicitMethod(test, signatureOf(test, get).Results().At(0).Type(), "Done"),
		"p.Zed":   explicitMethod(test, host.packages[dependencePath("p")].Scope().Lookup("I").Type(), "Zed"),
		"p.Run":   run,
		"p.value": run.Signature().Params().At(0),
		"p.Field": field,
		"p.Close": explicitMethod(test, field.Type(), "Close"),
		"p.inner": inner,
		"p.Deep":  deep,
		"p.Stop":  explicitMethod(test, deep.Type(), "Stop"),
	}
}

// signatureOf is function's signature.
func signatureOf(test *testing.T, function types.Object) *types.Signature {
	test.Helper()

	signature, isSignature := function.Type().(*types.Signature)
	if !isSignature {
		test.Fatalf("%s is no function", function.Name())
	}
	return signature
}

// explicitMethod is the method named name that walked, an interface, declares itself.
func explicitMethod(test *testing.T, walked types.Type, name string) *types.Func {
	test.Helper()

	declaring, isInterface := walked.Underlying().(*types.Interface)
	if !isInterface {
		test.Fatalf("%s is no interface", walked)
	}
	for method := range declaring.ExplicitMethods() {
		if method.Name() == name {
			return method
		}
	}
	test.Fatalf("%s declares no method %s", walked, name)
	return nil
}

// structField is the field named name of walked, a struct.
func structField(test *testing.T, walked types.Type, name string) *types.Var {
	test.Helper()

	structure, isStruct := walked.Underlying().(*types.Struct)
	if !isStruct {
		test.Fatalf("%s is no struct", walked)
	}
	for field := range structure.Fields() {
		if field.Name() == name {
			return field
		}
	}
	test.Fatalf("%s has no field %s", walked, name)
	return nil
}

// Reads the set cannot follow one by one are the whole import closure: persisted reads, reads never recorded or marked
// never, reads judged by closure alone, and a lookup that reads every type name of the import closure with a method of
// its name, Interface or Method, or one of a kind the set does not know.
func TestReadsTheSetCannotFollowAreTheWholeClosure(test *testing.T) {
	test.Parallel()

	lookupOf := func(lookup build_reads.Lookup) func(test *testing.T, host dependenceWorld) Restoring {
		return func(test *testing.T, host dependenceWorld) Restoring {
			restoring := dependenceRestoring(test, host, "member")
			restoring.Reads.AddLookup(lookup)
			return restoring
		}
	}
	for _, step := range []struct {
		name      string
		restoring func(test *testing.T, host dependenceWorld) Restoring
	}{
		{"persisted reads", func(test *testing.T, host dependenceWorld) Restoring {
			return persistedRestoring(test, dependenceRestoring(test, host, "member"), host.memo, ReadsRendererVersion)
		}},
		{"reads never recorded", func(*testing.T, dependenceWorld) Restoring {
			return Restoring{Path: dependencePath("user"), Reads: &build_reads.Reads{}}
		}},
		{"reads marked never", func(test *testing.T, host dependenceWorld) Restoring {
			restoring := dependenceRestoring(test, host, "member")
			restoring.Reads.MarkNever("a finding names another package's file")
			return restoring
		}},
		{"reads judged by closure alone", func(test *testing.T, host dependenceWorld) Restoring {
			return dependenceRestoring(test, host, "linknamed")
		}},
		{"an Interface lookup", lookupOf(build_reads.Lookup{Query: build_reads.QueryInterface, Name: "Size"})},
		{"a Method lookup", lookupOf(build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Size"})},
		{"a lookup of a kind the set does not know", lookupOf(build_reads.Lookup{Query: "Planted", Name: "Anything"})},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			host := dependenceBuild(test, "darwin")
			if got := dependenceNames(host.memo.Dependence(step.restoring(test, host))); !slices.Equal(got,
				[]string{"whole"}) {
				test.Errorf("the set is %v, want the whole closure", got)
			}
		})
	}
}

// An answer an entry read reaches its owner, found by ID as the answers' check finds it, by path, so a test variant's
// answer reaches the package it varies. An entry whose reads could not all be named, or that names an owner the run
// does not hold, is the whole import closure, since its check never holds.
func TestAnAnswerReadReachesItsOwner(test *testing.T) {
	test.Parallel()

	variant := dependencePath("s1") + " [" + dependencePath("s1") + ".test]"
	byIdentifier := map[string]*packages.Package{
		dependencePath("s2"): {ID: dependencePath("s2"), PkgPath: dependencePath("s2")},
		variant:              {ID: variant, PkgPath: dependencePath("s1")},
	}
	for _, step := range []struct {
		name     string
		restored *restoredPackage
		want     []string
	}{
		{"answers of two owners", &restoredPackage{reads: []storedRead{{Owner: dependencePath("s2")},
			{Owner: variant}}}, []string{"s1", "s2"}},
		{"reads not all named", &restoredPackage{reads: []storedRead{{Owner: dependencePath("s2")}},
			readsIncomplete: true}, []string{"whole"}},
		{"an owner the run does not hold", &restoredPackage{reads: []storedRead{{Owner: dependencePath("x")}}},
			[]string{"whole"}},
	} {
		if got := dependenceNames(answersDependence(step.restored, byIdentifier)); !slices.Equal(got, step.want) {
			test.Errorf("%s: the set is %v, want %v", step.name, got, step.want)
		}
	}
}

// A package's set is computed once, on the host's memo, and every later ask, from any goroutine, gets the same set.
func TestADependenceSetIsComputedOncePerPackage(test *testing.T) {
	test.Parallel()

	host := dependenceBuild(test, "darwin")
	restoring := dependenceRestoring(test, host, "copied")
	first := host.memo.Dependence(restoring)
	sets := make(chan DependenceSet, 4)
	for range cap(sets) {
		go func() { sets <- host.memo.Dependence(restoring) }()
	}
	for range cap(sets) {
		if again := <-sets; reflect.ValueOf(again.Paths).UnsafePointer() !=
			reflect.ValueOf(first.Paths).UnsafePointer() {
			test.Fatalf("an ask computed the set again: %v", dependenceNames(again))
		}
	}
}

// A set reads the reach of the types another set walked first from the memo (typeReach): copied's set walks s2's
// Outer and s1's Inner, and observedNode's, asked of the same memo after it, reaches Inner through Hidden's observed
// field, so its set holds s1 only through what the memo keeps of Inner.
func TestASetReadsTheReachOfTypesAnotherSetWalkedFirst(test *testing.T) {
	test.Parallel()

	host := dependenceBuild(test, "darwin")
	if got := dependenceNames(host.memo.Dependence(dependenceRestoring(test, host, "copied"))); !slices.Equal(got,
		[]string{"h", "s1", "s2"}) {
		test.Fatalf("copied's set is %v, want h, s1 and s2", got)
	}
	if got := dependenceNames(host.memo.Dependence(dependenceRestoring(test, host, "observedNode"))); !slices.Equal(got,
		[]string{"s1", "s2"}) {
		test.Errorf("observedNode's set, asked after copied's, is %v, want s1 and s2", got)
	}
}

// The set is sound by the verdict itself: a build that changes a package outside user's set, x on linux, keeps
// TypesHold holding on user, and one that changes a package inside it, s1 on freebsd, fails it. The interface method's
// set holds g, whose Named gains a method on windows that moves Done's objectpath, and the verdict fails there with
// nothing else of the fixture changed: the counterpart reads g, which its owner h does not declare.
func TestAChangeOutsideTheSetKeepsTheVerdictAndOneInsideFailsIt(test *testing.T) {
	test.Parallel()

	host := dependenceBuild(test, "darwin")
	for _, step := range []struct {
		user    string
		goos    string
		changed string
		want    string
	}{
		{"copied", "linux", "x", ""},
		{"copied", "freebsd", "s1", "T object " + dependencePath("s2") + ".Outer renders differently"},
		{"interfaceMethod", "linux", "x", ""},
		{"interfaceMethod", "windows", "g", "T object " + dependencePath("h") + ".Done: the build's " +
			dependencePath("h") + " has no Get.RA0.M0"},
	} {
		restoring := dependenceRestoring(test, host, step.user)
		set := host.memo.Dependence(restoring)
		built := dependenceBuild(test, step.goos)
		changed := dependencePath(step.changed)
		if slices.Equal(exportRendering(host.packages[changed]), exportRendering(built.packages[changed])) {
			test.Fatalf("%s renders alike on %s, so the build changes nothing", step.changed, step.goos)
		}
		if set.Paths[changed] != (step.want != "") {
			test.Errorf("%s's set %v holds %s %t, want %t", step.user, dependenceNames(set), step.changed,
				set.Paths[changed], step.want != "")
		}
		for path, built := range built.packages {
			if !set.Paths[path] || path == changed {
				continue
			}
			if !slices.Equal(exportRendering(host.packages[path]), exportRendering(built)) {
				test.Fatalf("%s, in %s's set, renders apart on %s too", path, step.user, step.goos)
			}
		}
		holds, why := TypesHold(restoring, maps.Clone(built.packages), nil, host.memo, built.memo)
		if holds != (step.want == "") || why != step.want {
			test.Errorf("%s on %s: want %t, %q, got %t, %q", step.user, step.goos, step.want == "", step.want, holds,
				why)
		}
	}
}

// The objectpath mirror (objectPathInterfaces) is the tripwire for a change to golang.org/x/tools' objectpath: for
// every object a fixture user's reads record, and the objects pathObjects builds through interfaces' methods, their
// parameters and results and unnamed structs' fields, the mirror lands on the object objectpath.Object finds at the
// host's path, and the counterpart's reach holds the package of every method of each interface it indexed, and of
// every interface that one embeds at any depth. Each object pathObjects builds reaches exactly the packages named.
func TestTheObjectPathMirrorLandsWhereObjectpathDoes(test *testing.T) {
	test.Parallel()

	host := dependenceBuild(test, "darwin")
	built := map[string][]string{
		"h.Done": {"g", "h"}, "p.Zed": {"p", "q"}, "p.Run": {"p", "q"}, "p.value": {"p", "q"},
		"p.Field": {"p"}, "p.Close": {"p", "q"}, "p.inner": {"p"}, "p.Deep": {"p"}, "p.Stop": {"p", "q", "r"},
	}
	objects := pathObjects(test, host)
	for user := range dependenceUsers {
		reads := dependenceRestoring(test, host, user).Reads
		for _, use := range reads.Uses() {
			objects[user+" use "+objectName(use.Object)] = use.Object
		}
		for _, selection := range reads.Selections() {
			objects[user+" receiver "+objectName(selection.Receiver)] = selection.Receiver
			objects[user+" selected "+objectName(selection.Selected)] = selection.Selected
		}
	}
	for _, name := range slices.Sorted(maps.Keys(objects)) {
		object := objects[name]
		if object.Pkg() == nil || object.Pkg() == types.Unsafe {
			continue
		}
		path, hasPath := host.memo.pathOf(object)
		if !hasPath {
			if built[name] != nil {
				test.Fatalf("%s has no objectpath", name)
			}
			continue
		}
		want, objectError := objectpath.Object(object.Pkg(), path)
		landed, indexed := objectPathInterfaces(object.Pkg(), path)
		if objectError != nil || landed != want {
			test.Errorf("%s at %s: the mirror lands on %v, objectpath on %v, %v", name, path, landed, want, objectError)
		}
		reach := &dependenceReach{memo: host.memo, paths: map[string]bool{}}
		reach.counterpart(object)
		for _, each := range indexed {
			for embedded := range interfacePackages(each) {
				if !reach.paths[embedded] {
					test.Errorf("%s at %s: the reach %v misses %s, of the interface it indexes", name, path,
						dependenceNames(DependenceSet{Paths: reach.paths}), embedded)
				}
			}
		}
		if want, isBuilt := built[name]; isBuilt {
			if got := dependenceNames(DependenceSet{Paths: reach.paths}); !slices.Equal(got, want) {
				test.Errorf("%s at %s: the reach is %v, want %v", name, path, got, want)
			}
		}
	}
}

// interfacePackages are the paths of the packages of every method of indexed's method set, and of every named type or
// alias it embeds at any depth, gathered from go/types' own method set and embedded types, apart from the reach.
func interfacePackages(indexed *types.Interface) map[string]bool {
	paths := map[string]bool{}
	for method := range indexed.Methods() {
		if method.Pkg() != nil {
			paths[method.Pkg().Path()] = true
		}
	}
	var walk func(embedded types.Type)
	walk = func(embedded types.Type) {
		switch declared := embedded.(type) {
		case *types.Alias:
			paths[declared.Obj().Pkg().Path()] = true
			walk(declared.Rhs())
		case *types.Named:
			paths[declared.Obj().Pkg().Path()] = true
			walk(declared.Underlying())
		case *types.Interface:
			for inner := range declared.EmbeddedTypes() {
				walk(inner)
			}
		}
	}
	walk(indexed)
	return paths
}
