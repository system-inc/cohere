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
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// A use reaches its body by the load mode (#w72zxtq) when its object's package is one the host and the build load in
// different modes, one from export data and the other from source: export data holds an untyped float as a 512-bit
// binary fraction and a check from source holds it exactly, so at cohere's root, where the host reads math from the
// system cache and a build under `--build-dependence` checks it from source, goja's math.Sqrt2 / 2 type-checks to two
// values. The Square ruled the rule general: every use of any object of such a package reaches, of any kind, and only
// a package loaded alike reaches nothing. One the host does not hold has no mode to compare, and reaches, failing
// wide. The census still counts the narrower rule it
// replaced, an untyped float or complex constant read by its value alone. The engine's fixture cannot load a package
// two ways, since a test's host checks the standard library from source, so the rule is pinned here.
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
	cases := []struct {
		name            string
		object          types.Object
		kinds           build_reads.UseKinds
		hostExportData  bool
		buildFromSource bool
		general         bool
		constantsOnly   bool
	}{
		{"an untyped float, export data on the host and source in the build", sqrt2, byValue, true, true, true,
			true},
		{"an untyped float, source on the host and export data in the build", sqrt2, byValue, false, false, true,
			true},
		{"an untyped float, source in both", sqrt2, byValue, false, true, false, false},
		{"an untyped float, export data in both", sqrt2, byValue, true, false, false, false},
		{"an untyped complex, export data on the host and source in the build", imaginary, byValue, true, true, true,
			true},
		{"an untyped integer, export data on the host and source in the build", maximum, byValue, true, true, true,
			false},
		{"a typed float, export data on the host and source in the build", typed, byValue, true, true, true, false},
		{"a function, export data on the host and source in the build", function, named, true, true, true, false},
		{"a function, source in both", function, named, false, true, false, false},
		{"an untyped float of a package the host does not hold", elsewhere, byValue, true, true, true, true},
	}
	for _, each := range cases {
		reacher := &bodyReacher{
			host: &bodyReachHost{identifiers: map[*types.Package]string{math: "math"},
				dependence: &hostDependence{exportData: map[string]bool{"math": each.hostExportData}}},
			fromSource: map[string]bool{"math": each.buildFromSource},
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
// the host's walk does, by its path as its ID, so the reach's judgment can be held to records it really makes.
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
		decoded: map[*types.Package]bool{}}, reads, nil)
	hostFiles := map[string][]string{}
	var roots []*packages.Package
	fromSource := map[string]bool{}
	for identifier, each := range checked {
		hostFiles[identifier] = each.GoFiles
		fromSource[identifier] = true
		roots = append(roots, each)
	}
	built := reachBuiltPackages(&load.Loaded{Roots: roots}, hostFiles)
	if alter != nil {
		for _, read := range built {
			alter(read)
		}
	}
	return host.reach(unchangedDependence(), built, fromSource, false)
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
	fromSource := map[string]bool{"math": true, "example.com/q": true, "example.com/a": true}
	for _, mathExportData := range []bool{false, true} {
		reacher := &bodyReacher{host: newBodyReachHost(&hostDependence{
			exportData: map[string]bool{"math": mathExportData}, decoded: map[*types.Package]bool{}}, reads, nil),
			fromSource: fromSource, carried: map[types.Object]bool{}}
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
	host := newBodyReachHost(&hostDependence{decoded: map[*types.Package]bool{}}, reads, nil)
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
	reacher := &bodyReacher{
		host: &bodyReachHost{dependence: &hostDependence{decoded: map[*types.Package]bool{}},
			identifiers: map[*types.Package]string{elsewhere: "example.com/elsewhere"}},
		dependence: build_dependence.NewGraph().Against(build_dependence.Difference{
			DependentObject: func(types.Object) (bool, bool) { return false, true }}),
		fromSource: map[string]bool{"example.com/elsewhere": true},
	}
	reason, object, bits := reacher.useReason(build_reads.UsedEntry{Object: number}, build_reads.UsedTypeArgument)
	if reason != reachedTypeArgument || object != number || bits != "underlying" {
		test.Errorf("a type argument whose underlying bit is set reaches by %q, %v, %q, want TypeArgument by its "+
			"underlying bit", reason, object, bits)
	}
}
