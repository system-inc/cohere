package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// The kept fixture's paths: shapes declares a struct with fields and a method, and two functions, and its user reads
// each of them, so its reads hold several uses and several selections.
const (
	keptShapesPath = "example.com/kept/shapes"
	keptUserPath   = "example.com/kept/user"
)

// keptSources are shapes and its user, by path.
var keptSources = map[string]string{
	keptShapesPath: "package shapes\n\ntype Shape struct {\n\tSides int\n\tName  string\n}\n\n" +
		"func (Shape) Area() int { return 0 }\n\nfunc New() *Shape { return &Shape{} }\n\n" +
		"func Count() int { return 1 }\n",
	keptUserPath: "package user\n\nimport \"example.com/kept/shapes\"\n\n" +
		"func Use() int {\n\tshape := shapes.New()\n" +
		"\treturn shape.Sides + shape.Area() + len(shape.Name) + shapes.Count()\n}\n",
}

// keptWorld is the kept fixture on the host and in a build that declares what the host does: the user's reads on the
// host, of all four kinds, the packages the build reaches by path, and the build's facts.
type keptWorld struct {
	restoring Restoring
	closure   map[string]*types.Package
	fact      FactLookup
}

// checkedKeptWorld checks the fixture twice, once as the host and once as a build of the same sources, records the
// user's type reads on the host from its syntax, and adds a fact read that hit, one that missed, two Scope lookups and
// a Deprecated one, each of which the build answers as the host did, so every verdict ranges over every read.
func checkedKeptWorld(test *testing.T) keptWorld {
	test.Helper()

	fileSet := token.NewFileSet()
	parse := func(path string) *ast.File {
		parsed, parseError := parser.ParseFile(fileSet, path+".go", keptSources[path], parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		return parsed
	}
	checkShapes := func() *types.Package {
		checked, checkError := (&types.Config{}).Check(keptShapesPath, fileSet, []*ast.File{parse(keptShapesPath)},
			nil)
		if checkError != nil {
			test.Fatal(checkError)
		}
		return checked
	}
	hostShapes, buildShapes := checkShapes(), checkShapes()
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == keptShapesPath {
			return hostShapes, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	userFiles := []*ast.File{parse(keptUserPath)}
	hostUser, userError := configuration.Check(keptUserPath, fileSet, userFiles, info)
	if userError != nil {
		test.Fatal(userError)
	}
	valueFactType := reflect.TypeFor[*valueFact]()
	reads := &build_reads.Reads{}
	reads.RecordTypes(hostUser, info, userFiles)
	reads.AddFacts([]build_reads.Fact{
		{Object: hostShapes.Scope().Lookup("New"), Package: hostShapes, FactType: valueFactType, Found: true,
			Value: &valueFact{Count: 1}},
		{Object: hostShapes.Scope().Lookup("Count"), Package: hostShapes, FactType: valueFactType},
	})
	reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryScope, Path: keptShapesPath, Name: "Shape", Found: true,
		Answer: hostShapes.Scope().Lookup("Shape")})
	reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryScope, Path: keptShapesPath, Name: "Missing"})
	reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: keptShapesPath,
		Subject: hostShapes.Scope().Lookup("Shape")})
	buildFacts := map[string]analysis.Fact{
		factKeyOf(keptShapesPath, "New", valueFactType): &valueFact{Count: 1},
	}
	return keptWorld{
		restoring: Restoring{Path: keptUserPath, Imports: []string{keptShapesPath}, Reads: reads},
		closure:   map[string]*types.Package{keptShapesPath: buildShapes},
		fact: func(owner string, path objectpath.Path, factType reflect.Type) (analysis.Fact, bool) {
			fact, found := buildFacts[factKeyOf(owner, path, factType)]
			return fact, found
		},
	}
}

// No verdict writes through the lists the reads keep: the reads hand every caller the same sorted slice
// (build_reads.Reads.Uses), which the verdicts of every extra build range over, side by side, so one that sorted it,
// appended to it or wrote an element would change what every other reads. Over reads of all four kinds, each list
// after TypesHold, ReadFactTypes and FactsHold of every fact type read is the slice it was, element for element, and
// each is as long as its capacity, so an append could not reach the kept array either.
func TestNoVerdictWritesThroughTheReadsKeptLists(test *testing.T) {
	test.Parallel()

	world := checkedKeptWorld(test)
	reads := world.restoring.Reads
	uses := listBeforeVerdicts(test, "uses", reads.Uses())
	selections := listBeforeVerdicts(test, "selections", reads.Selections())
	// A selection's path is a slice of its own, which a clone of the list would share with it.
	for index := range selections.before {
		selections.before[index].Index = slices.Clone(selections.before[index].Index)
	}
	facts := listBeforeVerdicts(test, "facts", reads.Facts())
	lookups := listBeforeVerdicts(test, "lookups", reads.Lookups())

	host, build := NewReadsBuild(), NewReadsBuild()
	if holds, why := TypesHold(world.restoring, world.closure, nil, host, build); !holds {
		test.Fatalf("the type reads do not hold, so the verdict stopped before its last read: %s", why)
	}
	factTypes := ReadFactTypes(world.restoring)
	if !factTypes[reflect.TypeFor[*valueFact]()] || !factTypes[deprecatedFactType] {
		test.Fatalf("the reads asked the fact types %v, want the value fact's and the deprecated fact's", factTypes)
	}
	if holds, why := FactsHold(world.restoring, closureLookupOf(world.closure), host, build, world.fact,
		factTypes); !holds {
		test.Fatalf("the fact reads do not hold, so the verdict stopped before its last read: %s", why)
	}

	same := func(left, right build_reads.Selection) bool {
		return left.Receiver == right.Receiver && left.Name == right.Name &&
			slices.Equal(left.Index, right.Index) && left.Selected == right.Selected
	}
	uses.requireUnwritten(test, reads.Uses(), func(left, right build_reads.ObjectUse) bool { return left == right })
	selections.requireUnwritten(test, reads.Selections(), same)
	facts.requireUnwritten(test, reads.Facts(), func(left, right build_reads.Fact) bool { return left == right })
	lookups.requireUnwritten(test, reads.Lookups(), func(left, right build_reads.Lookup) bool { return left == right })
}

// verdictList is one of the reads' kept lists as it stood before any verdict ran: kept, the slice the reads handed
// out, and before, a copy of its elements.
type verdictList[Read any] struct {
	kind   string
	kept   []Read
	before []Read
}

// listBeforeVerdicts is kept, the reads' list of kind, with a copy of its elements, once it holds two reads or more,
// so a write that reordered it would show, and is as long as its capacity.
func listBeforeVerdicts[Read any](test *testing.T, kind string, kept []Read) verdictList[Read] {
	test.Helper()

	if len(kept) < 2 {
		test.Fatalf("the reads hold %d %s, want two or more, so the fixture tells nothing", len(kept), kind)
	}
	if cap(kept) != len(kept) {
		test.Fatalf("the kept %s have capacity %d past their length %d, so an append would write into the kept array",
			kind, cap(kept), len(kept))
	}
	return verdictList[Read]{kind: kind, kept: kept, before: slices.Clone(kept)}
}

// requireUnwritten requires again, the list asked after the verdicts, to be the slice the reads handed out before
// them, and its elements those copied then, each compared by same.
func (list verdictList[Read]) requireUnwritten(test *testing.T, again []Read, same func(left, right Read) bool) {
	test.Helper()

	if len(again) != len(list.kept) || &again[0] != &list.kept[0] {
		test.Errorf("the %s asked after the verdicts are another slice, want the one the reads kept", list.kind)
	}
	if !slices.EqualFunc(list.kept, list.before, same) {
		test.Errorf("a verdict wrote through the kept %s: want\n%v\ngot\n%v", list.kind, list.before, list.kept)
	}
}
