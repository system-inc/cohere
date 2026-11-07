package correctness_require_declared_walk

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
)

// walker follows roots through one package until nothing more is learned.
type walker struct {
	pass *analysis.Pass
	// table reads a position's file and line without the FileSet's lock.
	table *positions.Table
	// directory is the package's own directory, which every known path is read against.
	directory   string
	functions   map[*types.Func]*function
	assignments map[*types.Var][]assigned
	variables   map[*types.Var]origins
	owners      map[*types.Var]*function
	// unreadable holds the variables written some way the rule doesn't read as a value, by a range, an operator
	// assignment, its address or a declaration with no value, and every parameter and result. constants holds the
	// strings a variable is only ever given as constants, so the path joined from it is known.
	unreadable map[*types.Var]bool
	constants  map[*types.Var][]string
}

// collect reads the package once: its functions, their parameters, returns and calls, every assignment to a variable
// of the package, and the walk-up loops that find the repository root.
func (walker *walker) collect() {
	for _, file := range walker.pass.Files {
		for _, declaration := range file.Decls {
			functionDeclaration, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || functionDeclaration.Body == nil {
				continue
			}
			object, isFunc := walker.pass.TypesInfo.Defs[functionDeclaration.Name].(*types.Func)
			if !isFunc {
				continue
			}
			signature := object.Signature()
			declared := &function{
				object:      object,
				declaration: functionDeclaration,
				signature:   signature,
				results:     make([]origins, signature.Results().Len()),
				walked:      map[origin]token.Pos{},
			}
			for index := range declared.results {
				declared.results[index] = origins{}
				walker.unreadable[signature.Results().At(index)] = true
			}
			for index := range signature.Params().Len() {
				parameter := signature.Params().At(index)
				walker.owners[parameter] = declared
				walker.unreadable[parameter] = true
				walker.variables[parameter] = origins{{parameter: parameter}: true}
			}
			walker.functions[object] = declared
			ast.Inspect(functionDeclaration.Body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.CallExpr:
					declared.calls = append(declared.calls, node)
				case *ast.ReturnStmt:
					declared.returns = append(declared.returns, node)
				case *ast.FuncLit:
					// A function literal's returns are its own, so they are read apart from the function's.
					ast.Inspect(node.Body, func(inner ast.Node) bool {
						if call, isCall := inner.(*ast.CallExpr); isCall {
							declared.calls = append(declared.calls, call)
						}
						return true
					})
					return false
				}
				return true
			})
			walker.findWalkUps(declared)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				for index, target := range node.Lhs {
					if node.Tok != token.ASSIGN && node.Tok != token.DEFINE {
						walker.markUnreadable(target)
						continue
					}
					walker.assign(target, node.Rhs, len(node.Lhs), index)
				}
			case *ast.ValueSpec:
				for index, name := range node.Names {
					walker.assign(name, node.Values, len(node.Names), index)
				}
			case *ast.RangeStmt:
				walker.markUnreadable(node.Key)
				walker.markUnreadable(node.Value)
			case *ast.UnaryExpr:
				if node.Op == token.AND {
					walker.markUnreadable(node.X)
				}
			}
			return true
		})
	}
	for variable, values := range walker.assignments {
		if walker.unreadable[variable] {
			continue
		}
		var texts []string
		for _, value := range values {
			text, isString := walker.constantString(value.expression)
			if !isString || value.result != -1 {
				texts = nil
				break
			}
			texts = append(texts, text)
		}
		if texts != nil {
			walker.constants[variable] = texts
		}
	}
}

func (walker *walker) markUnreadable(target ast.Expr) {
	if target == nil {
		return
	}
	if variable := walker.variableOf(target); variable != nil {
		walker.unreadable[variable] = true
	}
}

// assign records the value the index'th of count targets is given from values: its own, or one result of the call
// that is the only value.
func (walker *walker) assign(target ast.Expr, values []ast.Expr, count, index int) {
	variable := walker.variableOf(target)
	if variable == nil {
		return
	}
	switch {
	case len(values) == count:
		walker.assignments[variable] = append(
			walker.assignments[variable],
			assigned{expression: values[index], result: -1},
		)
	case len(values) == 1:
		walker.assignments[variable] = append(
			walker.assignments[variable],
			assigned{expression: values[0], result: index},
		)
	default:
		// A declaration with no value holds the zero value, which is no path the rule reads.
		walker.unreadable[variable] = true
	}
}

// variableOf is the package's own variable an expression names, or nil: a field, or another package's variable,
// is never followed.
func (walker *walker) variableOf(expression ast.Expr) *types.Var {
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	variable, isVariable := walker.pass.TypesInfo.ObjectOf(identifier).(*types.Var)
	if !isVariable || variable.IsField() || variable.Pkg() != walker.pass.Pkg {
		return nil
	}
	return variable
}

// findWalkUps finds the loop that walks up from the working directory to the repository root: a variable on a cycle
// of assignments through filepath.Dir, reached from os.Getwd's result, whose cycle joins a variable with "go.mod".
// Each variable on the cycle is that root from the start, since the loop may return or walk any of them.
func (walker *walker) findWalkUps(declared *function) {
	// sources holds, for each variable, the variables it is given and whether through filepath.Dir.
	type edge struct {
		from             *types.Var
		throughDirectory bool
	}
	sources := map[*types.Var][]edge{}
	workingDirectories := map[*types.Var]token.Pos{}
	joinedWithGoMod := map[*types.Var]bool{}
	record := func(target ast.Expr, value ast.Expr, result int) {
		variable := walker.variableOf(target)
		if variable == nil {
			return
		}
		call, isCall := ast.Unparen(value).(*ast.CallExpr)
		if isCall && result == 0 && walker.isFunction(call, "os", "Getwd") {
			workingDirectories[variable] = call.Pos()
			return
		}
		if result != -1 {
			return
		}
		if isCall && walker.isFunction(call, "path/filepath", "Dir") && len(call.Args) == 1 {
			if from := walker.variableOf(call.Args[0]); from != nil {
				sources[variable] = append(sources[variable], edge{from: from, throughDirectory: true})
			}
			return
		}
		if from := walker.variableOf(value); from != nil {
			sources[variable] = append(sources[variable], edge{from: from})
		}
	}
	ast.Inspect(declared.declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for index, target := range node.Lhs {
				switch {
				case len(node.Lhs) == len(node.Rhs):
					record(target, node.Rhs[index], -1)
				case len(node.Rhs) == 1:
					record(target, node.Rhs[0], index)
				}
			}
		case *ast.ValueSpec:
			for index, name := range node.Names {
				switch {
				case len(node.Names) == len(node.Values):
					record(name, node.Values[index], -1)
				case len(node.Values) == 1:
					record(name, node.Values[0], index)
				}
			}
		case *ast.CallExpr:
			if !walker.isFunction(node, "path/filepath", "Join") {
				return true
			}
			namesGoMod := false
			for _, argument := range node.Args {
				if value, isString := walker.constantString(argument); isString && filepath.Base(value) == "go.mod" {
					namesGoMod = true
				}
			}
			if !namesGoMod {
				return true
			}
			for _, argument := range node.Args {
				if variable := walker.variableOf(argument); variable != nil {
					joinedWithGoMod[variable] = true
				}
			}
		}
		return true
	})
	// reaches is every variable start is given, directly or through others, with whether some step went through
	// filepath.Dir; a variable reaching itself through filepath.Dir is on the walk-up cycle.
	reaches := func(start *types.Var) map[*types.Var]bool {
		type state struct {
			variable         *types.Var
			throughDirectory bool
		}
		seen := map[state]bool{}
		pending := []state{{variable: start}}
		reached := map[*types.Var]bool{}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			for _, source := range sources[current.variable] {
				next := state{
					variable:         source.from,
					throughDirectory: current.throughDirectory || source.throughDirectory,
				}
				if seen[next] {
					continue
				}
				seen[next] = true
				reached[next.variable] = reached[next.variable] || next.throughDirectory
				pending = append(pending, next)
			}
		}
		return reached
	}
	for variable := range sources {
		reached := reaches(variable)
		if !reached[variable] {
			continue
		}
		// The cycle is every variable that reaches back to this one; the go.mod test may be on any of them.
		readsGoMod := joinedWithGoMod[variable]
		for other := range reached {
			if _, onCycle := reaches(other)[variable]; onCycle && joinedWithGoMod[other] {
				readsGoMod = true
			}
		}
		// The loop starts from os.Getwd's result, given to this variable or one it reaches; the earliest is the
		// position, so the finding is stable.
		position := token.NoPos
		for other := range reached {
			if found, isWorking := workingDirectories[other]; isWorking && earlier(found, position) {
				position = found
			}
		}
		if found, isWorking := workingDirectories[variable]; isWorking && earlier(found, position) {
			position = found
		}
		if !readsGoMod || position == token.NoPos {
			continue
		}
		if walker.variables[variable] == nil {
			walker.variables[variable] = origins{}
		}
		// Where the repository root is from the package isn't written anywhere, so the path is unknown from the start.
		walker.variables[variable][origin{position: position, walksUpIn: declared.object, opaque: true}] = true
	}
}

// settle reads every assignment, return and call once more with what is known so far, and says whether anything grew.
func (walker *walker) settle() bool {
	grew := false
	for variable, values := range walker.assignments {
		known := walker.variables[variable]
		if known == nil {
			known = origins{}
			walker.variables[variable] = known
		}
		packageLevel := variable.Parent() == walker.pass.Pkg.Scope()
		for _, value := range values {
			found := walker.evaluate(value.expression, value.result)
			for candidate := range found {
				// A package variable belongs to no call, so a parameter it is given can't be resolved.
				if packageLevel && candidate.parameter != nil {
					delete(found, candidate)
				}
			}
			grew = known.add(found) || grew
		}
	}
	for _, declared := range walker.functions {
		grew = walker.settleReturns(declared) || grew
		for _, call := range declared.calls {
			grew = walker.settleCall(declared, call) || grew
		}
	}
	return grew
}

func (walker *walker) settleReturns(declared *function) bool {
	grew := false
	results := declared.signature.Results()
	for _, statement := range declared.returns {
		switch {
		case len(statement.Results) == 0:
			// A bare return gives the named results as they stand.
			for index := range results.Len() {
				grew = declared.results[index].add(walker.variables[results.At(index)]) || grew
			}
		case len(statement.Results) == results.Len():
			for index, result := range statement.Results {
				grew = declared.results[index].add(walker.evaluate(result, -1)) || grew
			}
		case len(statement.Results) == 1:
			for index := range results.Len() {
				grew = declared.results[index].add(walker.evaluate(statement.Results[0], index)) || grew
			}
		}
	}
	return grew
}

// settleCall records what a call in declared walks: the root of a walk itself, or the arguments a function of the
// package hands to its own walks.
func (walker *walker) settleCall(declared *function, call *ast.CallExpr) bool {
	grew := false
	walked := func(found origins, position token.Pos) {
		for candidate := range found {
			// Only this function's own parameters are resolved at its calls; anything else isn't followed.
			if candidate.parameter != nil && walker.owners[candidate.parameter] != declared {
				continue
			}
			if existing, recorded := declared.walked[candidate]; !recorded || position < existing {
				declared.walked[candidate] = position
				grew = true
			}
		}
	}
	if index, isSink := walker.sinkArgument(call); isSink {
		walked(walker.evaluate(call.Args[index], -1), call.Pos())
		return grew
	}
	callee := walker.packageFunction(call)
	if callee == nil {
		return grew
	}
	for candidate, position := range callee.walked {
		if candidate.parameter == nil {
			continue
		}
		walked(walker.resolve(call, callee, candidate), position)
	}
	return grew
}

// resolve is a parameter origin of callee as one call gives it: the origins of the arguments, with what callee
// joined onto the parameter joined onto them.
func (walker *walker) resolve(call *ast.CallExpr, callee *function, parameter origin) origins {
	found := origins{}
	for _, argument := range walker.arguments(call, callee, parameter.parameter) {
		found.add(walker.evaluate(argument, -1))
	}
	if parameter.opaque {
		return walker.joined(found, nil)
	}
	return walker.joined(found, []string{parameter.path})
}

// evaluate is where the value of an expression comes from, or of one result of it when result isn't -1.
func (walker *walker) evaluate(expression ast.Expr, result int) origins {
	unwrapped := ast.Unparen(expression)
	if result != -1 {
		call, isCall := unwrapped.(*ast.CallExpr)
		if !isCall {
			return origins{}
		}
		return walker.callResult(call, result)
	}
	if value, isString := walker.constantString(unwrapped); isString {
		return walker.source(unwrapped.Pos(), value)
	}
	switch expression := unwrapped.(type) {
	case *ast.Ident:
		if variable := walker.variableOf(expression); variable != nil {
			found := origins{}
			found.add(walker.variables[variable])
			return found
		}
	case *ast.BinaryExpr:
		// A path built by concatenation starts where its left side does; a right side that starts a new path element
		// joins it on, and anything else makes the rest of the path unknown.
		if expression.Op != token.ADD {
			return origins{}
		}
		left := walker.evaluate(expression.X, -1)
		found := origins{}
		values := walker.stringValues(expression.Y)
		if values == nil {
			return walker.joined(left, nil)
		}
		for _, value := range values {
			element, startsElement := strings.CutPrefix(value, "/")
			if !startsElement {
				found.add(walker.joined(left, nil))
				continue
			}
			found.add(walker.joined(left, []string{element}))
		}
		return found
	case *ast.CallExpr:
		// A conversion, string(root), names the same place as its operand.
		if walker.pass.TypesInfo.Types[expression.Fun].IsType() && len(expression.Args) == 1 {
			return walker.evaluate(expression.Args[0], -1)
		}
		return walker.callResult(expression, 0)
	}
	return origins{}
}

// callResult is where one result of a call comes from: a path function's argument, or a package function's result
// with its parameters resolved to this call's arguments.
func (walker *walker) callResult(call *ast.CallExpr, result int) origins {
	callee := typeutil.StaticCallee(walker.pass.TypesInfo, call)
	if callee == nil {
		return origins{}
	}
	callee = callee.Origin()
	if isPackageFunction(callee, "path/filepath") && result == 0 && len(call.Args) > 0 {
		switch {
		case callee.Name() == "Join":
			return walker.join(call)
		case callee.Name() == "Dir":
			return walker.parent(walker.evaluate(call.Args[0], -1))
		case pathCarriers[callee.Name()]:
			return walker.evaluate(call.Args[0], -1)
		}
	}
	// os.DirFS's file system is rooted where its argument is, so fs.WalkDir over it walks there.
	if isPackageFunction(callee, "os") && callee.Name() == "DirFS" && result == 0 {
		return walker.evaluate(call.Args[0], -1)
	}
	declared := walker.functions[callee]
	if declared == nil || result >= len(declared.results) {
		return origins{}
	}
	found := origins{}
	for candidate := range declared.results[result] {
		switch {
		case candidate.parameter != nil:
			found.add(walker.resolve(call, declared, candidate))
		case candidate.walksUpIn == callee:
			candidate.position = call.Pos()
			candidate.walksUpIn = nil
			found[candidate] = true
		default:
			found[candidate] = true
		}
	}
	return found
}

// join is where filepath.Join's result comes from. Its leading constant arguments are joined as the call would join
// them, so filepath.Join("..", "..") leaves the package though neither argument does alone; past them, the path
// starts where its first argument does, and each argument after is joined on.
func (walker *walker) join(call *ast.CallExpr) origins {
	if call.Ellipsis.IsValid() {
		return origins{}
	}
	var leading []string
	for _, argument := range call.Args {
		value, isString := walker.constantString(argument)
		if !isString {
			break
		}
		leading = append(leading, value)
	}
	var found origins
	var rest []ast.Expr
	if len(leading) > 0 {
		found = walker.source(call.Pos(), filepath.Join(leading...))
		rest = call.Args[len(leading):]
	} else {
		found = walker.evaluate(call.Args[0], -1)
		rest = call.Args[1:]
	}
	for _, argument := range rest {
		values := walker.stringValues(argument)
		if values == nil {
			found = walker.joined(found, nil)
			continue
		}
		joined := origins{}
		for _, value := range values {
			joined.add(walker.joined(found, []string{value}))
		}
		found = joined
	}
	return found
}

// source is the origin of a constant path at position, if the path leaves the package.
func (walker *walker) source(position token.Pos, path string) origins {
	if !walker.leavesPackage(path) {
		return origins{}
	}
	return origins{{position: position, path: path}: true}
}

// joined is found with elements joined onto each path, or, for nil elements, with something the rule can't read
// joined on. A source whose known path comes back into the package is dropped; one joined with something unknown is
// judged by its known part, which still leaves the package, since a source that came back was dropped already.
func (walker *walker) joined(found origins, elements []string) origins {
	result := origins{}
	for candidate := range found {
		switch {
		case candidate.opaque:
		case elements == nil:
			candidate.opaque = true
		default:
			candidate.path = filepath.Join(append([]string{candidate.path}, elements...)...)
			if candidate.parameter == nil && !walker.leavesPackage(candidate.path) {
				continue
			}
			// A function that calls itself with a path joined onto its parameter would join on forever, so a path
			// this deep is judged by what is known of it, as if the rest were unknown.
			if strings.Count(candidate.path, "/") >= deepestPath {
				candidate.opaque = true
			}
		}
		result[candidate] = true
	}
	return result
}

// parent is found after filepath.Dir: the directory holding each path, read as the program reads it.
func (walker *walker) parent(found origins) origins {
	result := origins{}
	for candidate := range found {
		switch {
		case candidate.opaque:
		case candidate.parameter != nil && candidate.path == "":
			// The parameter's own parent is no path joined onto it.
			candidate.opaque = true
		default:
			candidate.path = filepath.Dir(candidate.path)
			if candidate.parameter != nil && candidate.path == "." {
				candidate.path = ""
			}
			if candidate.parameter == nil && !walker.leavesPackage(candidate.path) {
				continue
			}
		}
		result[candidate] = true
	}
	return result
}

// stringValues are the strings an expression may be: a constant, or a variable only ever given constants, or nil.
func (walker *walker) stringValues(expression ast.Expr) []string {
	if value, isString := walker.constantString(expression); isString {
		return []string{value}
	}
	if variable := walker.variableOf(expression); variable != nil {
		return walker.constants[variable]
	}
	return nil
}

// arguments are the expressions a call gives a parameter: one, or all of a variadic parameter's unless the call
// spreads a slice into it.
func (walker *walker) arguments(call *ast.CallExpr, callee *function, parameter *types.Var) []ast.Expr {
	parameters := callee.signature.Params()
	if selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSelector {
		// A method expression takes its receiver as the first argument, which the rule doesn't follow.
		selection, isSelection := walker.pass.TypesInfo.Selections[selector]
		if isSelection && selection.Kind() == types.MethodExpr {
			return nil
		}
	}
	for index := range parameters.Len() {
		if parameters.At(index) != parameter {
			continue
		}
		if callee.signature.Variadic() && index == parameters.Len()-1 && !call.Ellipsis.IsValid() {
			if index >= len(call.Args) {
				return nil
			}
			return call.Args[index:]
		}
		if index < len(call.Args) {
			return call.Args[index : index+1]
		}
	}
	return nil
}

// packageFunction is the function of this package a call reaches, or nil.
func (walker *walker) packageFunction(call *ast.CallExpr) *function {
	callee := typeutil.StaticCallee(walker.pass.TypesInfo, call)
	if callee == nil {
		return nil
	}
	return walker.functions[callee.Origin()]
}

// sinkArgument is the index of the argument a walk call walks, matched by the function object, so a function of the
// same name in another package is never mistaken for it.
func (walker *walker) sinkArgument(call *ast.CallExpr) (int, bool) {
	callee := typeutil.StaticCallee(walker.pass.TypesInfo, call)
	if callee == nil || callee.Pkg() == nil {
		return 0, false
	}
	walks, isWalkPackage := walkSinks[callee.Pkg().Path()]
	if !isWalkPackage || !isPackageFunction(callee, callee.Pkg().Path()) {
		return 0, false
	}
	index, isWalk := walks[callee.Name()]
	if !isWalk || index >= len(call.Args) {
		return 0, false
	}
	return index, true
}

// isFunction says call reaches the package-level function name of the package at packagePath.
func (walker *walker) isFunction(call *ast.CallExpr, packagePath, name string) bool {
	callee := typeutil.StaticCallee(walker.pass.TypesInfo, call)
	return callee != nil && isPackageFunction(callee, packagePath) && callee.Name() == name
}

func (walker *walker) constantString(expression ast.Expr) (string, bool) {
	value := walker.pass.TypesInfo.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

// leavesPackage says a relative path, read from the package's directory, names a place outside it. A path that climbs
// out and back in, such as "../../internal/oracle/testdata" read from internal/oracle, stays in the package.
func (walker *walker) leavesPackage(path string) bool {
	if filepath.IsAbs(path) {
		return false
	}
	relative, relativeError := filepath.Rel(walker.directory, filepath.Join(walker.directory, path))
	if relativeError != nil {
		return false
	}
	relative = filepath.ToSlash(relative)
	return relative == ".." || strings.HasPrefix(relative, "../")
}

// report flags each source that reaches a walk once, on the source, with the walk it reaches as related.
func (walker *walker) report(readsFile string) {
	// Each file's lines are held by its name, which the table reads for every source.
	declaredLines := map[string]map[int]bool{}
	globlessLines := map[string]map[int]bool{}
	for _, file := range walker.pass.Files {
		fileName := walker.table.Position(file.Pos()).Filename
		declaredLines[fileName], globlessLines[fileName] = readsLines(walker.table, file)
	}
	walks := map[token.Pos]token.Pos{}
	for _, declared := range walker.functions {
		for candidate, walk := range declared.walked {
			if candidate.parameter != nil {
				continue
			}
			if existing, recorded := walks[candidate.position]; !recorded || walk < existing {
				walks[candidate.position] = walk
			}
		}
	}
	// The sources are reported in the order they sit in the package, so a run's findings read the same every time.
	sources := make([]token.Pos, 0, len(walks))
	for source := range walks {
		sources = append(sources, source)
	}
	slices.Sort(sources)
	for _, source := range sources {
		// A source in no file reads as the zero Position, with no name.
		position := walker.table.Position(source)
		if position.Filename == "" || declaredLines[position.Filename][position.Line] {
			continue
		}
		// A marker with no glob after it declares nothing, as Adamic reads it, but its writer meant to declare the
		// walk, so the finding names the missing glob rather than saying nothing was written.
		message := undeclaredWalkMessage
		if globlessLines[position.Filename][position.Line] {
			message = readsWithoutGlobMessage
		}
		walker.pass.Report(analysis.Diagnostic{
			Pos:      source,
			Category: message.Id,
			Message:  message.Render(map[string]string{"readsFile": readsFile}),
			Related:  []analysis.RelatedInformation{{Pos: walks[source], Message: relatedWalkText}},
		})
	}
}
