package standing_presence

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// packageSyntax is one package's syntax and types as the rows read them: a build's pass's own, or the host's while
// its run still holds them (ReadBodies).
type packageSyntax struct {
	fileSet     *token.FileSet
	files       []*ast.File
	information *types.Info
	own         *types.Package
}

// syntaxOf is the pass's own syntax and types.
func syntaxOf(pass *analysis.Pass) packageSyntax {
	return packageSyntax{fileSet: pass.Fset, files: pass.Files, information: pass.TypesInfo, own: pass.Pkg}
}

// ReadBodies is the record of each function body of the package whose syntax and types these are that holds anything
// presence reads, by its place (fact_reach.Place), read by the same walks the rows read a build's own syntax by: the
// edges as testinggoroutine and requireparallel resolve them (testingGoroutineEdges, requireParallelEdges), a
// candidate as consistency-no-any's climb judges one (noAnyDeclarations), and a marker as emptyfunction reads one
// (markerName). A body that holds none of them has no entry.
func ReadBodies(
	fileSet *token.FileSet,
	files []*ast.File,
	information *types.Info,
	own *types.Package,
) map[fact_reach.Place]fact_reach.BodyRecord {
	syntax := packageSyntax{fileSet: fileSet, files: files, information: information, own: own}
	records := map[fact_reach.Place]fact_reach.BodyRecord{}
	declarations := functionDeclarations(syntax)
	addStarts := func(edges []presenceEdge, isGo bool) {
		for _, each := range edges {
			callee, isFunction := information.Defs[each.callee.Name].(*types.Func)
			if each.caller == nil || each.caller.Body == nil || !isFunction {
				continue
			}
			place := placeOf(syntax, each.caller)
			record := records[place]
			if isGo && !slices.Contains(record.GoStarts, callee) {
				record.GoStarts = append(record.GoStarts, callee)
			}
			if !isGo && !slices.Contains(record.RunStarts, callee) {
				record.RunStarts = append(record.RunStarts, callee)
			}
			records[place] = record
		}
	}
	addStarts(testingGoroutineEdges(syntax, declarations), true)
	addStarts(requireParallelEdges(syntax, declarations), false)
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			ownAny, markers := readBody(information, function.Body)
			if !ownAny && len(markers) == 0 {
				continue
			}
			place := placeOf(syntax, function)
			record := records[place]
			record.OwnAny = ownAny
			record.LocalMarkers = markers
			records[place] = record
		}
	}
	return records
}

// ReadPackage is what presence reads of the package read while its syntax and type information are whole: its bodies'
// records (ReadBodies) and its marker interfaces' method names (ReadMarkers). The host's run takes it as its presence
// reader (check.Options.FactReachPresence), so the engine's core never imports this package.
func ReadPackage(read *packages.Package) (map[fact_reach.Place]fact_reach.BodyRecord, []string) {
	return ReadBodies(read.Fset, read.Syntax, read.TypesInfo, read.Types), ReadMarkers(read.Syntax, read.TypesInfo)
}

// readBody is whether body writes a candidate of consistency-no-any's of its own, an any or an interface{} the rule's
// climb judges a candidate (spellsAny, noAnyDeclarations), which within a body ends at a local variable's type, a
// function literal's signature or a local type's; and the method names of the marker interfaces body declares, sorted
// and each once.
func readBody(information *types.Info, body *ast.BlockStmt) (bool, []string) {
	ownAny := false
	var markers []string
	// The ancestors of the node being heard, from the body down. A climb from inside the body ends inside it, at a
	// declaration or at the body's own statements, where it judges nothing, so no ancestor above the body is read.
	stack := []ast.Node{}
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if !ownAny && spellsAny(information, node) {
			_, _, ownAny = noAnyDeclarations(information, node, stack)
		}
		if specification, isTypeSpecification := node.(*ast.TypeSpec); isTypeSpecification {
			if name, isMarker := markerName(information, specification); isMarker && !slices.Contains(markers, name) {
				markers = append(markers, name)
			}
		}
		stack = append(stack, node)
		return true
	})
	slices.Sort(markers)
	return ownAny, markers
}
