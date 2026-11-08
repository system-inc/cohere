// Package consistency_no_parameter_reassignment is the Go engine's rule that a parameter keeps the value its caller
// passed.
//
// A reader who sees a parameter used near the end of a function takes it for what the caller passed. An assignment
// halfway down makes that wrong without a sign at the place of use, so the reader has to scan every line above it
// first. A new variable with its own name says what the value has become, and leaves the parameter meaning what its
// name promised.
package consistency_no_parameter_reassignment

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-parameter-reassignment"

// listener gathers each function's parameters as the syntax dispatcher's walk meets the function, and judges every
// assignment, range, increment, decrement and address taken that the walk meets after.
var listener = rule.Listener{
	FileEnter: enterFile,
	Nodes: []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
		(*ast.AssignStmt)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.IncDecStmt)(nil),
		(*ast.UnaryExpr)(nil),
	},
	Node: judgeNode,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_parameter_reassignment", "Flags an assignment to a "+
	"parameter or receiver of a function or function literal (=, op=, a := that reuses it, a range with =, ++ and --) "+
	"and &parameter anywhere, since a pointer to it lets anyone reassign it. A write through it (parameter.Field, "+
	"parameter[index], *parameter) is the caller's data and not judged here, a := in an inner scope is a new "+
	"variable, and named results are left alone entirely, since assigning them is what Go names them for.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var parameterReassignedMessage = policy.MessageOf(Name, "parameterReassigned")

var parameterAddressTakenMessage = policy.MessageOf(Name, "parameterAddressTaken")

// fileState is the parameters of the file being walked, held in the rule's context's state.
type fileState struct {
	// parameters is every parameter and receiver of a function declared or written as a literal in the file that the
	// walk has met so far, by object, so a use resolves to one by identity and never by its spelling. A parameter is
	// in scope only inside its function, which the walk meets before anything in it, so every parameter a name
	// inside the function can resolve to is here by the time the name is judged. A function literal inside another
	// function sees that function's parameters too, and an assignment to one from inside it is the same
	// reassignment, which the identity catches without knowing which function encloses it.
	parameters map[*types.Var]bool
}

// enterFile starts the file with no parameters met.
func enterFile(context *rule.Context, _ *ast.File) {
	state := rule.State[fileState](context)
	if state.parameters == nil {
		state.parameters = map[*types.Var]bool{}
	}
	clear(state.parameters)
}

// judgeNode gathers a function's parameters, and reports an assignment to one or its address taken.
func judgeNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	parameters := rule.State[fileState](context).parameters
	switch statement := node.(type) {
	case *ast.FuncDecl:
		collectParameters(context, parameters, statement.Recv)
		collectParameters(context, parameters, statement.Type.Params)
	case *ast.FuncLit:
		collectParameters(context, parameters, statement.Type.Params)
	case *ast.AssignStmt:
		// Every token counts: = and op= assign, and a := that names a variable already in scope reuses it rather
		// than declaring it. go/types records a reused name in Uses and a new one in Defs, so a shadowing := in an
		// inner scope never resolves to the parameter here.
		for _, left := range statement.Lhs {
			reportReassigned(context, parameters, left)
		}
	case *ast.RangeStmt:
		// `for parameter = range values` assigns on every turn; with := the loop declares its own.
		if statement.Tok == token.ASSIGN {
			reportReassigned(context, parameters, statement.Key)
			reportReassigned(context, parameters, statement.Value)
		}
	case *ast.IncDecStmt:
		reportReassigned(context, parameters, statement.X)
	case *ast.UnaryExpr:
		// &parameter.Field and &parameter[index] point into the value, a write through it rather than a new value for
		// the name, so only the bare name's address counts.
		if statement.Op == token.AND {
			if identifier, parameter := parameterNamed(context, parameters, statement.X); parameter != nil {
				context.Report(analysis.Diagnostic{
					Pos:      statement.Pos(),
					End:      statement.End(),
					Category: parameterAddressTakenMessage.Id,
					Message:  parameterAddressTakenMessage.Render(map[string]string{"name": identifier.Name}),
				})
			}
		}
	}
}

// collectParameters adds each name fields declares to parameters. Named results are never handed here: Go names a
// result so the body can assign it.
//
// Mutates: parameters gains each name's variable, which is how the file's parameters are gathered
func collectParameters(context *rule.Context, parameters map[*types.Var]bool, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			if parameter, isVariable := context.TypesInfo.Defs[name].(*types.Var); isVariable {
				parameters[parameter] = true
			}
		}
	}
}

// reportReassigned flags expression when it is a parameter's bare name. Anything else, a field, an index, a
// dereference or a blank, is a write somewhere the name still points, or no write at all.
func reportReassigned(context *rule.Context, parameters map[*types.Var]bool, expression ast.Expr) {
	identifier, parameter := parameterNamed(context, parameters, expression)
	if parameter == nil {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: parameterReassignedMessage.Id,
		Message:  parameterReassignedMessage.Render(map[string]string{"name": identifier.Name}),
	})
}

// parameterNamed is the identifier and parameter when expression, through any parentheses, is a name that resolves
// to one of parameters, and nil otherwise. A nil expression, a range with no key or value, is no name.
func parameterNamed(
	context *rule.Context,
	parameters map[*types.Var]bool,
	expression ast.Expr,
) (*ast.Ident, *types.Var) {
	if expression == nil {
		return nil, nil
	}
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier {
		return nil, nil
	}
	parameter, isVariable := context.TypesInfo.Uses[identifier].(*types.Var)
	if !isVariable || !parameters[parameter] {
		return nil, nil
	}
	return identifier, parameter
}
