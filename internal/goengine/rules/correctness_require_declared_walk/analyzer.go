// Package correctness_require_declared_walk is the Go engine's rule that a directory walk reaching outside its
// package declares what it reads.
//
// A test planner that keys each package's tests by the package's Go closure, its own testdata, and the reads lines
// kept in one file reruns a test only when something in that key changes. A test that walks a directory outside its
// package, with no reads line, isn't rerun when what it walks changes, so a breaking change lands on a stale green and
// the red surfaces later on someone else's change. This is a port of Adamic's repowalk, written after every such red
// in eight days turned out to be a walk: one test walked another package's _test.go files, and another walked the
// whole repository.
//
// A walk is filepath.WalkDir, filepath.Walk, fs.WalkDir, os.ReadDir, filepath.Glob, os.DirFS or parser.ParseDir. Its
// root leaves the package when it is a constant path that cleans to one starting with "..", what filepath.Join, Abs,
// Clean or Dir make of one, or the directory found by walking up from os.Getwd through filepath.Dir to the one that
// holds go.mod. The root is followed within the package through variables, parameters and results, since the real
// cases go through helpers: a test hands "../.." to a checker, which hands it to a lister, which walks it. While the
// path is known it is read against the package's own directory, so "../../internal/oracle/testdata" walked from
// internal/oracle stays in the package; joined with something the rule can't read, the path is judged by what is
// known of it so far. A root the rule can't trace, such as test.TempDir() or a directory another package computes,
// isn't flagged: the rule never guesses. A root that leaves the package only to open one fixed file is no walk, so it
// isn't flagged.
//
// The finding sits on the source, where the reach outward happens, since the walk itself may be in a helper many
// callers share: on the "../.." argument, or on the call to the function that walks up to the repository root. A
// `// Reads: <glob>` comment on that line or just above it declares the walk, one comment per glob, written as in the
// reads line. The rule doesn't hold the glob against the reads file; the planner does that from the comments.
//
// The rule is off until it is told which file holds the reads lines, through its readsFile option, since a
// repository with no such planner has nothing a declaration would feed.
package correctness_require_declared_walk

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-declared-walk"

// Analyzer runs the rule over one package.
var Analyzer = newAnalyzer()

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{
	Name:      Name,
	Origin:    rule.OriginHouse,
	TypeAware: true,
	Analyzer:  Analyzer,
	// ConfiguredBy lets the engine list the rule as not configured rather than quiet, since it reports nothing
	// until readsFile names the reads file.
	ConfiguredBy: []string{readsFileOption},
}

var undeclaredWalkMessage = policy.MessageOf(Name, "undeclaredWalk")

// readsFileOption names the file of reads lines a planner holds each // Reads: glob against, such as Adamic's
// cloud/fast-gate/executors.txt, which Adamic's rule hardcoded. It is the rule's switch as well as its message's
// pointer: a repository with no such planner leaves it empty, and the rule reports nothing.
const readsFileOption = "readsFile"

// readsMarker opens the comment that declares a walk. It is matched as written, with nothing before it, because a
// planner reads the glob after it, and a comment the rule took as a declaration the planner skipped would leave the
// walk undeclared with no finding to say so.
const readsMarker = "// Reads:"

// relatedWalkText labels the walk a finding's source reaches, for a reader that shows related positions.
const relatedWalkText = "the walk it reaches"

// deepestPath is how many elements a known path may grow to before the rest of it counts as unknown.
const deepestPath = 32

// walkSinks holds each walk by package path and function name, with the index of the argument it walks. fs.WalkDir
// walks its file system, whose root os.DirFS carries; its own root argument is a path inside that file system.
var walkSinks = map[string]map[string]int{
	"path/filepath": {"WalkDir": 0, "Walk": 0, "Glob": 0},
	"io/fs":         {"WalkDir": 0},
	"os":            {"ReadDir": 0, "DirFS": 0},
	"go/parser":     {"ParseDir": 1},
}

// pathCarriers are the path/filepath functions whose first result names the same place as their first argument, so a
// root that leaves the package still leaves it after them. filepath.Dir moves the place up, and is read apart.
var pathCarriers = map[string]bool{
	"Abs": true, "Clean": true, "EvalSymlinks": true, "FromSlash": true, "ToSlash": true,
}

func newAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "correctness_require_declared_walk",
		Doc: "Flags the source of a directory walk rooted outside the package's own directory, a constant path " +
			"that climbs out or a walk up to the directory holding go.mod, when no // Reads: <glob> comment on its " +
			"line or just above it declares what it reads. Reports nothing until the readsFile option names the " +
			"file of reads lines the planner checks those globs against.",
		Run: run,
	}
	analyzer.Flags.String(readsFileOption, "", "the file of reads lines a planner holds each // Reads: glob "+
		"against, named in the finding; empty turns the rule off")
	return analyzer
}

// readsFileOf is the readsFile option of the analyzer running pass. It is read from the pass rather than bound to a
// package variable, so a configured copy of the analyzer and the unconfigured original can run side by side.
func readsFileOf(pass *analysis.Pass) string {
	option := pass.Analyzer.Flags.Lookup(readsFileOption)
	if option == nil {
		return ""
	}
	return option.Value.String()
}

// origin is where a followed path comes from. A source that leaves the package has the position its // Reads:
// comment answers; a parameter is resolved to the arguments at each call. A root found by walking up to go.mod is
// moved, at each call of the function that walks up (walksUpIn), to that call, since the call is where a reader
// sees the reach outward.
//
// path is what is known of the path: for a source, the path as the program builds it, relative to the package's
// directory, and for a parameter, what is joined onto it. opaque says something the rule can't read was joined on,
// so path is only the part before it.
type origin struct {
	position  token.Pos
	parameter *types.Var
	walksUpIn *types.Func
	path      string
	opaque    bool
}

type origins map[origin]bool

// add puts every origin of more into into, and says whether into grew.
func (into origins) add(more origins) bool {
	grew := false
	for incoming := range more {
		if !into[incoming] {
			into[incoming] = true
			grew = true
		}
	}
	return grew
}

// assigned is one value given to a variable: an expression, or one result of a call that returns several.
type assigned struct {
	expression ast.Expr
	result     int
}

// function is what the rule knows of one function declared in the package.
type function struct {
	object      *types.Func
	declaration *ast.FuncDecl
	signature   *types.Signature
	// returns are the function's own return statements, a function literal's left out, and calls are every call in
	// its body, a function literal's included, since a closure walking what its function holds is the same walk.
	returns []*ast.ReturnStmt
	calls   []*ast.CallExpr
	// results are the origins of each result, and walked the origins that reach a walk, each with the walk's position.
	results []origins
	walked  map[origin]token.Pos
}

// walker follows roots through one package until nothing more is learned.
type walker struct {
	pass *analysis.Pass
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

func run(pass *analysis.Pass) (any, error) {
	readsFile := readsFileOf(pass)
	if readsFile == "" || len(pass.Files) == 0 {
		return nil, nil
	}
	walker := &walker{
		pass: pass,
		// The file's own name, not one a //line directive gives it, since the walk runs from where the file sits.
		directory:   filepath.Dir(pass.Fset.PositionFor(pass.Files[0].Package, false).Filename),
		functions:   map[*types.Func]*function{},
		assignments: map[*types.Var][]assigned{},
		variables:   map[*types.Var]origins{},
		owners:      map[*types.Var]*function{},
		unreadable:  map[*types.Var]bool{},
		constants:   map[*types.Var][]string{},
	}
	walker.collect()
	// Every set only grows, and every origin is a position in the package or one of its parameters, with a path
	// joined from the package's own constants, so this ends.
	for walker.settle() {
	}
	walker.report(readsFile)
	return nil, nil
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

// earlier says found comes before position, where no position yet comes after everything.
func earlier(found token.Pos, position token.Pos) bool {
	return position == token.NoPos || found < position
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
	expression = ast.Unparen(expression)
	if result != -1 {
		call, isCall := expression.(*ast.CallExpr)
		if !isCall {
			return origins{}
		}
		return walker.callResult(call, result)
	}
	if value, isString := walker.constantString(expression); isString {
		return walker.source(expression.Pos(), value)
	}
	switch expression := expression.(type) {
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

// isPackageFunction says callee is a function, not a method, of the package at packagePath.
func isPackageFunction(callee *types.Func, packagePath string) bool {
	if callee.Pkg() == nil || callee.Pkg().Path() != packagePath {
		return false
	}
	return callee.Signature().Recv() == nil
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
	declaredLines := map[*token.File]map[int]bool{}
	for _, file := range walker.pass.Files {
		declaredLines[walker.pass.Fset.File(file.Pos())] = readsLines(walker.pass.Fset, file)
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
		file := walker.pass.Fset.File(source)
		if file == nil || declaredLines[file][walker.pass.Fset.PositionFor(source, false).Line] {
			continue
		}
		walker.pass.Report(analysis.Diagnostic{
			Pos:      source,
			Category: undeclaredWalkMessage.Id,
			Message:  undeclaredWalkMessage.Render(map[string]string{"readsFile": readsFile}),
			Related:  []analysis.RelatedInformation{{Pos: walks[source], Message: relatedWalkText}},
		})
	}
}

// readsLines are the lines a // Reads: <glob> comment declares: its own, for a comment at the end of a line, and the
// line just after its comment group, so a walk reading several globs carries a comment for each above the source.
func readsLines(fileSet *token.FileSet, file *ast.File) map[int]bool {
	lines := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			rest, isDeclaration := strings.CutPrefix(comment.Text, readsMarker)
			if !isDeclaration {
				continue
			}
			glob, spaced := strings.CutPrefix(rest, " ")
			// An empty glob names nothing for the planner to check, and a comment holds one glob, written as in
			// the reads line, so anything else declares nothing.
			if !spaced || glob == "" || strings.ContainsFunc(glob, unicode.IsSpace) {
				continue
			}
			lines[fileSet.PositionFor(comment.Pos(), false).Line] = true
			lines[fileSet.PositionFor(group.End(), false).Line+1] = true
		}
	}
	return lines
}
