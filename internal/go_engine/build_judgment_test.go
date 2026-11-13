package go_engine

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
)

// layoutDependency is the layout fixture's dependency, which stands for a package the host restored.
const layoutDependency = "example.com/layout/dependency"

// layoutRestored is the build_dependence layout fixture on darwin, its dependency loaded from source and its workspace
// checked again against the dependency read from export data, as the host checks its workspace against the standard
// library it restored from the binary's table: the workspace package, and the dependency.
func layoutRestored(test *testing.T) (*packages.Package, *packages.Package) {
	test.Helper()
	var environment []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if !slices.Contains([]string{"GOOS", "CGO_ENABLED", "GOFLAGS", "GOWORK"}, key) {
			environment = append(environment, pair)
		}
	}
	roots, loadError := packages.Load(&packages.Config{
		Mode: packages.LoadAllSyntax | packages.NeedTypesInfo,
		Dir:  filepath.Join("build_dependence", "testdata", "layout"),
		Env:  append(environment, "GOOS=darwin", "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS="),
	}, "./...")
	if loadError != nil {
		test.Fatal(loadError)
	}
	loaded := map[string]*packages.Package{}
	packages.Visit(roots, nil, func(visited *packages.Package) { loaded[visited.PkgPath] = visited })
	workspace, dependency := loaded["example.com/layout"], loaded[layoutDependency]
	if workspace == nil || dependency == nil || len(workspace.Errors)+len(dependency.Errors) > 0 {
		test.Fatalf("the layout fixture did not load whole: %v", loaded)
	}
	var exportData bytes.Buffer
	if writeError := gcexportdata.Write(&exportData, dependency.Fset, dependency.Types); writeError != nil {
		test.Fatal(writeError)
	}
	imported := map[string]*types.Package{}
	_, readError := gcexportdata.Read(&exportData, token.NewFileSet(), imported, layoutDependency)
	if readError != nil {
		test.Fatal(readError)
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
	configuration := types.Config{Importer: importerByPath(func(path string) (*types.Package, error) {
		if read, isRead := imported[path]; isRead {
			return read, nil
		}
		return nil, fmt.Errorf("the layout fixture's workspace imports %s, which no export data holds", path)
	})}
	checked, checkError := configuration.Check(workspace.PkgPath, workspace.Fset, workspace.Syntax, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	restored := &packages.Package{ID: workspace.ID, PkgPath: workspace.PkgPath, GoFiles: workspace.GoFiles,
		Fset: workspace.Fset, Syntax: workspace.Syntax, Types: checked, TypesInfo: information}
	return restored, dependency
}

// importerByPath is a function as a types.Importer.
type importerByPath func(path string) (*types.Package, error)

// Import is the function's answer for path.
func (importer importerByPath) Import(path string) (*types.Package, error) {
	return importer(path)
}

// The facts census follows a host unit's static call into a package the host restored through that package's own
// graph (#ss4t5g6): CallsWide, in the layout fixture's workspace, calls the dependency's Wide, read from export data,
// so the host's graph holds no unit of Wide. With Wide's fact differing between the builds, CallsWide, independent, is
// the one first-order caller the census counts in the workspace, found through Wide's counterpart in the dependency's
// graph.
func TestACallIntoARestoredPackageIsFollowedThroughItsGraph(test *testing.T) {
	test.Parallel()

	restored, dependency := layoutRestored(test)
	hostGraph := build_dependence.NewGraph()
	hostGraph.AddPackage(restored, true)
	dependencyGraph := build_dependence.NewGraph()
	dependencyGraph.AddPackage(dependency, false)
	nothingDiffers := build_dependence.Difference{}
	dependencyJudged := dependencyGraph.Against(nothingDiffers)
	judged := &buildJudgment{
		dependence:      hostGraph.Against(nothingDiffers),
		standardLibrary: dependencyJudged,
		counterparts:    build_dependence.NewCounterparts(dependencyGraph, dependencyJudged),
	}
	units, unitContaining := judged.census()

	wide := dependency.Types.Scope().Lookup("Wide")
	position := dependency.Fset.Position(wide.Pos())
	wideUnit, isUnit := unitContaining(dependency.ID, position.Filename, position.Offset)
	if !isUnit {
		test.Fatalf("no unit of the census holds Wide at %s", position)
	}
	var callers []int
	for index, unit := range units {
		if slices.Contains(unit.Calls, wideUnit) {
			callers = append(callers, index)
		}
	}
	if len(callers) != 1 || !units[callers[0]].Workspace || units[callers[0]].Dependent {
		test.Fatalf("want one independent workspace unit, CallsWide, calling Wide's unit, got the callers %v", callers)
	}

	workspace, dependencies := factCallers(units, map[int]bool{wideUnit: true})
	if workspace.FirstOrderUnits != 1 || workspace.FirstOrderNodes != units[callers[0]].Nodes {
		test.Errorf("want CallsWide the workspace's one first-order caller, got %+v", workspace)
	}
	if dependencies.DifferingUnits != 1 {
		test.Errorf("want Wide the one differing dependency unit, got %+v", dependencies)
	}
}
