package guard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// No rule writes the TypeScript syntax tree, and this is what makes that structural rather than remembered.
//
// The tree is shared with every checker. While a rule's listener runs on one goroutine, a checker type-checking
// another file on another goroutine reads the same nodes, so a write of any kind is a data race, a temporary one
// put back after the call too. no-dupe-class-members once swapped a class's member list for one without its
// computed members for the length of its listener, on the reasoning that the walk is single-threaded per file,
// and a checker that resolved the class during the swap kept it without its `*[Symbol.iterator]()` for the run:
// false TS2488 in 2 of 60 cold runs of loom (#72kn826). This holds every rule to computing on a copy (#qb5xxj1).
//
// It reads every package under internal/lint from source with its types: the rules, the rule interface, and
// every shelf a rule calls with a node (checking, ecmascript and the rest). A helper is scanned where it is
// written, so a write in a helper a rule calls is named at the helper, whoever calls it. What it reports:
//
//   - an assignment, op-assignment, `++` or `--`, or a range clause assigning, whose target is reached through a
//     field of a struct the ast package declares (`node.Members = ...`, `list.Nodes[i] = ...`, `symbol.Flags |=`),
//     or is an element or target whose type is the tree's (`nodes[i] = ...` over a `[]*ast.Node`, `*list = ...`
//     through a `*ast.NodeList`, an entry of an `ast.SymbolTable`);
//   - the address of such a place, taken explicitly or by calling a pointer method on it, since whoever holds the
//     address can write it: the old swap took `&declaration.Members` and wrote through a `**ast.NodeList`, which
//     reads like a write to any variable, so it is the address that is named. The address of one of the ast
//     package's structs (`&statement.Node`) is the node itself, written only through its fields, and is not;
//   - copy into, append onto, or an in-place sort, reversal, deletion or insertion of such a slice;
//   - a call to one of the ast package's writing methods or functions (syntaxTreeWriters).
//
// A write is the rule's own, and not reported, when its storage is a local variable's: either the variable itself
// with no pointer, slice or map followed on the way (a value copy), or, for a variable that only ever holds what
// the function built (fresh: a composite literal, make, new, append onto a fresh value, slices.Clone, a node
// factory's New, a call to a function of the package that only ever returns what it built), the one allocation it
// holds and nothing reached through that. A parameter is never fresh, since the caller decides what it holds, and
// neither is a variable whose address is taken, since what the address is handed to decides.
//
// Known limits: a call through a function value or an interface is not followed, and is only seen where the
// function it reaches is written in internal/lint; a write inside the TypeScript packages a rule calls (the
// checker's own caches) is the checker's to synchronize and is not looked at; a fresh allocation's field that is
// handed a slice the tree owns, then written through, reads as the rule's own; and freshness is not followed into
// a parameter, so a helper writing a slice every caller built is reported until it is handed a copy.
// TestEveryRuleWalksBesideReadersOfTheSameTreeAtOnce (internal/types/program) runs every rule beside a reader of
// the tree under -race, which catches a write wherever it is written.
func TestRulesNeverWriteTheSyntaxTree(test *testing.T) {
	test.Parallel()

	root, absolutePathError := filepath.Abs("../..")
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	scan := syntaxTreeWriteScan{root: root}
	for _, lintPackage := range lintPackages(test, guardOverlay(test)) {
		if len(lintPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", lintPackage.ImportPath, lintPackage.Errors[0])
		}
		scan.read(lintPackage.FileSet, lintPackage.TypesInfo, lintPackage.Syntax)
	}
	// About 650 files under internal/lint name the tree today. A scan that saw a handful lost the packages, or
	// stopped resolving the ast package's types, and would pass on nothing.
	if scan.treeFiles < 300 {
		test.Fatalf("the scan read %d files, %d of them naming the syntax tree's types, too few to be every rule",
			scan.files, scan.treeFiles)
	}
	for _, site := range scan.writes {
		test.Errorf("%s: the syntax tree is shared with every checker, which reads it on other goroutines while "+
			"the rules run, so compute on a copy and leave the tree as the parser made it (#72kn826)", site)
	}
}

// The scan's positive control: a fixture holding the shapes of tree write it reports, each marked with what it must
// say, among writes to the function's own copies it must not report. Type-checked against the same ast package the
// rules are, so it cannot pass by resolving nothing.
func TestTheSyntaxTreeWriteScanNamesTheFixturesWrites(test *testing.T) {
	test.Parallel()

	root, absolutePathError := filepath.Abs("../..")
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	paths, globError := filepath.Glob(filepath.Join("testdata", "syntax_tree_writes", "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(paths) == 0 {
		test.Fatal("no fixture under testdata/syntax_tree_writes, so the scan was checked against nothing")
	}

	imported := map[string]*types.Package{}
	for _, lintPackage := range lintPackages(test, guardOverlay(test)) {
		collectImports(lintPackage.Types, imported)
	}
	fileSet := token.NewFileSet()
	for _, path := range paths {
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		file, parseError := parser.ParseFile(fileSet, path, source, parser.ParseComments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Implicits:  map[ast.Node]types.Object{},
		}
		configuration := types.Config{Importer: packageImporter(imported)}
		if _, checkError := configuration.Check("fixture", fileSet, []*ast.File{file}, info); checkError != nil {
			test.Fatalf("type-checking %s: %v", path, checkError)
		}

		wanted := fixtureWants(source)
		if len(wanted) == 0 {
			test.Fatalf("%s marks no write the scan must name, so it proves nothing", path)
		}
		scan := syntaxTreeWriteScan{root: root}
		scan.read(fileSet, info, []*ast.File{file})
		if scan.treeFiles != 1 {
			test.Fatalf("%s did not read as naming the syntax tree's types", path)
		}
		found := map[int]string{}
		for _, site := range scan.writes {
			line, description := siteLine(test, site)
			found[line] = description
		}
		for line, want := range wanted {
			if found[line] != want {
				test.Errorf("%s:%d: the scan said %q, want %q", path, line, found[line], want)
			}
		}
		for line, description := range found {
			if _, marked := wanted[line]; !marked {
				test.Errorf("%s:%d: the scan named %q where the fixture writes only its own copy", path, line,
					description)
			}
		}
	}
}

// fixtureWant is a fixture line's `// want: ...` mark, what the scan must say of the line.
var fixtureWant = regexp.MustCompile(`// want: (.+)$`)

// fixtureWants is every marked line of a fixture, by line number, with what the scan must say of it.
func fixtureWants(source []byte) map[int]string {
	wanted := map[int]string{}
	for index, line := range strings.Split(string(source), "\n") {
		if match := fixtureWant.FindStringSubmatch(line); match != nil {
			wanted[index+1] = match[1]
		}
	}
	return wanted
}

// siteLine splits a reported site, `path:line what`, into its line and what it says.
func siteLine(test *testing.T, site string) (int, string) {
	test.Helper()
	location, description, found := strings.Cut(site, " ")
	if !found {
		test.Fatalf("the site %q names no write", site)
	}
	line, parseError := strconv.Atoi(location[strings.LastIndex(location, ":")+1:])
	if parseError != nil {
		test.Fatalf("the site %q names no line: %v", site, parseError)
	}
	return line, description
}

// collectImports adds every package imported, transitively, by imported's own imports.
func collectImports(from *types.Package, imported map[string]*types.Package) {
	if from == nil {
		return
	}
	for _, dependency := range from.Imports() {
		if imported[dependency.Path()] == nil {
			imported[dependency.Path()] = dependency
			collectImports(dependency, imported)
		}
	}
}

// packageImporter imports from packages already type-checked, by path.
type packageImporter map[string]*types.Package

func (importer packageImporter) Import(path string) (*types.Package, error) {
	if imported := importer[path]; imported != nil {
		return imported, nil
	}
	return nil, fmt.Errorf("no package %s was loaded with the rule packages", path)
}

// syntaxTreePackages are the import paths of typescript-go's ast package: its own, where every node type and
// every struct the tree holds is declared, and the shim cohere imports it through, which declares only aliases.
var syntaxTreePackages = map[string]bool{
	"github.com/microsoft/TypeScript/tsc/internal/ast": true,
	"github.com/microsoft/TypeScript/tsc/shim/ast":     true,
}

// syntaxTreeWriters are the ast package's methods and functions that write what they are handed, by name, found by
// reading the package for every one that assigns a field of its receiver or argument without a lock of its own
// (#qb5xxj1). The lazy getters that fill a cache under a once or a mutex (GetDeclarationMap, GetNameTable,
// ECMALineMap, GetOrCreateToken and the like) are synchronized by the package itself and not here. AsMutable is the
// door to every MutableNode setter, and a rule has no reason to open it.
var syntaxTreeWriters = map[string]bool{
	"AsMutable":                  true,
	"SetBindDiagnostics":         true,
	"SetCategory":                true,
	"SetContentMapperInfo":       true,
	"SetDiagnostics":             true,
	"SetExpression":              true,
	"SetExternalData":            true,
	"SetExternalModuleIndicator": true,
	"SetFile":                    true,
	"SetHasLazyJSDoc":            true,
	"SetImportsOfSourceFile":     true,
	"SetInitializer":             true,
	"SetJSDiagnostics":           true,
	"SetJSDocCache":              true,
	"SetJSDocDiagnostics":        true,
	"SetLocation":                true,
	"SetMessageChain":            true,
	"SetModifiers":               true,
	"SetParentInChildren":        true,
	"SetParseJSDocForNode":       true,
	"SetRelatedInfo":             true,
	"SetRepopulateInfo":          true,
	"SetSkippedOnNoEmit":         true,
	"SetType":                    true,
}

// inPlaceSliceFunctions are the standard library's functions that write the slice they are handed as their first
// argument, by package path and name.
var inPlaceSliceFunctions = map[string]bool{
	"slices.Compact":        true,
	"slices.CompactFunc":    true,
	"slices.Delete":         true,
	"slices.DeleteFunc":     true,
	"slices.Insert":         true,
	"slices.Replace":        true,
	"slices.Reverse":        true,
	"slices.Sort":           true,
	"slices.SortFunc":       true,
	"slices.SortStableFunc": true,
	"sort.Slice":            true,
	"sort.SliceStable":      true,
}

// freshFunctions are the standard library's functions that return a new slice or map, by package path and name.
var freshFunctions = map[string]bool{"maps.Clone": true, "slices.Clone": true}

// syntaxTreeWriteScan reads packages for writes into the syntax tree, and keeps what it found.
type syntaxTreeWriteScan struct {
	// root is the module's directory, which every site is named relative to.
	root string

	files     int
	treeFiles int

	// writes is every site found, `path:line what`, sorted.
	writes []string
}

// syntaxTreeWriteReader reads one package's files.
type syntaxTreeWriteReader struct {
	scan    *syntaxTreeWriteScan
	fileSet *token.FileSet
	info    *types.Info

	// fresh are the package's local variables that only ever hold what their function built, and freshResults its
	// functions of one result that only ever return what they built (freshness).
	fresh        map[*types.Var]bool
	freshResults map[*types.Func]bool
}

func (scan *syntaxTreeWriteScan) read(fileSet *token.FileSet, info *types.Info, files []*ast.File) {
	reader := &syntaxTreeWriteReader{scan: scan, fileSet: fileSet, info: info}
	reader.freshness(files)
	for _, file := range files {
		scan.files++
		for _, imported := range file.Imports {
			if path, unquoteError := strconv.Unquote(imported.Path.Value); unquoteError == nil &&
				syntaxTreePackages[path] {
				scan.treeFiles++
				break
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			reader.inspect(node)
			return true
		})
	}
	sort.Strings(scan.writes)
}

func (reader *syntaxTreeWriteReader) inspect(node ast.Node) {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		if typed.Tok != token.DEFINE {
			for _, target := range typed.Lhs {
				reader.target(target, "writes")
			}
		}
	case *ast.IncDecStmt:
		reader.target(typed.X, "writes")
	case *ast.RangeStmt:
		if typed.Tok == token.ASSIGN {
			for _, target := range []ast.Expr{typed.Key, typed.Value} {
				if target != nil {
					reader.target(target, "writes")
				}
			}
		}
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			reader.addressTarget(typed.X)
		}
	case *ast.CallExpr:
		reader.call(typed)
	}
}

// accessStep is one step of the path from a variable to the place a write lands, outermost first.
type accessStep struct {
	// label is what the step reaches: `Type.field`, `an element`, `an entry`, `the target`.
	label string

	// treeField is the step's `Type.Field` when it selects a field of a struct the ast package declares.
	treeField string

	// treeElement says what the step reaches when it is an element, entry or target of the tree's own type.
	treeElement string

	// indirect is a step that follows a pointer, a slice or a map, so what it reaches is not the storage of
	// what it started from.
	indirect bool
}

// accessPath is the steps from target's root to target, outermost first, and the root: the expression it starts
// from, an identifier when the path starts at a variable.
func (reader *syntaxTreeWriteReader) accessPath(target ast.Expr) (ast.Expr, []accessStep) {
	var steps []accessStep
	expression := target
	for {
		switch typed := expression.(type) {
		case *ast.ParenExpr:
			expression = typed.X
			continue
		case *ast.SelectorExpr:
			selection := reader.info.Selections[typed]
			if selection == nil || selection.Kind() != types.FieldVal {
				return expression, steps
			}
			field := selection.Obj()
			step := accessStep{label: typeName(selection.Recv()) + "." + field.Name(), indirect: selection.Indirect()}
			if field.Pkg() != nil && syntaxTreePackages[field.Pkg().Path()] {
				step.treeField = step.label
			}
			steps = append(steps, step)
			expression = typed.X
			continue
		case *ast.IndexExpr:
			container := reader.info.TypeOf(typed.X)
			if container == nil {
				return expression, steps
			}
			step, isElement := elementStep(container)
			if !isElement {
				return expression, steps
			}
			steps = append(steps, step)
			expression = typed.X
			continue
		case *ast.SliceExpr:
			container := reader.info.TypeOf(typed.X)
			if container == nil {
				return expression, steps
			}
			_, isArray := container.Underlying().(*types.Array)
			steps = append(steps, accessStep{label: "a slice", indirect: !isArray})
			expression = typed.X
			continue
		case *ast.StarExpr:
			pointee := reader.info.TypeOf(typed)
			if pointee == nil {
				return expression, steps
			}
			// A pointer to a pointer, slice or map points into the tree only at an address taken of the tree's
			// field, which is reported where it is taken; a pointer to the tree's own struct is a node, a list.
			step := accessStep{label: "the target", indirect: true}
			if isSyntaxTreeType(pointee) {
				step.treeElement = "the " + typeString(pointee) + " a " + typeString(reader.info.TypeOf(typed.X)) +
					" points to"
			}
			steps = append(steps, step)
			expression = typed.X
			continue
		}
		return expression, steps
	}
}

// elementStep is the step an index into container takes, and false when container is not indexed for storage (a
// string, or a generic function's instantiation).
func elementStep(container types.Type) (accessStep, bool) {
	switch typed := container.Underlying().(type) {
	case *types.Slice:
		step := accessStep{label: "an element", indirect: true}
		if namesTheSyntaxTree(typed.Elem(), 0) {
			step.treeElement = "an element of a " + typeString(container)
		}
		return step, true
	case *types.Array:
		step := accessStep{label: "an element"}
		if namesTheSyntaxTree(typed.Elem(), 0) {
			step.treeElement = "an element of a " + typeString(container)
		}
		return step, true
	case *types.Pointer:
		array, isArray := typed.Elem().Underlying().(*types.Array)
		if !isArray {
			return accessStep{}, false
		}
		step := accessStep{label: "an element", indirect: true}
		if namesTheSyntaxTree(array.Elem(), 0) {
			step.treeElement = "an element of a " + typeString(container)
		}
		return step, true
	case *types.Map:
		// A map is the tree's only as a type the ast package declares (ast.SymbolTable): an entry written into a
		// map the rule keeps of nodes writes the rule's map, not the tree.
		step := accessStep{label: "an entry", indirect: true}
		if isSyntaxTreeType(container) {
			step.treeElement = "an entry of an " + typeString(container)
		}
		return step, true
	}
	return accessStep{}, false
}

// target reports target, a place written or whose address is taken, when the place is the tree's.
func (reader *syntaxTreeWriteReader) target(target ast.Expr, verb string) {
	root, steps := reader.accessPath(target)
	reader.report(target.Pos(), verb, root, steps)
}

// addressTarget reports a place whose address is taken, when the place is the tree's. A place that is itself one of
// the ast package's structs is left alone: `&statement.Node` is the node every listener is handed, and a write
// through it selects a field of the tree, which is reported where it is written. What else the tree holds (a list
// pointer, a slice, flags, a range) has no field to be written through, so its address is the write's door.
func (reader *syntaxTreeWriteReader) addressTarget(place ast.Expr) {
	if placeType := reader.info.TypeOf(place); placeType != nil && isSyntaxTreeType(placeType) {
		if _, isStruct := placeType.Underlying().(*types.Struct); isStruct {
			return
		}
	}
	reader.target(place, "takes the address of")
}

// report records the place steps reach from root, under verb, unless it is not the tree's or is the function's own.
func (reader *syntaxTreeWriteReader) report(position token.Pos, verb string, root ast.Expr, steps []accessStep) {
	description := describeSteps(steps)
	if description == "" || reader.ownStorage(root, steps) {
		return
	}
	reader.record(position, verb+" "+description)
}

func (reader *syntaxTreeWriteReader) record(position token.Pos, what string) {
	location := reader.fileSet.Position(position)
	path := location.Filename
	if relative, relativeError := filepath.Rel(reader.scan.root, path); relativeError == nil {
		path = filepath.ToSlash(relative)
	}
	reader.scan.writes = append(reader.scan.writes, path+":"+strconv.Itoa(location.Line)+" "+what)
}

// describeSteps says what a path reaches of the tree, and nothing when it reaches nothing of it: the outermost field
// of a struct the ast package declares, what of it is reached past that, or else the tree-typed element or target.
func describeSteps(steps []accessStep) string {
	for index, step := range steps {
		if step.treeField == "" {
			continue
		}
		if index > 0 {
			return steps[0].label + " of " + step.treeField
		}
		return step.treeField
	}
	for _, step := range steps {
		if step.treeElement != "" {
			return step.treeElement
		}
	}
	return ""
}

// ownStorage reports a path whose storage is the function's own: a local variable's, reached with no pointer, slice
// or map followed, or the one allocation a fresh variable holds.
func (reader *syntaxTreeWriteReader) ownStorage(root ast.Expr, steps []accessStep) bool {
	identifier, isIdentifier := root.(*ast.Ident)
	if !isIdentifier {
		return false
	}
	variable := reader.localVariable(identifier)
	if variable == nil {
		return false
	}
	indirections := 0
	for _, step := range steps {
		if step.indirect {
			indirections++
		}
	}
	if indirections == 0 {
		return true
	}
	if indirections > 1 || !reader.fresh[variable] {
		return false
	}
	// The one indirection a fresh variable's own allocation takes is the first step from it, through the pointer,
	// slice or map it holds.
	switch variable.Type().Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map:
		return steps[len(steps)-1].indirect
	}
	return false
}

// localVariable is the variable identifier names when it is one declared inside a function, and nil otherwise.
func (reader *syntaxTreeWriteReader) localVariable(identifier *ast.Ident) *types.Var {
	variable, isVariable := reader.info.ObjectOf(identifier).(*types.Var)
	if !isVariable || variable.IsField() || variable.Pkg() == nil || variable.Parent() == nil ||
		variable.Parent() == variable.Pkg().Scope() {
		return nil
	}
	return variable
}

// call reports a call that writes the tree: one of the ast package's writers, a builtin or standard function that
// writes a tree slice, or a pointer method called on a place of the tree, which takes its address.
func (reader *syntaxTreeWriteReader) call(call *ast.CallExpr) {
	callee := reader.callee(call)
	switch typed := callee.(type) {
	case *types.Builtin:
		if len(call.Args) == 0 {
			return
		}
		switch typed.Name() {
		case "copy":
			reader.sliceWrite(call.Args[0], "copies into", true)
		case "append":
			// Appending onto a slice the function was handed is the common case and writes past its length, which
			// no reader of the tree looks at; onto a field of the tree it writes the tree's spare capacity, and
			// onto a slice of one (`list.Nodes[:i]`) it writes the elements after i.
			reader.sliceWrite(call.Args[0], "appends onto", false)
		}
		return
	case *types.Func:
		function := typed
		if function.Pkg() == nil {
			return
		}
		signature, isSignature := function.Type().(*types.Signature)
		if !isSignature {
			return
		}
		qualified := function.Pkg().Path() + "." + function.Name()
		if inPlaceSliceFunctions[qualified] && len(call.Args) > 0 {
			reader.sliceWrite(call.Args[0], "writes in place, by "+function.Pkg().Name()+"."+function.Name()+",",
				true)
			return
		}
		if syntaxTreePackages[function.Pkg().Path()] && syntaxTreeWriters[function.Name()] {
			reader.writerCall(call, function, signature)
			return
		}
		if signature.Recv() != nil {
			reader.pointerMethodCall(call)
		}
	}
}

// callee is the function or builtin call calls, and nil for a conversion or a call through a function value.
func (reader *syntaxTreeWriteReader) callee(call *ast.CallExpr) types.Object {
	switch function := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return reader.info.Uses[function]
	case *ast.SelectorExpr:
		return reader.info.Uses[function.Sel]
	case *ast.IndexExpr:
		// An explicit instantiation, `slices.SortFunc[...]`.
		switch generic := ast.Unparen(function.X).(type) {
		case *ast.Ident:
			return reader.info.Uses[generic]
		case *ast.SelectorExpr:
			return reader.info.Uses[generic.Sel]
		}
	}
	return nil
}

// sliceWrite reports a call that writes slice's storage, when it is the tree's. With elements set the call writes
// slice's elements, the tree's when slice is reached through a field of the tree or holds the tree's type and is not
// a slice the function built. Without it the call writes past what slice holds, the tree's only when slice is
// reached through a field of the tree: `kept.Nodes = append(kept.Nodes, member)` onto a fresh list is the rule's
// own.
func (reader *syntaxTreeWriteReader) sliceWrite(slice ast.Expr, verb string, elements bool) {
	root, steps := reader.accessPath(slice)
	sliceType := reader.info.TypeOf(slice)
	if sliceType == nil {
		return
	}
	if elements {
		step := accessStep{label: "an element", indirect: true}
		if element, isSlice := sliceType.Underlying().(*types.Slice); isSlice && namesTheSyntaxTree(element.Elem(), 0) {
			step.treeElement = "a " + typeString(sliceType)
		}
		steps = append([]accessStep{step}, steps...)
	}
	reader.report(slice.Pos(), verb, root, steps)
}

// writerCall reports a call to one of the ast package's writers, unless what it writes is the function's own: a
// method's receiver, or a function's first pointer argument. A writer handed no pointer writes the package's own
// state, which every checker reads.
func (reader *syntaxTreeWriteReader) writerCall(call *ast.CallExpr, function *types.Func, signature *types.Signature) {
	name := function.Name()
	var written ast.Expr
	var writtenType types.Type
	if receiver := signature.Recv(); receiver != nil {
		name = typeName(receiver.Type()) + "." + name
		if selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSelector {
			written, writtenType = selector.X, reader.info.TypeOf(selector.X)
		}
	} else {
		name = "ast." + name
		for index := 0; index < signature.Params().Len() && index < len(call.Args); index++ {
			if _, isPointer := signature.Params().At(index).Type().Underlying().(*types.Pointer); isPointer {
				written, writtenType = call.Args[index], reader.info.TypeOf(call.Args[index])
				break
			}
		}
	}
	if written == nil || writtenType == nil {
		reader.record(call.Pos(), "calls "+name+", which writes the ast package's own state")
		return
	}
	root, steps := reader.accessPath(written)
	_, isPointer := writtenType.Underlying().(*types.Pointer)
	writtenStep := accessStep{label: "the target", treeElement: "the " + typeString(writtenType), indirect: isPointer}
	steps = append([]accessStep{writtenStep}, steps...)
	if reader.ownStorage(root, steps) {
		return
	}
	reader.record(call.Pos(), "calls "+name+", which writes "+describeSteps(steps))
}

// pointerMethodCall reports a pointer method called on an addressable place of the tree, which takes the place's
// address as a write would: `node.Loc.Set(...)` were TextRange's methods pointer ones.
func (reader *syntaxTreeWriteReader) pointerMethodCall(call *ast.CallExpr) {
	if place, isPlace := reader.pointerMethodReceiver(call); isPlace {
		reader.addressTarget(place)
	}
}

// pointerMethodReceiver is the place a call takes the address of to call a pointer method on it, a value rather
// than a pointer, and false for any other call.
func (reader *syntaxTreeWriteReader) pointerMethodReceiver(call *ast.CallExpr) (ast.Expr, bool) {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return nil, false
	}
	selection := reader.info.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil, false
	}
	signature, isSignature := selection.Obj().Type().(*types.Signature)
	if !isSignature || signature.Recv() == nil {
		return nil, false
	}
	if _, pointerReceiver := signature.Recv().Type().Underlying().(*types.Pointer); !pointerReceiver {
		return nil, false
	}
	receiverType := reader.info.TypeOf(selector.X)
	if receiverType == nil {
		return nil, false
	}
	if _, isPointer := receiverType.Underlying().(*types.Pointer); isPointer {
		return nil, false
	}
	return selector.X, true
}

// freshness finds the package's local variables that only ever hold what their function built, and its functions
// that only ever return what they built, to a fixed point: a variable assigned from another, or from a call, holds
// what that one does, and a function returning a variable returns what the variable holds. A variable whose address
// is taken, by `&` or a pointer method called on it, is not fresh, since what the address is handed to may store
// anything there.
func (reader *syntaxTreeWriteReader) freshness(files []*ast.File) {
	values := map[*types.Var][]ast.Expr{}
	stale := map[*types.Var]bool{}
	returns := map[*types.Func][]*ast.ReturnStmt{}
	markFields := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := reader.info.Defs[name].(*types.Var); isVariable {
					stale[variable] = true
				}
			}
		}
	}
	assign := func(identifier *ast.Ident, value ast.Expr, known bool) {
		variable := reader.localVariable(identifier)
		if variable == nil {
			return
		}
		if !known {
			stale[variable] = true
			return
		}
		values[variable] = append(values[variable], value)
	}
	// addressed marks the local variable a place's path starts at, when its address, or a part's, is taken.
	addressed := func(place ast.Expr) {
		root, _ := reader.accessPath(place)
		if identifier, isIdentifier := root.(*ast.Ident); isIdentifier {
			if variable := reader.localVariable(identifier); variable != nil {
				stale[variable] = true
			}
		}
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			object, isFunctionObject := reader.info.Defs[function.Name].(*types.Func)
			if !isFunctionObject {
				continue
			}
			signature, isSignature := object.Type().(*types.Signature)
			if !isSignature || signature.Results().Len() != 1 {
				continue
			}
			found := []*ast.ReturnStmt{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.FuncLit:
					// A function literal's returns are its own.
					return false
				case *ast.ReturnStmt:
					found = append(found, typed)
				}
				return true
			})
			returns[object] = found
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				markFields(typed.Recv)
				markFields(typed.Type.Params)
				markFields(typed.Type.Results)
			case *ast.FuncLit:
				markFields(typed.Type.Params)
				markFields(typed.Type.Results)
			case *ast.AssignStmt:
				if typed.Tok != token.DEFINE && typed.Tok != token.ASSIGN {
					return true
				}
				for index, target := range typed.Lhs {
					if identifier, isIdentifier := ast.Unparen(target).(*ast.Ident); isIdentifier {
						if len(typed.Lhs) == len(typed.Rhs) {
							assign(identifier, typed.Rhs[index], true)
						} else {
							assign(identifier, nil, false)
						}
					}
				}
			case *ast.ValueSpec:
				for index, name := range typed.Names {
					switch len(typed.Values) {
					case 0:
						// The zero value, which is the variable's own.
						assign(name, nil, true)
					case len(typed.Names):
						assign(name, typed.Values[index], true)
					default:
						assign(name, nil, false)
					}
				}
			case *ast.RangeStmt:
				for _, target := range []ast.Expr{typed.Key, typed.Value} {
					if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
						assign(identifier, nil, false)
					}
				}
			case *ast.CaseClause:
				if implicit, isVariable := reader.info.Implicits[typed].(*types.Var); isVariable {
					stale[implicit] = true
				}
			case *ast.UnaryExpr:
				if typed.Op == token.AND {
					addressed(typed.X)
				}
			case *ast.CallExpr:
				if place, isPlace := reader.pointerMethodReceiver(typed); isPlace {
					addressed(place)
				}
			}
			return true
		})
	}
	reader.fresh = map[*types.Var]bool{}
	for variable := range values {
		if !stale[variable] {
			reader.fresh[variable] = true
		}
	}
	reader.freshResults = map[*types.Func]bool{}
	for function := range returns {
		reader.freshResults[function] = true
	}
	for changed := true; changed; {
		changed = false
		for variable := range reader.fresh {
			for _, value := range values[variable] {
				if value != nil && !reader.freshExpression(value) {
					delete(reader.fresh, variable)
					changed = true
					break
				}
			}
		}
		for function := range reader.freshResults {
			for _, statement := range returns[function] {
				// A bare return hands back named results, which are never fresh.
				if len(statement.Results) != 1 || !reader.freshExpression(statement.Results[0]) {
					delete(reader.freshResults, function)
					changed = true
					break
				}
			}
		}
	}
}

// freshExpression reports an expression whose value the function built itself, given which variables and functions
// are fresh so far.
func (reader *syntaxTreeWriteReader) freshExpression(expression ast.Expr) bool {
	switch typed := ast.Unparen(expression).(type) {
	case *ast.CompositeLit, *ast.BasicLit, *ast.FuncLit:
		return true
	case *ast.Ident:
		switch object := reader.info.ObjectOf(typed).(type) {
		case *types.Nil:
			return true
		case *types.Var:
			return reader.fresh[object]
		}
	case *ast.UnaryExpr:
		return typed.Op == token.AND && reader.freshExpression(typed.X)
	case *ast.SliceExpr:
		return reader.freshExpression(typed.X)
	case *ast.CallExpr:
		if function := reader.info.Types[typed.Fun]; function.IsType() {
			return len(typed.Args) == 1 && reader.freshExpression(typed.Args[0])
		}
		switch callee := reader.callee(typed).(type) {
		case *types.Builtin:
			switch callee.Name() {
			case "make", "new":
				return true
			case "append":
				return len(typed.Args) > 0 && reader.freshExpression(typed.Args[0])
			}
		case *types.Func:
			if callee.Pkg() == nil {
				return false
			}
			if freshFunctions[callee.Pkg().Path()+"."+callee.Name()] || reader.freshResults[callee] {
				return true
			}
			signature, isSignature := callee.Type().(*types.Signature)
			return isSignature && signature.Recv() != nil && syntaxTreePackages[callee.Pkg().Path()] &&
				typeName(signature.Recv().Type()) == "NodeFactory" && strings.HasPrefix(callee.Name(), "New")
		}
	}
	return false
}

// namesTheSyntaxTree reports a type that is the tree's, or a pointer, slice or array of one.
func namesTheSyntaxTree(subject types.Type, depth int) bool {
	if subject == nil || depth > 8 {
		return false
	}
	if isSyntaxTreeType(subject) {
		return true
	}
	switch typed := types.Unalias(subject).Underlying().(type) {
	case *types.Pointer:
		return namesTheSyntaxTree(typed.Elem(), depth+1)
	case *types.Slice:
		return namesTheSyntaxTree(typed.Elem(), depth+1)
	case *types.Array:
		return namesTheSyntaxTree(typed.Elem(), depth+1)
	}
	return false
}

// isSyntaxTreeType reports a named type the ast package declares.
func isSyntaxTreeType(subject types.Type) bool {
	named, isNamed := types.Unalias(subject).(*types.Named)
	return isNamed && named.Obj().Pkg() != nil && syntaxTreePackages[named.Obj().Pkg().Path()]
}

// typeName is a type's own name, through a pointer: `ClassDeclaration` for `*ast.ClassDeclaration`.
func typeName(subject types.Type) string {
	if pointer, isPointer := types.Unalias(subject).(*types.Pointer); isPointer {
		subject = pointer.Elem()
	}
	if named, isNamed := types.Unalias(subject).(*types.Named); isNamed {
		return named.Obj().Name()
	}
	return typeString(subject)
}

// typeString spells a type with its packages by name, `[]*ast.Node`.
func typeString(subject types.Type) string {
	return types.TypeString(subject, func(typePackage *types.Package) string { return typePackage.Name() })
}
