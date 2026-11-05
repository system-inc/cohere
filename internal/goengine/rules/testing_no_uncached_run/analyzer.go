// Package testing_no_uncached_run is the Go engine's rule that no code runs go test with -count=1.
//
// Go's test cache is keyed honestly: a cached pass is replayed only while the code, the environment and the files the
// test read are unchanged, so it is a real pass. -count=1 throws that away, usually to rerun around a flaky test
// instead of fixing it, and every rerun burns the shared pool other work is waiting on.
package testing_no_uncached_run

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-uncached-run"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "testing_no_uncached_run",
	Doc: "Flags an os/exec Command or CommandContext that runs the constant program \"go\" with test and -count=1 " +
		"among its constant arguments, written inline, spread from a []string literal, or spread from a local " +
		"assigned that literal once, since -count=1 discards an honest test cache to rerun around a flaky test. " +
		"Arguments the rule cannot read as constants, and those after -args, which go to the test binary, leave it " +
		"quiet, as does a // Uncached: <reason> comment directly above the call's statement or at the end of it.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var uncachedRunMessage = policy.MessageOf(Name, "uncachedRun")

var uncachedWithoutReasonMessage = policy.MessageOf(Name, "uncachedWithoutReason")

// uncachedMarker opens the comment that excuses a run from the test cache, matched as written.
const uncachedMarker = "Uncached:"

// argument is one element of a command's argument list as the rule reads it. An argument whose value is not a
// string constant is kept as unknown, holding its place, so a flag and its value are only paired when they are
// adjacent.
type argument struct {
	value string
	known bool
	node  ast.Expr
}

// locals is what the rule knows about the package's local variables, gathered once so tracing a spread slice to its
// definition costs a map lookup.
type locals struct {
	// definitions is the expression each local was given where it was declared, when it was declared with one.
	definitions map[*types.Var]ast.Expr
	// written holds every local assigned again after its declaration, or whose elements or address are written, so
	// its declared value is no longer known to be what reaches the command.
	written map[*types.Var]bool
}

func run(pass *analysis.Pass) (any, error) {
	known := gatherLocals(pass)
	// A literal spread from a local reaches every command that spreads that local, and one finding at it is enough.
	reported := map[token.Pos]bool{}
	for _, file := range pass.Files {
		comments := marker.NewIndex(pass.Fset, file)
		// The nodes from the file down to the one being visited, so a call finds the statement holding it.
		var enclosing []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				enclosing = enclosing[:len(enclosing)-1]
				return true
			}
			enclosing = append(enclosing, node)
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			arguments, isGoCommand := goArguments(pass, known, call)
			if !isGoCommand {
				return true
			}
			message := uncachedRunMessage
			switch comments.ExcuseFor(holdingStatement(enclosing), uncachedMarker) {
			case marker.WithReason:
				return true
			case marker.WithoutReason:
				message = uncachedWithoutReasonMessage
			case marker.None:
			}
			for _, flag := range uncachedFlags(arguments) {
				if reported[flag.node.Pos()] {
					continue
				}
				reported[flag.node.Pos()] = true
				pass.Report(analysis.Diagnostic{
					Pos:      flag.node.Pos(),
					End:      flag.node.End(),
					Category: message.Id,
					Message:  message.Render(map[string]string{}),
				})
			}
			return true
		})
	}
	return nil, nil
}

// holdingStatement is the innermost statement among enclosing, or, for a call in a package-level declaration, which
// no statement holds, the innermost value specification or declaration. It is what an Uncached comment sits beside.
// enclosing ends with the call itself, which stands in when nothing holds it.
func holdingStatement(enclosing []ast.Node) ast.Node {
	for index := len(enclosing) - 1; index >= 0; index-- {
		switch enclosing[index].(type) {
		case ast.Stmt, *ast.ValueSpec, ast.Decl:
			return enclosing[index]
		}
	}
	return enclosing[len(enclosing)-1]
}

// goArguments is the argument list, after the program, of a call to os/exec's Command or CommandContext whose program
// is the constant "go". isGoCommand is false for any other call, and for one whose spread slice cannot be read.
func goArguments(pass *analysis.Pass, known locals, call *ast.CallExpr) (arguments []argument, isGoCommand bool) {
	function, isFunc := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunc || function.Pkg() == nil || function.Pkg().Path() != "os/exec" || function.Signature().Recv() != nil {
		return nil, false
	}
	// The program is Command's first parameter and CommandContext's second, after the context.
	programIndex := 0
	switch function.Name() {
	case "Command":
	case "CommandContext":
		programIndex = 1
	default:
		return nil, false
	}
	if len(call.Args) <= programIndex {
		return nil, false
	}
	program, isConstant := stringConstant(pass, call.Args[programIndex])
	if !isConstant || program != "go" {
		return nil, false
	}
	rest := call.Args[programIndex+1:]
	// A spread call passes exactly one slice after the program, the whole argument list.
	if call.Ellipsis.IsValid() {
		if len(rest) != 1 {
			return nil, false
		}
		return known.sequence(pass, rest[0])
	}
	for _, expression := range rest {
		arguments = append(arguments, argumentOf(pass, expression))
	}
	return arguments, true
}

// sequence is the elements of a slice expression spread into a command, when the rule can read them: a []string
// literal, a call to the builtin append on a slice it can read, or a local declared with one of those and never
// written since. Anything else, a slice from a call, a parameter or a package variable, is unreadable.
func (known locals) sequence(pass *analysis.Pass, expression ast.Expr) ([]argument, bool) {
	switch shape := ast.Unparen(expression).(type) {
	case *ast.CompositeLit:
		var elements []argument
		for _, element := range shape.Elts {
			// A keyed element places itself by index, so the literal's written order is not the slice's.
			if _, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
				return nil, false
			}
			elements = append(elements, argumentOf(pass, element))
		}
		return elements, true
	case *ast.CallExpr:
		return known.appended(pass, shape)
	case *ast.Ident:
		variable, isVariable := pass.TypesInfo.Uses[shape].(*types.Var)
		if !isVariable || variable.Parent() == pass.Pkg.Scope() || known.written[variable] {
			return nil, false
		}
		definition, isDefined := known.definitions[variable]
		if !isDefined {
			return nil, false
		}
		return known.sequence(pass, definition)
	}
	return nil, false
}

// appended reads a call to the builtin append whose first argument is a slice the rule can read. What it appends is
// read too, and a spread it cannot read holds one unknown place, since it can add arguments but not remove the
// constants before it.
func (known locals) appended(pass *analysis.Pass, call *ast.CallExpr) ([]argument, bool) {
	identifier, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdentifier || len(call.Args) == 0 {
		return nil, false
	}
	builtin, isBuiltin := pass.TypesInfo.Uses[identifier].(*types.Builtin)
	if !isBuiltin || builtin.Name() != "append" {
		return nil, false
	}
	elements, readable := known.sequence(pass, call.Args[0])
	if !readable {
		return nil, false
	}
	added := call.Args[1:]
	if call.Ellipsis.IsValid() && len(added) == 1 {
		spread, spreadReadable := known.sequence(pass, added[0])
		if !spreadReadable {
			return append(elements, argument{node: added[0]}), true
		}
		return append(elements, spread...), true
	}
	for _, expression := range added {
		elements = append(elements, argumentOf(pass, expression))
	}
	return elements, true
}

// uncachedFlags is every argument that sets the test count to 1 in a go test command line: -count=1, -test.count=1,
// or either flag followed by a separate "1", with one dash or two, as Go's flag package reads them all alike. The
// command must be go test, its subcommand first or after -C and its directory, so a program go run runs, which may
// take arguments spelled the same, is never mistaken for it. Everything after -args goes to the test binary as it is,
// so a count there is not go test's flag.
func uncachedFlags(arguments []argument) []argument {
	subcommand := subcommandIndex(arguments)
	if subcommand < 0 || !arguments[subcommand].known || arguments[subcommand].value != "test" {
		return nil
	}
	var flags []argument
	for index := subcommand + 1; index < len(arguments); index++ {
		candidate := arguments[index]
		if !candidate.known {
			continue
		}
		if candidate.value == "-args" || candidate.value == "--args" {
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(candidate.value, "-"), "-"), "=")
		if !strings.HasPrefix(candidate.value, "-") || (name != "count" && name != "test.count") {
			continue
		}
		if hasValue {
			if value == "1" {
				flags = append(flags, candidate)
			}
			continue
		}
		if index+1 < len(arguments) && arguments[index+1].known && arguments[index+1].value == "1" {
			flags = append(flags, candidate)
		}
	}
	return flags
}

// subcommandIndex is where the go command's subcommand sits: first, or after -C and its directory, the one flag go
// reads before a subcommand. It is -1 when the list is too short or opens with something the rule cannot read.
func subcommandIndex(arguments []argument) int {
	if len(arguments) == 0 || !arguments[0].known {
		return -1
	}
	switch {
	case arguments[0].value == "-C" || arguments[0].value == "--C":
		if len(arguments) < 3 {
			return -1
		}
		return 2
	case strings.HasPrefix(arguments[0].value, "-C=") || strings.HasPrefix(arguments[0].value, "--C="):
		if len(arguments) < 2 {
			return -1
		}
		return 1
	}
	return 0
}

// argumentOf reads one argument expression, known when it is a string constant, a literal or a named constant alike.
func argumentOf(pass *analysis.Pass, expression ast.Expr) argument {
	value, isConstant := stringConstant(pass, expression)
	return argument{value: value, known: isConstant, node: expression}
}

// stringConstant is the value of expression when the type checker folded it to a string constant.
func stringConstant(pass *analysis.Pass, expression ast.Expr) (string, bool) {
	typeAndValue, recorded := pass.TypesInfo.Types[expression]
	if !recorded || typeAndValue.Value == nil || typeAndValue.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(typeAndValue.Value), true
}

// gatherLocals finds every local variable's declared value and every local written after its declaration. A local
// is visible only inside the function declaring it, so walking the package's files sees every write it can have.
func gatherLocals(pass *analysis.Pass) locals {
	known := locals{definitions: map[*types.Var]ast.Expr{}, written: map[*types.Var]bool{}}
	markWritten := func(expression ast.Expr) {
		if variable := writtenVariable(pass, expression); variable != nil {
			known.written[variable] = true
		}
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for index, left := range statement.Lhs {
					identifier, isIdentifier := left.(*ast.Ident)
					// A := declares only the names that are new in its scope; any other name on its left is assigned.
					if statement.Tok == token.DEFINE && isIdentifier {
						if variable, isNew := pass.TypesInfo.Defs[identifier].(*types.Var); isNew {
							if len(statement.Lhs) == len(statement.Rhs) {
								known.definitions[variable] = statement.Rhs[index]
							}
							continue
						}
					}
					markWritten(left)
				}
			case *ast.ValueSpec:
				for index, name := range statement.Names {
					variable, isVariable := pass.TypesInfo.Defs[name].(*types.Var)
					if isVariable && len(statement.Names) == len(statement.Values) {
						known.definitions[variable] = statement.Values[index]
					}
				}
			case *ast.IncDecStmt:
				markWritten(statement.X)
			case *ast.RangeStmt:
				if statement.Tok == token.ASSIGN {
					markWritten(statement.Key)
					markWritten(statement.Value)
				}
			case *ast.UnaryExpr:
				// An address taken lets anything holding it write the slice.
				if statement.Op == token.AND {
					markWritten(statement.X)
				}
			case *ast.CallExpr:
				// The builtin copy writes the elements of its first argument.
				identifier, isIdentifier := ast.Unparen(statement.Fun).(*ast.Ident)
				if isIdentifier && len(statement.Args) > 0 {
					if builtin, isBuiltin := pass.TypesInfo.Uses[identifier].(*types.Builtin); isBuiltin &&
						builtin.Name() == "copy" {
						markWritten(statement.Args[0])
					}
				}
			}
			return true
		})
	}
	return known
}

// writtenVariable is the variable a write to expression changes: the variable itself, or the one whose elements an
// index or slice of it reaches. It is nil for anything else, a field or a dereference, which no traced local is.
func writtenVariable(pass *analysis.Pass, expression ast.Expr) *types.Var {
	switch shape := ast.Unparen(expression).(type) {
	case *ast.Ident:
		variable, _ := pass.TypesInfo.Uses[shape].(*types.Var)
		return variable
	case *ast.IndexExpr:
		return writtenVariable(pass, shape.X)
	case *ast.SliceExpr:
		return writtenVariable(pass, shape.X)
	}
	return nil
}
