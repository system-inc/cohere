package go_engine

import (
	"cmp"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// A use reaches its body by the load mode (#w72zxtq) when its object's package is one the host and the build load in
// different modes, one from export data and the other from source: export data holds an untyped float as a 512-bit
// binary fraction and a check from source holds it exactly, so at cohere's root, wherever the host reads math from the
// system cache and a build misses its own there and checks it from source, goja's math.Sqrt2 / 2 type-checks to two
// values. The Square ruled the rule general: every use of any object of such a package reaches, of any kind, and only
// a package loaded alike reaches nothing. One the host does not hold has no mode to compare, and reaches, failing
// wide, and so does one the build's graph lacks, whatever the host's mode (#bed0mnk): the direct rule reads a package
// by its ID alone, whatever the carry makes of one the build lacks (#wzre5eg), since the package the build holds of
// its path in its place, a test variant, may declare the object otherwise. The census still counts the narrower rule
// it replaced, an untyped float or complex constant read by its value alone. A fixture loads a package two ways only
// over a filled system cache, the whole standard library at once
// (TestAPackageLoadedOtherwiseStillStartsTheCarryBesideALackedOne), so the rule's cases are pinned here.
func TestAUseReachesItsBodyWhereTheLoadModesDiffer(test *testing.T) {
	test.Parallel()

	math := types.NewPackage("math", "math")
	unheld := types.NewPackage("example.com/unheld", "unheld")
	sqrt2 := types.NewConst(token.NoPos, math, "Sqrt2", types.Typ[types.UntypedFloat],
		constant.MakeFromLiteral("1.41421356237309504880168872420969807856967187537694807317667974", token.FLOAT, 0))
	imaginary := types.NewConst(token.NoPos, math, "Imaginary", types.Typ[types.UntypedComplex],
		constant.MakeImag(constant.MakeInt64(1)))
	maximum := types.NewConst(token.NoPos, math, "MaxInt8", types.Typ[types.UntypedInt], constant.MakeInt64(127))
	typed := types.NewConst(token.NoPos, math, "Typed", types.Typ[types.Float64], constant.MakeFloat64(0.5))
	function := types.NewFunc(token.NoPos, math, "Abs", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	elsewhere := types.NewConst(token.NoPos, unheld, "Pi", types.Typ[types.UntypedFloat],
		constant.MakeFloat64(3.14))
	named := build_reads.UsedNamed
	byValue := build_reads.UsedNamed | build_reads.UsedConstant
	// lacked is the mode of a package the build's graph does not hold.
	const lacked = builtLoadMode("")
	cases := []struct {
		name           string
		object         types.Object
		kinds          build_reads.UseKinds
		hostExportData bool
		built          builtLoadMode
		general        bool
		constantsOnly  bool
	}{
		{"an untyped float, export data on the host and source in the build", sqrt2, byValue, true, builtFromSource,
			true, true},
		{"an untyped float, source on the host and export data in the build", sqrt2, byValue, false, builtRestored,
			true, true},
		{"an untyped float, source in both", sqrt2, byValue, false, builtFromSource, false, false},
		{"an untyped float, export data in both", sqrt2, byValue, true, builtRestored, false, false},
		{"an untyped complex, export data on the host and source in the build", imaginary, byValue, true,
			builtFromSource, true, true},
		{"an untyped integer, export data on the host and source in the build", maximum, byValue, true,
			builtFromSource, true, false},
		{"a typed float, export data on the host and source in the build", typed, byValue, true, builtFromSource, true,
			false},
		{"a function, export data on the host and source in the build", function, named, true, builtFromSource, true,
			false},
		{"a function, source in both", function, named, false, builtFromSource, false, false},
		{"an untyped float of a package the host does not hold", elsewhere, byValue, true, builtFromSource, true,
			true},
		{"an untyped float, source on the host and no package of its ID in the build", sqrt2, byValue, false, lacked,
			true, true},
		{"a function, source on the host and no package of its ID in the build", function, named, false, lacked, true,
			false},
		{"an untyped float, export data on the host and no package of its ID in the build", sqrt2, byValue, true,
			lacked, true, true},
		{"a function, export data on the host and no package of its ID in the build", function, named, true, lacked,
			true, false},
		{"a function, source on the host and export data in the build", function, named, false, builtRestored, true,
			false},
	}
	for _, each := range cases {
		graph := newReachBuiltGraph(&load.Loaded{})
		if each.built != lacked {
			graph.hold("math", "math", each.built)
		}
		reacher := &bodyReacher{
			host: &bodyReachHost{identifiers: map[*types.Package]string{math: "math"},
				dependence: &hostDependence{exportData: map[string]bool{"math": each.hostExportData}}},
			graph: graph,
		}
		if got := reacher.loadModeReaches(each.object, each.kinds); got != each.general {
			test.Errorf("%s: the load mode reaches its body %t, want %t", each.name, got, each.general)
		}
		reacher.constantsOnly = true
		if got := reacher.loadModeReaches(each.object, each.kinds); got != each.constantsOnly {
			test.Errorf("%s: the constants-only rule reaches its body %t, want %t", each.name, got,
				each.constantsOnly)
		}
	}
}

// recordedSource is one package the reach's unit tests type-check and record: its path, its imports' paths, and its
// files' sources by name.
type recordedSource struct {
	path    string
	imports []string
	files   map[string]string
}

// recordedPackages type-checks sources, each after those it imports, in one file set, and records each one's reads as
// the host's walk does under the census, its function uses asked for, by its path as its ID, so the reach's judgment
// can be held to records it really makes.
func recordedPackages(
	test *testing.T,
	sources []recordedSource,
) (map[string]*packages.Package, map[*packages.Package]*build_reads.Reads) {
	test.Helper()
	fileSet := token.NewFileSet()
	checked := map[string]*packages.Package{}
	reads := map[*packages.Package]*build_reads.Reads{}
	for _, source := range sources {
		loaded := &packages.Package{ID: source.path, PkgPath: source.path, Fset: fileSet,
			Imports: map[string]*packages.Package{}}
		for _, name := range slices.Sorted(maps.Keys(source.files)) {
			file, parseError := parser.ParseFile(fileSet, name, source.files[name], parser.ParseComments)
			if parseError != nil {
				test.Fatal(parseError)
			}
			loaded.Syntax = append(loaded.Syntax, file)
			loaded.GoFiles = append(loaded.GoFiles, name)
		}
		for _, imported := range source.imports {
			loaded.Imports[imported] = checked[imported]
		}
		loaded.TypesInfo = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
			Uses: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{}, Instances: map[*ast.Ident]types.Instance{},
			Scopes: map[ast.Node]*types.Scope{}}
		configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
			return checked[path].Types, nil
		})}
		typesPackage, checkError := configuration.Check(source.path, fileSet, loaded.Syntax, loaded.TypesInfo)
		if checkError != nil {
			test.Fatal(checkError)
		}
		loaded.Types = typesPackage
		recorded := &build_reads.Reads{}
		recorded.RecordFunctionUses()
		recorded.Claim()
		recorded.Record(typesPackage, loaded.TypesInfo, loaded.Syntax)
		checked[source.path], reads[loaded] = loaded, recorded
	}
	return checked, reads
}

// importerFunction is a function as a types.Importer.
type importerFunction func(path string) (*types.Package, error)

// Import is the package at path.
func (imported importerFunction) Import(path string) (*types.Package, error) {
	return imported(path)
}

// unchangedDependence is a build's judgment of an empty graph whose every object answers unchanged, so no bit reaches
// anything.
func unchangedDependence() *build_dependence.Dependence {
	return build_dependence.NewGraph().Against(build_dependence.Difference{
		DependentObject: func(types.Object) (bool, bool) { return false, false }})
}

// reachRecorded is the census of a build judged against an empty graph whose every object answers unchanged
// (unchangedDependence), over checked's packages, math among them, which the host read from export data and the build
// checks from source, so only the load mode reaches anything, each package as the build reads it altered by alter
// when it is not nil.
func reachRecorded(
	test *testing.T,
	checked map[string]*packages.Package,
	reads map[*packages.Package]*build_reads.Reads,
	alter func(*reachBuiltPackage),
) *buildBodyReach {
	test.Helper()
	host := newBodyReachHost(&hostDependence{exportData: map[string]bool{"math": true},
		decoded: map[*types.Package]bool{}}, reads, nil, nil)
	hostFiles := map[string][]string{}
	for identifier, each := range checked {
		hostFiles[identifier] = each.GoFiles
	}
	loaded := &load.Loaded{Roots: slices.Collect(maps.Values(checked))}
	built := reachBuiltPackages(loaded, hostFiles)
	if alter != nil {
		for _, read := range built {
			alter(read)
		}
	}
	return host.reach(unchangedDependence(), built, newReachBuiltGraph(loaded), false)
}

// mathSource is a math package of one untyped float constant, which the reach's unit tests load two ways.
var mathSource = recordedSource{path: "math", files: map[string]string{"/math/math.go": `package math

const Pi = 3.14159265358979323846264338327950288419716939937510582097494459
`}}

// A constant declared from what the load mode reaches holds the other load's value too (#w72zxtq, the review's must 3):
// `const twoPi = 2 * math.Pi` is reached by math.Pi, and so is Turn, which reads twoPi alone, a constant of a package
// loaded alike in both, and TwoPi, declared from twoPi, and a's Around, reading TwoPi from another package judged
// before q's: the load mode is carried through each declaration to a fixed point over the build. Plain and Still read
// nothing loaded otherwise, and stay unreached.
func TestTheLoadModeIsCarriedThroughEachDeclaration(test *testing.T) {
	test.Parallel()

	checked, reads := recordedPackages(test, []recordedSource{mathSource,
		{path: "example.com/q", imports: []string{"math"}, files: map[string]string{"/q/q.go": `package q

import "math"

const twoPi = 2 * math.Pi

const TwoPi = twoPi

func Turn(angle float64) float64 { return angle * twoPi }

func Plain(angle float64) float64 { return angle }
`}},
		{path: "example.com/a", imports: []string{"example.com/q"}, files: map[string]string{"/a/a.go": `package a

import "example.com/q"

func Around(angle float64) float64 { return angle * q.TwoPi }

func Still(angle float64) float64 { return -angle }
`}}})
	census := reachRecorded(test, checked, reads, nil)
	got := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier != "math" {
			got[body.name] = cmp.Or(string(body.reason), "unreached")
		}
	}
	want := map[string]string{"twoPi": "LoadMode", "TwoPi": "LoadMode", "Turn": "LoadMode", "Plain": "unreached",
		"Around": "LoadMode", "Still": "unreached"}
	if !maps.Equal(got, want) {
		test.Errorf("the census judges\n%v\nwant\n%v", got, want)
	}
}

// An unrecorded package's value specs carry the load mode too (#w72zxtq, the review's must A): the reach judges no
// declaration of a package the host recorded no uses of, reaching its bodies whole, so it cannot tell which of its
// constants read what the load mode reaches. While any package loads otherwise, every package-level const and var of
// one is carried, wider than needed: q's TwoPi, declared from math.Pi, reaches a's Around, which reads it alone, and
// a's turn, declared from it, carries it to Turn, though a is judged before q. Still reads nothing of q, and stays
// unreached.
func TestAnUnrecordedPackagesValuesCarryTheLoadMode(test *testing.T) {
	test.Parallel()

	checked, reads := recordedPackages(test, []recordedSource{mathSource,
		{path: "example.com/q", imports: []string{"math"}, files: map[string]string{"/q/q.go": `package q

import "math"

const TwoPi = 2 * math.Pi
`}},
		{path: "example.com/a", imports: []string{"example.com/q"}, files: map[string]string{"/a/a.go": `package a

import "example.com/q"

const turn = 2 * q.TwoPi

func Turn(angle float64) float64 { return angle * turn }

func Around(angle float64) float64 { return angle * q.TwoPi }

func Still(angle float64) float64 { return -angle }
`}}})
	// q's record taken away, as a package checked for its types alone or with cgo holds none.
	reads[checked["example.com/q"]] = nil
	census := reachRecorded(test, checked, reads, nil)
	got := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier == "example.com/a" {
			got[body.name] = cmp.Or(string(body.reason), "unreached")
		}
	}
	want := map[string]string{"turn": "LoadMode", "Turn": "LoadMode", "Around": "LoadMode", "Still": "unreached"}
	if !maps.Equal(got, want) {
		test.Errorf("the census judges\n%v\nwant\n%v", got, want)
	}
	if census.byReason[reachedUnrecorded] != 1 {
		test.Errorf("want q's one body reached, Unrecorded, got %d", census.byReason[reachedUnrecorded])
	}
	// A build loading every package as the host does carries none of q's values, which can hold no other load's.
	graph := newReachBuiltGraph(&load.Loaded{Roots: slices.Collect(maps.Values(checked))})
	for _, mathExportData := range []bool{false, true} {
		reacher := &bodyReacher{host: newBodyReachHost(&hostDependence{
			exportData: map[string]bool{"math": mathExportData}, decoded: map[*types.Package]bool{}}, reads, nil, nil),
			graph: graph, carried: map[types.Object]bool{}}
		reacher.carryUnrecorded(checked["example.com/q"].Types)
		// TwoPi, q's one value, carried only where math loads otherwise.
		want := 0
		if mathExportData {
			want = 1
		}
		if len(reacher.carried) != want {
			test.Errorf("with math read from export data on the host %t, %d of q's values are carried, want %d",
				mathExportData, len(reacher.carried), want)
		}
	}
}

// The carry starts by a package the build holds, never by one it lacks whole (#wzre5eg). The host holds tool by its
// own ID, and the build's graph is read for tool's path: held by that ID, the two loads are compared as the direct
// rule compares them; lacked by that ID and held as its test variant, which an extra build's load checks in a
// command's place, the variant stands in, and the carry starts unless every package of the path is loaded in the
// host's mode for tool; and with nothing of the path held, nothing the build compiles can have read tool, so no carry
// starts. That last is held as a check, by the import specs of the files the build compiles: one that names the
// path all the same, a vendored package's by its path past the vendor directory, starts the carry as before, and is
// counted. No load makes such a graph, so the sets are built by hand.
func TestTheCarryStartsOnlyByAPackageTheBuildHolds(test *testing.T) {
	test.Parallel()

	const tool = "example.com/tool"
	const variant = "example.com/tool [example.com/tool.test]"
	type held struct {
		identifier string
		mode       builtLoadMode
	}
	cases := []struct {
		name           string
		path           string
		hostExportData bool
		holds          []held
		imported       string
		starts         bool
		byImport       bool
	}{
		{"held by its ID, source in both", tool, false, []held{{tool, builtFromSource}}, "", false, false},
		{"held by its ID, source on the host and export data in the build", tool, false, []held{{tool, builtRestored}},
			"", true, false},
		{"held by its ID, export data on the host and source in the build", tool, true,
			[]held{{tool, builtFromSource}}, "", true, false},
		{"lacked whole, source on the host", tool, false, nil, "", false, false},
		{"lacked whole, export data on the host", tool, true, nil, "", false, false},
		{"lacked by its ID, its test variant checked from source as the host checked it", tool, false,
			[]held{{variant, builtFromSource}}, "", false, false},
		{"lacked by its ID, its test variant restored where the host checked it from source", tool, false,
			[]held{{variant, builtRestored}}, "", true, false},
		{"lacked by its ID, its test variant checked from source where the host read export data", tool, true,
			[]held{{variant, builtFromSource}}, "", true, false},
		{"lacked by its ID, its test variant restored as the host restored it", tool, true,
			[]held{{variant, builtRestored}}, "", false, false},
		{"lacked by its ID, one of two of its path loaded otherwise", tool, false,
			[]held{{variant, builtFromSource}, {tool + " [example.com/other.test]", builtRestored}}, "", true, false},
		{"lacked whole, and a file the build compiles imports it", tool, false, nil, tool, true, true},
		{"lacked whole and vendored, and a file the build compiles imports it", "vendor/" + tool, false, nil, tool,
			true, true},
		{"lacked whole and vendored below a module, and a file the build compiles imports it",
			"example.com/module/vendor/" + tool, false, nil, tool, true, true},
		{"lacked whole, and a file the build compiles imports another path", tool, false, nil, tool + "/other", false,
			false},
	}
	for _, each := range cases {
		graph := newReachBuiltGraph(&load.Loaded{})
		for _, standIn := range each.holds {
			graph.hold(standIn.identifier, each.path, standIn.mode)
		}
		if each.imported != "" {
			graph.imported[each.imported] = true
		}
		reacher := &bodyReacher{graph: graph, host: &bodyReachHost{dependence: &hostDependence{
			paths: map[string]string{tool: each.path}, exportData: map[string]bool{tool: each.hostExportData}}}}
		if starts, byImport := reacher.startsCarry(tool, each.path); starts != each.starts ||
			byImport != each.byImport {
			test.Errorf("%s: it starts the carry %t, by the check %t, want %t and %t", each.name, starts, byImport,
				each.starts, each.byImport)
		}
		// The carry's trigger and the census's count read the same answer, over every package the host holds.
		var wantCounted []string
		if each.byImport {
			wantCounted = []string{tool}
		}
		if some, counted := reacher.anyLoadModeDiffers(), reacher.lackedYetImported(); some != each.starts ||
			!slices.Equal(counted, wantCounted) {
			test.Errorf("%s: some package starts the carry %t, and the check counts %v, want %t and %v", each.name,
				some, counted, each.starts, wantCounted)
		}
	}
}

// The check fires through a whole census (#wzre5eg): the host holds gone, the build's graph holds nothing of gone's
// path, and a's file, which the build compiles, imports gone all the same, a graph no load makes, made here by taking
// gone from a's imports once a is checked. The carry starts as it did before a lacked package was told apart, so
// Around, reading the constant of q, a package with no record, is reached by the load mode, and the census counts
// gone on the check's line. With gone held, loaded as the host loaded it, nothing starts the carry: Around stays
// unreached and the line reads 0.
func TestAPackageTheBuildLacksAndStillImportsStartsTheCarryAndIsCounted(test *testing.T) {
	test.Parallel()

	sources := []recordedSource{
		{path: "example.com/gone", files: map[string]string{"/gone/gone.go": `package gone

const Value = 1
`}},
		{path: "example.com/q", files: map[string]string{"/q/q.go": `package q

const Scale = 3
`}},
		{path: "example.com/a", imports: []string{"example.com/gone", "example.com/q"},
			files: map[string]string{"/a/a.go": `package a

import (
	_ "example.com/gone"
	"example.com/q"
)

func Around(angle int) int { return angle * q.Scale }
`}}}
	for _, each := range []struct {
		name     string
		lacked   bool
		around   string
		line     string
		byImport []string
	}{
		{"the build holding gone", false, "unreached",
			"  lacked by the build and imported by a file it compiles, which starts the carry: 0 packages", nil},
		{"the build lacking gone", true, "LoadMode",
			"  lacked by the build and imported by a file it compiles, which starts the carry: 1 packages",
			[]string{"example.com/gone"}},
	} {
		checked, reads := recordedPackages(test, sources)
		// q's record taken away, as a package checked for its types alone or with cgo holds none.
		reads[checked["example.com/q"]] = nil
		if each.lacked {
			delete(checked["example.com/a"].Imports, "example.com/gone")
		}
		// The host holds gone either way, the build only as a's imports reach it.
		host := newBodyReachHost(&hostDependence{decoded: map[*types.Package]bool{}}, reads, nil, nil)
		hostFiles := map[string][]string{}
		for identifier, hosted := range checked {
			hostFiles[identifier] = hosted.GoFiles
		}
		loaded := &load.Loaded{Roots: []*packages.Package{checked["example.com/a"]}}
		census := host.reach(unchangedDependence(), reachBuiltPackages(loaded, hostFiles), newReachBuiltGraph(loaded),
			false)
		around := ""
		for _, body := range census.bodies {
			if body.name == "Around" {
				around = cmp.Or(string(body.reason), "unreached")
			}
		}
		if around != each.around || !slices.Equal(census.lackedImported, each.byImport) {
			test.Errorf("%s: Around is %s and the check counts %v, want %s and %v", each.name, around,
				census.lackedImported, each.around, each.byImport)
		}
		var written strings.Builder
		// No rule ran, so the facts' reach took nothing out.
		writeBuildBodyReach(&written, "build fixture", census, nil)
		lines := strings.Split(written.String(), "\n")
		if !slices.Contains(lines, each.line) || each.lacked && !slices.Contains(lines, "    example.com/gone") {
			test.Errorf("%s: the census has no line %q, or names no gone under it:\n%s", each.name, each.line,
				written.String())
		}
	}
}

// A selection reaches its body by the load mode where a member on its way is of a package loaded otherwise (#w72zxtq,
// the review's mutant 3). a's Read selects Y, promoted into a's Wrapper through Outer, of a package the host read from
// export data and the build checks from source, which reachRecorded names math, and through Leaf, the field Outer
// embeds, which math declares. Y and Leaf's type are c's, and Wrapper and Outer's field a's, each loaded alike, so only
// the embedded field math declares, which the record holds as a selection of its own and no plain use, reaches.
// Plain selects Wrapper's own field, and stays unreached.
func TestASelectionThroughAMemberOfAPackageLoadedOtherwiseReachesItsBody(test *testing.T) {
	test.Parallel()

	checked, reads := recordedPackages(test, []recordedSource{
		{path: "example.com/c", files: map[string]string{"/c/c.go": `package c

type Leaf struct{ Y int }
`}},
		{path: "math", imports: []string{"example.com/c"}, files: map[string]string{"/math/outer.go": `package math

import "example.com/c"

type Outer struct{ c.Leaf }
`}},
		{path: "example.com/a", imports: []string{"math"}, files: map[string]string{"/a/a.go": `package a

import "math"

type Wrapper struct {
	math.Outer
	Own int
}

func Read(wrapper Wrapper) int { return wrapper.Y }

func Plain(wrapper Wrapper) int { return wrapper.Own }
`}}})
	census := reachRecorded(test, checked, reads, nil)
	got := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier == "example.com/a" {
			got[body.name] = cmp.Or(string(body.reason), "unreached") + " " + body.driver
		}
	}
	want := map[string]string{"Read": "LoadMode math.Outer.UF0 (/math/outer.go) LoadMode load mode in the body",
		"Plain": "unreached "}
	if !maps.Equal(got, want) {
		test.Errorf("the census judges\n%v\nwant\n%v", got, want)
	}
}

// A name a file imports by a dot, which a build-only file of the package declares at package level, means that file's
// object in the build rather than the one the host recorded (#w72zxtq): Measured calls Area, dot-imported from shapes,
// and is reached, Shadowed, though no bit is set. Such a build does not compile, the two names clashing in the file's
// block, so the case is a guard, and no fixture module can hold it: the build's read of q is told of the name here.
// Other, naming nothing of shapes, stays unreached.
func TestADotImportedNameABuildOnlyFileDeclaresReachesItsBody(test *testing.T) {
	test.Parallel()

	checked, reads := recordedPackages(test, []recordedSource{
		{path: "example.com/shapes", files: map[string]string{"/shapes/shapes.go": `package shapes

func Area() int { return 1 }
`}},
		{path: "example.com/q", imports: []string{"example.com/shapes"}, files: map[string]string{"/q/q.go": `package q

import . "example.com/shapes"

func Measured() int { return Area() }

func Other() int { return 2 }
`}}})
	census := reachRecorded(test, checked, reads, func(read *reachBuiltPackage) {
		if read.identifier == "example.com/q" {
			read.buildOnlyNames["Area"] = true
		}
	})
	got := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier == "example.com/q" {
			got[body.name] = cmp.Or(string(body.reason), "unreached") + " " + body.driver
		}
	}
	want := map[string]string{"Measured": "Shadowed example.com/shapes.Area shadowed in the body",
		"Other": "unreached "}
	if !maps.Equal(got, want) {
		test.Errorf("the census judges\n%v\nwant\n%v", got, want)
	}
}

// A declaration of a record whose file the host's file set does not hold is a doubt, never a body to leave alone: each
// is reached, Unknown, and called dependent, as the graph calls a place it knows no unit at (#w72zxtq).
func TestADeclarationWithNoFileIsReachedAndDependent(test *testing.T) {
	test.Parallel()

	checked, reads := recordedPackages(test, []recordedSource{{path: "example.com/q",
		files: map[string]string{"/q/q.go": `package q

func Plain() int { return 1 }
`}}})
	hosted := checked["example.com/q"]
	record, isRecorded := reads[hosted].FunctionUses()
	if !isRecorded {
		test.Fatal("q's uses are unrecorded")
	}
	host := newBodyReachHost(&hostDependence{decoded: map[*types.Package]bool{}}, reads, nil, nil)
	reacher := &bodyReacher{host: host, dependence: unchangedDependence(), carried: map[types.Object]bool{}}
	elsewhere := *hosted
	elsewhere.Fset = token.NewFileSet()
	bodies, absent := reacher.reachPackage(&elsewhere, record,
		reachBuiltPackages(&load.Loaded{Roots: []*packages.Package{hosted}}, map[string][]string{
			"example.com/q": hosted.GoFiles})[0])
	if len(bodies) != 1 || absent != 0 || bodies[0].reason != reachedUnknown || !bodies[0].unitDependent {
		test.Errorf("want Plain reached, Unknown, and dependent, got %+v and %d absent", bodies, absent)
	}
}

// A use read as a type argument reaches its body by either bit of its object, as a value's type does (#w72zxtq). No
// record holds a type argument alone: an explicit one is a type expression and an inferred one some argument's type,
// each recorded as ValueType in the same part, whose row reads the same bits first. So the row is held here, of an
// object of a package the graph does not hold, whose underlying bit alone the build sets.
func TestATypeArgumentReachesItsBodyByEitherBit(test *testing.T) {
	test.Parallel()

	elsewhere := types.NewPackage("example.com/elsewhere", "elsewhere")
	number := types.NewTypeName(token.NoPos, elsewhere, "Number", nil)
	types.NewNamed(number, types.Typ[types.Int], nil)
	graph := newReachBuiltGraph(&load.Loaded{})
	graph.hold("example.com/elsewhere", "example.com/elsewhere", builtFromSource)
	reacher := &bodyReacher{
		host: &bodyReachHost{dependence: &hostDependence{decoded: map[*types.Package]bool{}},
			identifiers: map[*types.Package]string{elsewhere: "example.com/elsewhere"}},
		dependence: build_dependence.NewGraph().Against(build_dependence.Difference{
			DependentObject: func(types.Object) (bool, bool) { return false, true }}),
		graph: graph,
	}
	reason, object, bits := reacher.useReason(build_reads.UsedEntry{Object: number}, build_reads.UsedTypeArgument)
	if reason != reachedTypeArgument || object != number || bits != "underlying" {
		test.Errorf("a type argument whose underlying bit is set reaches by %q, %v, %q, want TypeArgument by its "+
			"underlying bit", reason, object, bits)
	}
}

// A name one side alone declares at package level that a rule asks its package's scope about reaches every body of
// the package, ScopeName (#b255cfy), Plain among them though it names nothing: a universe name, copy, in a file the
// build alone compiles; fmt, which S1028 declares it asks, and fmt7, which stringintconv's fresh import names match,
// each in a file the build does not compile, a var with no value, so no body is absent. Other, a name no rule asks,
// reaches nothing.
func TestANameARuleAsksTheScopeAboutReachesItsPackageWhole(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		name      string
		declared  string
		buildOnly bool
		want      string
	}{
		{"a universe name the build alone declares", "copy", true,
			"ScopeName the package's scope, by copy, a universe name, declared by a file the build alone compiles"},
		{"a name a rule declares, the host's alone", "fmt", false, "ScopeName the package's scope, by fmt, which " +
			"cohere-go/S1028 asks of the scope, declared by a file the build does not compile"},
		{"a prefix's match, the host's alone", "fmt7", false, "ScopeName the package's scope, by fmt7, which " +
			"cohere-go/stringintconv asks of the scope, declared by a file the build does not compile"},
		{"a name no rule asks, the host's alone", "Other", false, "unreached "},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			files := map[string]string{"/q/q.go": "package q\n\nfunc Plain() int { return 1 }\n"}
			if !each.buildOnly {
				files["/q/host.go"] = "package q\n\nvar " + each.declared + " int\n"
			}
			checked, reads := recordedPackages(test, []recordedSource{{path: "example.com/q", files: files}})
			census := reachRecorded(test, checked, reads, func(read *reachBuiltPackage) {
				if each.buildOnly {
					read.buildOnlyNames[each.declared] = true
				} else {
					delete(read.files, "/q/host.go")
				}
			})
			got := map[string]string{}
			for _, body := range census.bodies {
				got[body.name] = cmp.Or(string(body.reason), "unreached") + " " + body.driver
			}
			if want := map[string]string{"Plain": each.want}; !maps.Equal(got, want) {
				test.Errorf("the census judges\n%v\nwant\n%v", got, want)
			}
		})
	}
}
