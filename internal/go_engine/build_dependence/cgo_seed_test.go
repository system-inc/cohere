package build_dependence

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
)

// The cgoseed fixture's packages: native, which has the cgo file, and user, which has none.
const (
	cgoSeedNative = "example.com/cgoseed/native"
	cgoSeedUser   = "example.com/cgoseed/user"
)

// cgoEnvironment is buildEnvironment's for the platform the test runs on with cgo on, since the cgo tool runs the
// machine's own C compiler and compiles for no other platform.
func cgoEnvironment(tags string) []string {
	environment := slices.DeleteFunc(buildEnvironment(runtime.GOOS, tags), func(pair string) bool {
		return strings.HasPrefix(pair, "CGO_ENABLED=")
	})
	return append(environment, "CGO_ENABLED=1")
}

// The cgoseed fixture's two builds, cgo on in both and nothing but the tag `wide` between them.
var (
	cgoHostBuild = fixtureBuild{name: "cgo on", environment: cgoEnvironment("")}
	cgoWideBuild = fixtureBuild{name: "cgo on tags wide", environment: cgoEnvironment("wide")}
)

// requireCCompiler skips test where the machine has no C compiler for the cgo tool to run, saying how to run it, and
// where go cannot translate cgo for any other reason (cgo_testing.RequireTranslation). Every other fixture of the
// package loads with cgo off, so it loads the same on every machine (buildEnvironment); the cgoseed fixture is about
// what the cgo tool writes, and loads only where it runs.
func requireCCompiler(test *testing.T) {
	test.Helper()
	named, goEnvironmentError := exec.Command("go", "env", "CC").Output()
	if goEnvironmentError != nil {
		test.Fatalf("reading CC with go env: %v", goEnvironmentError)
	}
	compiler := strings.Fields(string(named))
	found := len(compiler) > 0
	if found {
		_, lookPathError := exec.LookPath(compiler[0])
		found = lookPathError == nil
	}
	if !found {
		test.Skipf("no C compiler is found here (go env CC names %q), and this test loads a cgo package with cgo on, "+
			"which runs one: put a C compiler on the path, or name one in CC, and run `go run ./command/cohere-dev "+
			"test ./internal/go_engine/build_dependence -run %s`", strings.TrimSpace(string(named)), test.Name())
	}
	cgo_testing.RequireTranslation(test)
}

// expectEncodedAgain fails when again, the record decoded from encoded and encoded once more, is not encoded byte for
// byte, saying where the first byte differs, so two encodings of one length still say where they part.
func expectEncodedAgain(test *testing.T, label string, again []byte, encoded []byte) {
	test.Helper()
	if string(again) == string(encoded) {
		return
	}
	at := 0
	for at < min(len(again), len(encoded)) && again[at] == encoded[at] {
		at++
	}
	test.Errorf("%s decoded encodes to %d bytes, not its own %d, the first differing at byte %d", label, len(again),
		len(encoded), at)
}

// unitVerdictOf is dependence's verdict on the unit of loaded named name (unitName), where a reader sees it.
func unitVerdictOf(test *testing.T, loaded *packages.Package, dependence *Dependence, name string) bool {
	test.Helper()
	for _, file := range loaded.Syntax {
		tokenFile := loaded.Fset.File(file.FileStart)
		for _, unit := range unitDeclarations(file) {
			if unitName(unit) == name {
				return dependence.UnitDependent(loaded.ID, tokenFile.Position(unit.Pos()).Filename,
					tokenFile.Offset(unit.Pos()))
			}
		}
	}
	test.Fatalf("%s: no such unit in %s", name, loaded.ID)
	return false
}

// In the cgoseed fixture every Go file is byte for byte the same on the host and under the tag `wide`, cgo on in both,
// and only the C side moves: `#cgo wide CFLAGS: -DWIDE` makes the macro `SLOTS` 8 where the host's is 4, and struct
// shape two long longs where the host's is one int. The build runs the cgo tool again, under its own tags, so what the
// tool writes is the build's own: native's Slots, computed from `C.SLOTS`, is another constant, and Shape, whose field
// is a `C.struct_shape`, has another layout, with no file as written differing. Every unit of native that is in none of
// its plain Go files is therefore a seed, the cgo file's own, Slots and Shape, and those of the file the tool
// generates, `_Ciconst_SLOTS` and `_Ctype_struct_shape`, which no file list names. So Slots' bit and Shape's underlying
// bit are set, user's Count, which reads Slots, and Size, which reads Shape's layout, are dependent, and Control, which
// reads Steady, a constant of native's plain file, is not, nor is Steady. The two loads, each whole, agree: no unit the
// judgment calls independent records differently in them (checkUnits), and Count and Size really do. The graph read
// back from its records resolves and judges alike, the mark on each unit being the record's own.
func TestACgoPackagesUnitsAreSeedsWhereTheBuildKeepsCgoOn(test *testing.T) {
	test.Parallel()
	requireCCompiler(test)

	host, built := loadFixture(test, "cgoseed", cgoHostBuild), loadFixture(test, "cgoseed", cgoWideBuild)
	hostNative, builtNative := host[cgoSeedNative], built[cgoSeedNative]
	if hostNative == nil || builtNative == nil || host[cgoSeedUser] == nil {
		test.Fatalf("want %s in both builds and %s in the host, got %v, %v and %v", cgoSeedNative, cgoSeedUser,
			hostNative, builtNative, host[cgoSeedUser])
	}
	// The fixture proves nothing unless both builds translate native, share every file of it as written, and still
	// give Slots another value and Shape another size.
	if len(CgoFiles(hostNative)) == 0 || len(CgoFiles(builtNative)) == 0 {
		test.Fatalf("want native translated by the cgo tool in both builds, got the compiled files %q and %q",
			hostNative.CompiledGoFiles, builtNative.CompiledGoFiles)
	}
	if !slices.Equal(hostNative.GoFiles, builtNative.GoFiles) {
		test.Fatalf("want native's files as written the same in both builds, got %q and %q", hostNative.GoFiles,
			builtNative.GoFiles)
	}
	moved := func(loaded *packages.Package) string {
		slots, isConstant := loaded.Types.Scope().Lookup("Slots").(*types.Const)
		shape := loaded.Types.Scope().Lookup("Shape")
		if !isConstant || shape == nil {
			test.Fatalf("want %s to declare the constant Slots and the type Shape", loaded.ID)
		}
		return fmt.Sprintf("Slots %s, Shape %d bytes", slots.Val().ExactString(),
			loaded.TypesSizes.Sizeof(shape.Type()))
	}
	if hostMoved, builtMoved := moved(hostNative), moved(builtNative); hostMoved != "Slots 4, Shape 4 bytes" ||
		builtMoved != "Slots 8, Shape 16 bytes" {
		test.Fatalf("want the C side to move with the tag, got the host's %s and the build's %s", hostMoved, builtMoved)
	}

	difference := differenceOf(host, built)
	if differing := difference.Packages[cgoSeedNative]; len(differing.HostOnlyFiles) > 0 ||
		len(differing.BuildOnlyFiles) > 0 {
		test.Fatalf("want no file of native only one build compiles, got %d host-only and %d build-only",
			len(differing.HostOnlyFiles), len(differing.BuildOnlyFiles))
	}
	live := graphOf(host, nil)
	expectNoUnnameable(test, "cgoseed", live)
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "cgoseed resolved", resolvedLines(live), resolvedLines(recorded))
	encoded, _ := live.EncodedRecord(cgoSeedNative)
	if decoded, decodeError := decodeRecord(encoded); decodeError != nil {
		test.Errorf("native's record: %v", decodeError)
	} else {
		expectEncodedAgain(test, "native's record", encodeRecord(decoded), encoded)
	}
	dependence, recordedDependence := live.Against(difference), recorded.Against(difference)
	expectAlike(test, "cgoseed under the tag", unitVerdicts(dependence), unitVerdicts(recordedDependence))

	for _, graph := range []struct {
		name       string
		dependence *Dependence
	}{
		{"the live graph", dependence},
		{"the graph read back from its records", recordedDependence},
	} {
		for _, name := range []struct {
			local      string
			name       bool
			underlying bool
		}{
			{local: "Slots", name: true},
			{local: "Shape", underlying: true},
			{local: "_Ciconst_SLOTS", name: true},
			{local: "_Ctype_struct_shape", underlying: true},
			{local: "Steady"},
		} {
			bits, isDeclared := graph.dependence.NameBits(cgoSeedNative, name.local)
			if !isDeclared {
				test.Errorf("%s: no unit of native declares it in %s", name.local, graph.name)
				continue
			}
			switch {
			case name.name && !bits.Name:
				test.Errorf("%s: want its name bit set under the tag in %s, got %+v", name.local, graph.name, bits)
			case name.underlying && !bits.Underlying:
				test.Errorf("%s: want its underlying bit set under the tag in %s, got %+v", name.local, graph.name,
					bits)
			case !name.name && !name.underlying && bits != (Bits{}):
				test.Errorf("%s: want neither bit set under the tag in %s, as no build changes it, got %+v",
					name.local, graph.name, bits)
			}
		}
	}

	expectVerdicts(test, verdicts(test, loadedBuild{cgoSeedUser: host[cgoSeedUser]}, dependence), map[string]bool{
		"func Count":   true,
		"func Size":    true,
		"func Control": false,
	})
	for name, want := range map[string]bool{
		"value Slots":              true,
		"type Shape":               true,
		"value _Ciconst_SLOTS":     true,
		"type _Ctype_struct_shape": true,
		"value Steady":             false,
	} {
		if dependent := unitVerdictOf(test, hostNative, dependence, name); dependent != want {
			test.Errorf("%s: want dependent %t, got %t", name, want, dependent)
		}
	}

	checked := checkUnits(host, built, dependence)
	for _, miss := range checked.misses {
		test.Errorf("independent and records differently: %s", miss)
	}
	if checked.dependentDiffer < 2 {
		test.Errorf("want Count and Size dependent and recording differently, got %d such units, so the comparison "+
			"does not see the C side move", checked.dependentDiffer)
	}
}

// checkedAsWritten is the package at path of files, each a name and its source, parsed and checked here with package
// "C" faked, as the engine's load checks a package's cgo files where its cgo tool translates nothing: every file is one
// of the package's files as written and one it compiles. No loader and no C compiler runs.
func checkedAsWritten(test *testing.T, path string, files [][2]string) *packages.Package {
	test.Helper()
	fileSet := token.NewFileSet()
	var names []string
	var syntax []*ast.File
	for _, file := range files {
		parsed, parseError := parser.ParseFile(fileSet, file[0], file[1], 0)
		if parseError != nil {
			test.Fatal(parseError)
		}
		names = append(names, file[0])
		syntax = append(syntax, parsed)
	}
	information := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Instances:  map[*ast.Ident]types.Instance{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	checked, checkError := (&types.Config{FakeImportC: true}).Check(path, fileSet, syntax, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return &packages.Package{ID: path, PkgPath: path, GoFiles: names, CompiledGoFiles: names, Fset: fileSet,
		Syntax: syntax, Types: checked, TypesInfo: information}
}

// withhold empties, in loaded's syntax, the body of each function and the initial values of each package-level var
// that names holds, as the engine's load does of every declaration that reaches cgo in a package it checks as written
// (load.withholdCgoDeclarations), once the package is type-checked and before the graph collects it, and returns each
// one's position as the load gives it, for AddPackage.
//
// Mutates: loaded the named functions lose their bodies and the named vars their initial values
func withhold(test *testing.T, loaded *packages.Package, names ...string) []string {
	test.Helper()
	var positions []string
	withheld := 0
	for _, file := range loaded.Syntax {
		for _, declaration := range file.Decls {
			switch declared := declaration.(type) {
			case *ast.FuncDecl:
				if slices.Contains(names, declared.Name.Name) {
					declared.Body = nil
					withheld++
					positions = append(positions, loaded.Fset.Position(declared.Pos()).String())
				}
			case *ast.GenDecl:
				for _, specification := range declared.Specs {
					value, isValue := specification.(*ast.ValueSpec)
					if isValue && declared.Tok == token.VAR && slices.Contains(names, value.Names[0].Name) {
						value.Values = nil
						withheld++
						positions = append(positions, loaded.Fset.Position(value.Pos()).String())
					}
				}
			}
		}
	}
	if withheld != len(names) {
		test.Fatalf("want %d declarations of %s withheld from, %q, found %d", len(names), loaded.ID, names, withheld)
	}
	return positions
}

// A cgo file checked as written, package "C" faked, as the engine's load checks a dependency's where the project has
// no cgo of its own, imports "C", so its units carry the mark a translated file's do, and so do those of every other
// file of its package, whose bodies the load may have emptied (cgo_files.go). Where a build keeps cgo on in the
// package each is a seed, Slots of the cgo file and Steady of the plain file alike, and so where the package differs
// otherwise: a build with cgo off and no `!cgo` file, whose cgo file is the host's alone, and a build with a plain file
// of its own, each the one rule that seeds Steady there, nothing of the package withheld; where no build says so, none
// is.
// Cgo is kept on in a package (KeepsCgo) when the build's cgo tool translated it, whatever the host did, and when the
// host's did, whatever the build does, a build that lists no cgo file, cgo being off, among them, since the file the
// host's tool generated is in no file list; only where both check the package as written is it not. No C compiler
// runs here.
func TestACgoFileCheckedAsWrittenIsMarkedByItsImport(test *testing.T) {
	test.Parallel()

	const path = "example.com/faked"
	cgoFile, plainFile := "/faked/native.go", "/faked/plain.go"
	faked := checkedAsWritten(test, path, [][2]string{
		{plainFile, "package faked\n\nconst Steady = 1\n"},
		{cgoFile, "package faked\n\nimport \"C\"\n\nvar Slots = C.SLOTS\n"},
	})
	translated := &packages.Package{ID: path, PkgPath: path, GoFiles: faked.GoFiles,
		CompiledGoFiles: []string{plainFile, "/cache/generated", "/cache/translation"}}
	withoutCgo := &packages.Package{ID: path, PkgPath: path, GoFiles: []string{plainFile},
		CompiledGoFiles: []string{plainFile}}

	if cgoFiles := CgoFiles(faked); len(cgoFiles) > 0 {
		test.Errorf("want no file of a package checked as written translated, got %q", cgoFiles)
	}
	if cgoFiles := CgoFiles(translated); !slices.Equal(cgoFiles, []string{cgoFile}) {
		test.Errorf("want %s alone translated, got %q", cgoFile, cgoFiles)
	}
	for _, kept := range []struct {
		name         string
		hostCgoFiles []string
		built        *packages.Package
		want         bool
	}{
		{"both builds translate it", CgoFiles(translated), translated, true},
		{"the build translates what the host faked", CgoFiles(faked), translated, true},
		{"the build fakes what the host translated", CgoFiles(translated), faked, true},
		{"both builds fake it", CgoFiles(faked), faked, false},
		{"the build has cgo off where the host translated", CgoFiles(translated), withoutCgo, true},
		{"the build has cgo off where the host faked", CgoFiles(faked), withoutCgo, false},
	} {
		if got := KeepsCgo(kept.hostCgoFiles, kept.built); got != kept.want {
			test.Errorf("%s: want cgo kept on %t, got %t", kept.name, kept.want, got)
		}
	}

	live := NewGraph()
	live.AddPackage(faked, true, nil)
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "faked resolved", resolvedLines(live), resolvedLines(recorded))
	keepsCgo := Difference{Packages: map[string]PackageDifference{path: {KeepsCgo: true}}}
	cgoOff := Difference{Packages: map[string]PackageDifference{path: {HostOnlyFiles: []string{cgoFile},
		SharedFiles: []string{plainFile}}}}
	extra, parseError := parser.ParseFile(token.NewFileSet(), "/faked/extra.go",
		"//go:build extra\n\npackage faked\n\nconst Extra = 2\n", parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	withExtra := Difference{Packages: map[string]PackageDifference{path: {BuildOnlyFiles: []*ast.File{extra}}}}
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		for _, judged := range []struct {
			name       string
			difference Difference
			seeded     bool
		}{
			{"a build that keeps cgo on", keepsCgo, true},
			{"a build with cgo off and no `!cgo` file", cgoOff, true},
			{"a build with a plain file of its own", withExtra, true},
			{"a build that says nothing of cgo", Difference{}, false},
		} {
			dependence := graph.graph.Against(judged.difference)
			for _, local := range []string{"Slots", "Steady"} {
				if bits, isDeclared := dependence.NameBits(path, local); !isDeclared || bits.Name != judged.seeded {
					test.Errorf("%s: want its name bit %t under %s in %s, got %+v, declared %t", local, judged.seeded,
						judged.name, graph.name, bits, isDeclared)
				}
			}
		}
	}
}

// Where the host checks a package as written, the load has emptied, before the graph collects the package, every
// function body and every var initializer that reaches cgo, in its plain files too (load.withholdCgoDeclarations, which
// the test does by hand, no loader running here). Raw, a plain file's var whose initializer calls raw of the cgo file,
// then reaches the collector with no value and a type "C" left invalid: no edge leads from it to raw. Fatal, a plain
// file's function whose body calls cleanup of the cgo file, reaches it with no body: it reads nothing. A build whose
// cgo tool translates the package checks Raw as a `_Ctype_long` and Fatal's body for the first time, and so does a
// build with cgo off, every other platform's, which compiles the `!cgo` file in the cgo file's place, Raw an int64
// there, with nothing said of cgo kept on (KeepsCgo false). So both are build-dependent wherever the package differs
// at all, as every unit of a package checked as written beside a file that imports "C" is, and neither where no build
// says so. The test holds the mark alone: no withheld positions are handed to the graph, so the row where no build
// says so wants neither seeded. The real load hands the graph Raw's and Fatal's positions, and each is then a seed in
// every build, that row's too (TestAUnitTheHostWithheldIsASeedInEveryBuild).
func TestAFakedCgoPackagesWithheldUnitsAreSeedsWhereThePackageDiffers(test *testing.T) {
	test.Parallel()

	const path = "example.com/withheld"
	plainFile, cgoFile, withoutCgoFile := "/withheld/plain.go", "/withheld/native.go", "/withheld/without_cgo.go"
	faked := checkedAsWritten(test, path, [][2]string{
		{plainFile, "package withheld\n\nvar Raw = raw()\n\nfunc Fatal() {\n\tcleanup()\n" +
			"\tpanic(\"fatal\")\n}\n"},
		{cgoFile, "package withheld\n\n// long value(void);\nimport \"C\"\n\n" +
			"func raw() C.long { return C.value() }\n\nfunc cleanup() {}\n"},
	})
	withhold(test, faked, "Raw", "Fatal")
	withoutCgo := &packages.Package{ID: path, PkgPath: path, GoFiles: []string{plainFile, withoutCgoFile},
		CompiledGoFiles: []string{plainFile, withoutCgoFile}}
	buildOnly, parseError := parser.ParseFile(token.NewFileSet(), withoutCgoFile,
		"//go:build !cgo\n\npackage withheld\n\nfunc raw() int64 { return 8 }\n\nfunc cleanup() {}\n",
		parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	cgoOff := Difference{Packages: map[string]PackageDifference{path: {
		HostOnlyFiles:  []string{cgoFile},
		SharedFiles:    []string{plainFile},
		BuildOnlyFiles: []*ast.File{buildOnly},
		KeepsCgo:       KeepsCgo(CgoFiles(faked), withoutCgo),
	}}}
	if cgoOff.Packages[path].KeepsCgo {
		test.Fatal("want cgo not kept on where the host faked the package and the build has cgo off")
	}

	live := NewGraph()
	live.AddPackage(faked, false, nil)
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "withheld resolved", resolvedLines(live), resolvedLines(recorded))
	keepsCgo := Difference{Packages: map[string]PackageDifference{path: {KeepsCgo: true}}}
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		for _, judged := range []struct {
			name       string
			difference Difference
			want       bool
		}{
			{"a build that keeps cgo on", keepsCgo, true},
			{"a build with cgo off", cgoOff, true},
			{"a build that says nothing of cgo", Difference{}, false},
		} {
			dependence := graph.graph.Against(judged.difference)
			if bits, isDeclared := dependence.NameBits(path, "Raw"); !isDeclared || bits.Name != judged.want {
				test.Errorf("Raw: want its name bit %t under %s in %s, got %+v, declared %t", judged.want, judged.name,
					graph.name, bits, isDeclared)
			}
			if dependent := unitVerdictOf(test, faked, dependence, "func Fatal"); dependent != judged.want {
				test.Errorf("func Fatal: want dependent %t under %s in %s, got %t", judged.want, judged.name,
					graph.name, dependent)
			}
		}
	}
}

// A unit the host's load withheld from is a seed in every build, under a difference that says nothing of its package
// at all, in the live graph and in the graph read back from its records, whose unit flags carry the mark: the host
// never checked what it held, whatever the load withheld it for, its own cgo file or another faked package's invalid
// expression (load's holdsInvalid), which the engine's TestWhatTheHostWithheldIsASeedInEveryBuild loads. Here a
// package checked as written beside a cgo file has Doubled's initializer and Size's body withheld by hand, as the
// load withholds them (withhold), and its positions handed to the graph; Steady, left whole, is no seed.
func TestAUnitTheHostWithheldIsASeedInEveryBuild(test *testing.T) {
	test.Parallel()

	const path = "example.com/withheldelsewhere"
	faked := checkedAsWritten(test, path, [][2]string{
		{"/elsewhere/plain.go", "package withheldelsewhere\n\nvar Doubled = value() * 2\n\n" +
			"func Size() any { return value() }\n\nconst Steady = 1\n"},
		{"/elsewhere/native.go", "package withheldelsewhere\n\nimport \"C\"\n\n" +
			"func value() C.int { return 4 }\n"},
	})
	withheld := withhold(test, faked, "Doubled", "Size")

	live := NewGraph()
	live.AddPackage(faked, false, withheld)
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "withheld elsewhere resolved", resolvedLines(live), resolvedLines(recorded))
	encoded, _ := live.EncodedRecord(path)
	decoded, decodeError := decodeRecord(encoded)
	if decodeError != nil {
		test.Fatalf("withheldelsewhere's record: %v", decodeError)
	}
	expectEncodedAgain(test, "withheldelsewhere's record", encodeRecord(decoded), encoded)
	var marked []string
	for _, unit := range decoded.units {
		if unit.withheld {
			marked = append(marked, fmt.Sprintf("%s:%d", unit.file, unit.start))
		}
	}
	if len(marked) != 2 {
		test.Errorf("want the record to read back Doubled's and Size's units withheld, got %q", marked)
	}
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		dependence := graph.graph.Against(Difference{})
		for local, want := range map[string]bool{"Doubled": true, "Size": true, "Steady": false} {
			if bits, isDeclared := dependence.NameBits(path, local); !isDeclared || bits.Name != want {
				test.Errorf("%s: want its name bit %t under a build that says nothing of the package in %s, got "+
					"%+v, declared %t", local, want, graph.name, bits, isDeclared)
			}
		}
	}
}

// A withheld position no unit's declaration starts at is one the graph cannot place: the load and the graph disagree
// on where a declaration is, and the unit the load withheld would keep no mark. So every unit of the package is marked
// withheld instead, the larger set, and each is a seed under a difference that says nothing of the package, in the
// live graph and in the graph read back from its records.
func TestAWithheldPositionNoUnitStartsAtMarksTheWholePackage(test *testing.T) {
	test.Parallel()

	const path = "example.com/misplaced"
	faked := checkedAsWritten(test, path, [][2]string{
		{"/misplaced/plain.go", "package misplaced\n\nconst Steady = 1\n\nfunc Plain() int { return Steady }\n"},
		{"/misplaced/native.go", "package misplaced\n\nimport \"C\"\n\nvar Slots = C.SLOTS\n"},
	})
	live := NewGraph()
	live.AddPackage(faked, false, []string{"/misplaced/plain.go:40:1"})
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "misplaced resolved", resolvedLines(live), resolvedLines(recorded))
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		dependence := graph.graph.Against(Difference{})
		for _, local := range []string{"Steady", "Plain", "Slots"} {
			if bits, isDeclared := dependence.NameBits(path, local); !isDeclared || !bits.Name {
				test.Errorf("%s: want its name bit under a build that says nothing of the package in %s, its "+
					"package's withheld position placing no unit, got %+v, declared %t", local, graph.name, bits,
					isDeclared)
			}
		}
	}
}

// The files of the package translatedByHand builds for the tests below: its plain Go file and its cgo file as written,
// the `!cgo` file a build with cgo off compiles in the cgo file's place, and the cgo tool's two outputs in the build
// cache, the file it generates and the cgo file's translation.
const (
	handPlainFile     = "/native/plain.go"
	handCgoFile       = "/native/native.go"
	handWithoutCgo    = "/native/without_cgo.go"
	handGeneratedFile = "/cache/_cgo_gotypes.go"
	handTranslation   = "/cache/native.cgo1.go"
)

// translatedByHand is a package as the cgo tool leaves one, built by hand so no C compiler runs: its files as written
// are a plain Go file and a cgo file, and what it compiles, and its syntax, the plain file and the tool's two outputs,
// neither of them a file as written. The translation opens with the `//line` directive the tool writes, so a reader
// sees its units in the cgo file. The generated file's declarations have none, so a reader sees its units in the build
// cache, down to the wrapper of an exported function whose result may hold a pointer, inside which the tool writes a
// directive naming the cgo file (cmd/cgo's writeExports): `_Ciconst_AFTER`, declared after it, is a unit a reader sees
// in the cgo file. The plain file's WidthBytes names a generated type, as nothing stops a plain file doing, and its
// Steady names nothing of C. The translation's Width returns a C type, and its Handle, Handle's method Close and Plain
// name nothing of C.
func translatedByHand(test *testing.T) *packages.Package {
	test.Helper()
	translated := parsedPackage(test, "native", [][2]string{
		{handPlainFile, "package native\n\nimport \"unsafe\"\n\n" +
			"const WidthBytes = unsafe.Sizeof(_Ctype_width_t(0))\n\nconst Steady = 1\n"},
		{handGeneratedFile, "package native\n\ntype _Ctype_int int32\n\ntype _Ctype_width_t = _Ctype_int\n\n" +
			"func _cgoexp_Exported() {\n//line " + handCgoFile + ":9\n\t_ = 0\n}\n\nconst _Ciconst_AFTER = 2\n"},
		{handTranslation, "//line " + handCgoFile + ":1:1\npackage native\n\n" +
			"func Width() _Ctype_width_t { return 4 }\n\ntype Handle struct{}\n\nfunc (Handle) Close() {}\n\n" +
			"func Plain() int { return 1 }\n"},
	})
	translated.GoFiles = []string{handPlainFile, handCgoFile}
	translated.CompiledGoFiles = []string{handPlainFile, handGeneratedFile, handTranslation}
	return translated
}

// Where the host's cgo tool translated a package and the build drops its cgo files, cgo being off there or its tags
// leaving them out, the translation's units are in a file only the host compiles, by the name its `//line` directive
// gives them, and are seeds as any such file's are. The generated file's are not: a reader sees them in the build
// cache, a path no file list holds. So WidthBytes, a plain file's constant that sizes `_Ctype_width_t`, which the
// build's `!cgo` file declares as an int64 where the host's tool wrote an int32, is 4 on the host and 8 in the build
// with no file of its own differing, and only the mark on the generated file's units says so. The difference is the
// engine's for such a build: the cgo file the host's alone, the `!cgo` file the build's alone, and what KeepsCgo says
// of the two loads. Steady, which names nothing of C, stays independent.
func TestTheGeneratedFilesUnitsAreSeedsWhereTheBuildDropsTheCgoFiles(test *testing.T) {
	test.Parallel()

	translated := translatedByHand(test)
	withoutCgo := &packages.Package{ID: translated.ID, PkgPath: translated.PkgPath,
		GoFiles: []string{handPlainFile, handWithoutCgo}, CompiledGoFiles: []string{handPlainFile, handWithoutCgo}}
	buildOnly, parseError := parser.ParseFile(token.NewFileSet(), handWithoutCgo,
		"//go:build !cgo\n\npackage native\n\ntype _Ctype_width_t = int64\n\n"+
			"func Width() _Ctype_width_t { return 8 }\n", parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	difference := Difference{Packages: map[string]PackageDifference{translated.ID: {
		HostOnlyFiles:  []string{handCgoFile},
		SharedFiles:    []string{handPlainFile},
		BuildOnlyFiles: []*ast.File{buildOnly},
		KeepsCgo:       KeepsCgo(CgoFiles(translated), withoutCgo),
	}}}

	live := NewGraph()
	live.AddPackage(translated, true, nil)
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "native resolved", resolvedLines(live), resolvedLines(recorded))
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		dependence := graph.graph.Against(difference)
		for _, name := range []struct {
			local string
			want  bool
		}{
			{"Width", true},
			{"_Ctype_width_t", true},
			{"_Ctype_int", true},
			{"WidthBytes", true},
			{"Steady", false},
		} {
			if bits, isDeclared := dependence.NameBits(translated.PkgPath, name.local); !isDeclared ||
				bits.Name != name.want {
				test.Errorf("%s: want its name bit %t where the build drops the cgo files in %s, got %+v, declared %t",
					name.local, name.want, graph.name, bits, isDeclared)
			}
		}
	}
}

// The cgo tool's outputs carry the mark with no C compiler run, on translatedByHand's package, whose compiled files
// are none of its files as written, as the tool leaves them. Where both builds translate the package and no file as
// written differs, every unit of the generated file and of the translation is a seed by its mark alone: the generated
// types, Width, which returns one, and Handle, Close and Plain of the translation, which name nothing of C, so no edge
// to a generated object stands in for their mark. `_Ciconst_AFTER`, which the generated file declares after the
// directive the tool writes inside an exported function's wrapper, is a unit a reader sees in the cgo file, at an
// offset of the generated file, and carries the mark all the same, the mark being set from the parsed file's own name
// and never from a unit's place. WidthBytes reads a seed, and Steady, of the plain file, reads none. Close is a marked
// unit with a receiver, the one kind whose record writes both unit flags: its record reads back with its receiver and
// its mark, and encodes again to the same bytes. Where nothing is said of cgo, no bit is set.
func TestTheCgoToolsOutputsAreMarkedWithNoCompiler(test *testing.T) {
	test.Parallel()

	translated := translatedByHand(test)
	live := NewGraph()
	live.AddPackage(translated, true, nil)
	recorded := recordedGraphOf(test, live)
	expectAlike(test, "native resolved", resolvedLines(live), resolvedLines(recorded))

	encoded, _ := live.EncodedRecord(translated.ID)
	decoded, decodeError := decodeRecord(encoded)
	if decodeError != nil {
		test.Fatalf("native's record: %v", decodeError)
	}
	expectEncodedAgain(test, "native's record", encodeRecord(decoded), encoded)
	var methods []string
	for _, unit := range decoded.units {
		if unit.receiver != "" {
			methods = append(methods, fmt.Sprintf("%s in %s, marked %t", unit.receiver, unit.file, unit.fromCgo))
		}
	}
	if want := []string{"Handle in " + handCgoFile + ", marked true"}; !slices.Equal(methods, want) {
		test.Errorf("want the record to read back one unit with a receiver, %q, got %q", want, methods)
	}

	keepsCgo := Difference{Packages: map[string]PackageDifference{translated.ID: {KeepsCgo: true}}}
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		for _, judged := range []struct {
			name       string
			difference Difference
			seeded     bool
		}{
			{"a build that translates the package again", keepsCgo, true},
			{"a build that says nothing of cgo", Difference{}, false},
		} {
			dependence := graph.graph.Against(judged.difference)
			for _, name := range []struct {
				local string
				want  bool
			}{
				{"_Ctype_int", judged.seeded},
				{"_Ctype_width_t", judged.seeded},
				{"_Ciconst_AFTER", judged.seeded},
				{"Width", judged.seeded},
				{"Handle", judged.seeded},
				{"Plain", judged.seeded},
				{"WidthBytes", judged.seeded},
				{"Steady", false},
			} {
				if bits, isDeclared := dependence.NameBits(translated.PkgPath, name.local); !isDeclared ||
					bits.Name != name.want {
					test.Errorf("%s: want its name bit %t under %s in %s, got %+v, declared %t", name.local, name.want,
						judged.name, graph.name, bits, isDeclared)
				}
			}
			if dependent := unitVerdictOf(test, translated, dependence, "method Handle.Close"); dependent !=
				judged.seeded {
				test.Errorf("method Handle.Close: want dependent %t under %s in %s, got %t", judged.seeded,
					judged.name, graph.name, dependent)
			}
			index, isDeclared := dependence.UnitDeclaringName(translated.PkgPath, "_Ciconst_AFTER")
			if !isDeclared || dependence.Units()[index].File != handCgoFile {
				test.Errorf("_Ciconst_AFTER: want its unit placed in %s by the generated file's directive in %s, "+
					"declared %t", handCgoFile, graph.name, isDeclared)
			}
		}
	}
}

// A place in the cgo file as written is in the unit whose span holds it however the cgo tool's outputs share the name.
// translatedByHand's generated file names the cgo file past the directive inside the exported function's wrapper, so
// `_Ciconst_AFTER`, a unit of the generated file, comes before every unit of the translation in the cgo file's row, at
// an offset of the generated file past Plain's start and inside its span: the row is out of start order, and two of
// its spans overlap. Each unit of the row holds its own start, `_Ciconst_AFTER` starting after Plain and so holding
// the place both spans do; Plain holds the place past `_Ciconst_AFTER`'s end, and Width its own last byte; no unit
// holds the place past every end, and every place of every file answers as the maps did (expectUnitsFoundAsByMaps).
// A search by start alone, binary over the row as the units come, finds another unit or none.
func TestTheUnitHoldingAPlaceIsFoundWhereTheGeneratedFileNamesTheCgoFile(test *testing.T) {
	test.Parallel()

	translated := translatedByHand(test)
	live := NewGraph()
	live.AddPackage(translated, true, nil)
	recorded := recordedGraphOf(test, live)
	for _, graph := range []struct {
		name  string
		graph *Graph
	}{
		{"the live graph", live},
		{"the graph read back from its records", recorded},
	} {
		resolved := graph.graph.resolve()
		declaring := func(local string) int {
			index, isDeclared := resolved.unitDeclaringName(translated.PkgPath, local)
			if !isDeclared || resolved.files.names[resolved.unitFile[index]] != handCgoFile {
				test.Fatalf("%s: want a unit of %s declaring it in %s, declared %t", local, handCgoFile, graph.name,
					isDeclared)
			}
			return index
		}
		after, plain, width := declaring("_Ciconst_AFTER"), declaring("Plain"), declaring("Width")
		start, end := func(index int) int { return int(resolved.unitStart[index]) },
			func(index int) int { return int(resolved.unitEnd[index]) }
		if !(start(plain) < start(after) && end(after) < end(plain)) {
			test.Fatalf("want _Ciconst_AFTER, %d to %d, inside Plain, %d to %d, in %s", start(after), end(after),
				start(plain), end(plain), graph.name)
		}
		var lastEnd int
		places := map[int]int{end(after): plain, end(width) - 1: width}
		for index := range resolved.unitCount() {
			if resolved.files.names[resolved.unitFile[index]] == handCgoFile {
				places[start(index)] = index
				lastEnd = max(lastEnd, end(index))
			}
		}
		for offset, want := range places {
			if got, isFound := resolved.unitContaining(translated.ID, handCgoFile, offset); !isFound || got != want {
				test.Errorf("%s:%d: want it in unit %d in %s, got %d, found %t", handCgoFile, offset, want, graph.name,
					got, isFound)
			}
		}
		if got, isFound := resolved.unitContaining(translated.ID, handCgoFile, lastEnd); isFound {
			test.Errorf("%s:%d: want it past every unit in %s, got unit %d", handCgoFile, lastEnd, graph.name, got)
		}
		expectUnitsFoundAsByMaps(test, "native in "+graph.name, graph.graph)
	}
}
