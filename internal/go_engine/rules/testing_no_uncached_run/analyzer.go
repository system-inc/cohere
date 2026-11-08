// Package testing_no_uncached_run is the Go engine's rule that no code runs go test with -count=1.
//
// Go's test cache is keyed honestly: a cached pass is replayed only while the code, the environment and the files the
// test read are unchanged, so it is a real pass. -count=1 throws that away, usually to rerun around a flaky test
// instead of fixing it, and every rerun burns the shared pool other work is waiting on.
package testing_no_uncached_run

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-uncached-run"

// listener gathers the package's locals and go commands over the syntax dispatcher's walk, reading each file's
// comments as it enters, and judges the commands once every local and every write to one has been seen.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{marker.Analyzer},
	FileEnter: enterFile,
	Nodes: []ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
		(*ast.IncDecStmt)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.UnaryExpr)(nil),
		(*ast.CallExpr)(nil),
	},
	Node:        gather,
	PackageExit: judgeCommands,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "testing_no_uncached_run", "Flags an os/exec Command or CommandContext "+
	"that runs the constant program \"go\" with test and -count=1 among its constant arguments, written inline, "+
	"spread from a []string literal, or spread from a local assigned that literal once, since -count=1 discards an "+
	"honest test cache to rerun around a flaky test. Arguments the rule cannot read as constants, and those after "+
	"-args, which go to the test binary, leave it quiet, as does a // Uncached: <reason> comment directly above the "+
	"call's statement or at the end of it.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var uncachedRunMessage = policy.MessageOf(Name, "uncachedRun")

var uncachedWithoutReasonMessage = policy.MessageOf(Name, "uncachedWithoutReason")

// argument is one element of a command's argument list as the rule reads it. An argument whose value is not a
// string constant is kept as unknown, holding its place, so a flag and its value are only paired when they are
// adjacent.
type argument struct {
	value string
	known bool
	node  ast.Expr
}

// packageState is what the rule gathers over its package, held in its context's state.
type packageState struct {
	// indexes is the package's comment indexes, read as the walk enters its first file.
	indexes *marker.Indexes
	// comments is the comment index of the file being walked.
	comments *marker.Index
	// known is every local's declared value and every local written after its declaration, gathered over the whole
	// walk, since a write may follow the command that spreads the local.
	known locals
	// commands is every call that runs the constant program "go", in the order the walk met them.
	commands []goCommand
}

// goCommand is one call to os/exec's Command or CommandContext whose program is the constant "go", kept for judging
// once the walk is over.
type goCommand struct {
	call *ast.CallExpr
	// programIndex is where the program sits among the call's arguments.
	programIndex int
	// holder is holdingStatement of the call, which an Uncached comment sits beside.
	holder ast.Node
	// comments is the comment index of the call's file.
	comments *marker.Index
}

// enterFile reads the package's comment indexes the first time, and file's comment index.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if state.indexes == nil {
		indexes, isIndexes := context.ResultOf(marker.Analyzer).(*marker.Indexes)
		if !isIndexes {
			// Invariant: marker's result is always its Indexes, so another is a mistake in the engine, and the
			// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
			panic(fmt.Sprintf("reading the comment indexes: marker gave %T", context.ResultOf(marker.Analyzer)))
		}
		state.indexes = indexes
		state.known = locals{definitions: map[*types.Var]ast.Expr{}, written: map[*types.Var]bool{}}
	}
	state.comments = state.indexes.ForFile(file)
}

// judgeCommands reports every -count=1 in a go test command the walk found, unless a reasoned Uncached comment beside
// its statement excuses it.
func judgeCommands(context *rule.Context) {
	state := rule.State[packageState](context)
	// A literal spread from a local reaches every command that spreads that local, and one finding at it is enough.
	reported := map[token.Pos]bool{}
	for _, command := range state.commands {
		arguments, isGoCommand := goArguments(context, state.known, command.call, command.programIndex)
		if !isGoCommand {
			continue
		}
		message := uncachedRunMessage
		switch command.comments.ExcuseFor(command.holder, marker.Uncached) {
		case marker.WithReason:
			continue
		case marker.WithoutReason:
			message = uncachedWithoutReasonMessage
		case marker.None:
		}
		for _, flag := range uncachedFlags(arguments) {
			if reported[flag.node.Pos()] {
				continue
			}
			reported[flag.node.Pos()] = true
			context.Report(analysis.Diagnostic{
				Pos:      flag.node.Pos(),
				End:      flag.node.End(),
				Category: message.Id,
				Message:  message.Render(map[string]string{}),
			})
		}
	}
}

// holdingStatement is the innermost statement among the call's ancestors, or, for a call in a package-level
// declaration, which no statement holds, the innermost value specification or declaration. It is what an Uncached
// comment sits beside. The call itself stands in when nothing holds it.
func holdingStatement(ancestors []ast.Node, call *ast.CallExpr) ast.Node {
	for index := len(ancestors) - 1; index >= 0; index-- {
		switch ancestors[index].(type) {
		case ast.Stmt, *ast.ValueSpec, ast.Decl:
			return ancestors[index]
		}
	}
	return call
}

// goProgramIndex is where the program sits among the arguments of call when it is a call to os/exec's Command or
// CommandContext whose program is the constant "go". isGoCommand is false for any other call.
func goProgramIndex(context *rule.Context, call *ast.CallExpr) (programIndex int, isGoCommand bool) {
	function, isFunc := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	if !isFunc || function.Pkg() == nil || function.Pkg().Path() != "os/exec" || function.Signature().Recv() != nil {
		return 0, false
	}
	// The program is Command's first parameter and CommandContext's second, after the context.
	switch function.Name() {
	case "Command":
	case "CommandContext":
		programIndex = 1
	default:
		return 0, false
	}
	if len(call.Args) <= programIndex {
		return 0, false
	}
	program, isConstant := stringConstant(context, call.Args[programIndex])
	if !isConstant || program != "go" {
		return 0, false
	}
	return programIndex, true
}

// goArguments is the argument list, after the program at programIndex, of a call goProgramIndex found runs "go".
// isGoCommand is false for one whose spread slice cannot be read.
func goArguments(
	context *rule.Context,
	known locals,
	call *ast.CallExpr,
	programIndex int,
) (arguments []argument, isGoCommand bool) {
	rest := call.Args[programIndex+1:]
	// A spread call passes exactly one slice after the program, the whole argument list.
	if call.Ellipsis.IsValid() {
		if len(rest) != 1 {
			return nil, false
		}
		return known.sequence(context, rest[0])
	}
	for _, expression := range rest {
		arguments = append(arguments, argumentOf(context, expression))
	}
	return arguments, true
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
func argumentOf(context *rule.Context, expression ast.Expr) argument {
	value, isConstant := stringConstant(context, expression)
	return argument{value: value, known: isConstant, node: expression}
}

// stringConstant is the value of expression when the type checker folded it to a string constant.
func stringConstant(context *rule.Context, expression ast.Expr) (string, bool) {
	typeAndValue, recorded := context.TypesInfo.Types[expression]
	if !recorded || typeAndValue.Value == nil || typeAndValue.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(typeAndValue.Value), true
}

// gather finds, in node, a local variable's declared value, a write to a local after its declaration, or a call
// that runs the constant program "go". A local is visible only inside the function declaring it, so walking the
// package's files sees every write it can have.
func gather(context *rule.Context, node ast.Node, stack []ast.Node) {
	state := rule.State[packageState](context)
	markWritten := func(expression ast.Expr) {
		if variable := writtenVariable(context, expression); variable != nil {
			state.known.written[variable] = true
		}
	}
	switch statement := node.(type) {
	case *ast.AssignStmt:
		for index, left := range statement.Lhs {
			identifier, isIdentifier := left.(*ast.Ident)
			// A := declares only the names that are new in its scope; any other name on its left is assigned.
			if statement.Tok == token.DEFINE && isIdentifier {
				if variable, isNew := context.TypesInfo.Defs[identifier].(*types.Var); isNew {
					if len(statement.Lhs) == len(statement.Rhs) {
						state.known.definitions[variable] = statement.Rhs[index]
					}
					continue
				}
			}
			markWritten(left)
		}
	case *ast.ValueSpec:
		for index, name := range statement.Names {
			variable, isVariable := context.TypesInfo.Defs[name].(*types.Var)
			if isVariable && len(statement.Names) == len(statement.Values) {
				state.known.definitions[variable] = statement.Values[index]
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
			if builtin, isBuiltin := context.TypesInfo.Uses[identifier].(*types.Builtin); isBuiltin &&
				builtin.Name() == "copy" {
				markWritten(statement.Args[0])
			}
		}
		// Whether the command's arguments can be read waits for the end of the walk, when every write is known.
		if programIndex, isGoCommand := goProgramIndex(context, statement); isGoCommand {
			state.commands = append(state.commands, goCommand{
				call:         statement,
				programIndex: programIndex,
				holder:       holdingStatement(stack, statement),
				comments:     state.comments,
			})
		}
	}
}

// writtenVariable is the variable a write to expression changes: the variable itself, or the one whose elements an
// index or slice of it reaches. It is nil for anything else, a field or a dereference, which no traced local is.
func writtenVariable(context *rule.Context, expression ast.Expr) *types.Var {
	switch shape := ast.Unparen(expression).(type) {
	case *ast.Ident:
		variable, _ := context.TypesInfo.Uses[shape].(*types.Var)
		return variable
	case *ast.IndexExpr:
		return writtenVariable(context, shape.X)
	case *ast.SliceExpr:
		return writtenVariable(context, shape.X)
	}
	return nil
}
