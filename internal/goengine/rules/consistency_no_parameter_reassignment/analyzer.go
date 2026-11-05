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

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-parameter-reassignment"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_parameter_reassignment",
	Doc: "Flags an assignment to a parameter or receiver of a function or function literal (=, op=, a := that " +
		"reuses it, a range with =, ++ and --) and &parameter anywhere, since a pointer to it lets anyone reassign " +
		"it. A write through it (parameter.Field, parameter[index], *parameter) is the caller's data and not judged " +
		"here, a := in an inner scope is a new variable, and named results are left alone entirely, since " +
		"assigning them is what Go names them for.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var parameterReassignedMessage = policy.MessageOf(Name, "parameterReassigned")

var parameterAddressTakenMessage = policy.MessageOf(Name, "parameterAddressTaken")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// A generated file is its generator's to change, and the generator's next run undoes any edit by hand. A
		// driver filter will take this over.
		if ast.IsGenerated(file) {
			continue
		}
		parameters := parametersOf(pass, file)
		if len(parameters) == 0 {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				// Every token counts: = and op= assign, and a := that names a variable already in scope reuses it
				// rather than declaring it. go/types records a reused name in Uses and a new one in Defs, so a
				// shadowing := in an inner scope never resolves to the parameter here.
				for _, left := range statement.Lhs {
					reportReassigned(pass, parameters, left)
				}
			case *ast.RangeStmt:
				// `for parameter = range values` assigns on every turn; with := the loop declares its own.
				if statement.Tok == token.ASSIGN {
					reportReassigned(pass, parameters, statement.Key)
					reportReassigned(pass, parameters, statement.Value)
				}
			case *ast.IncDecStmt:
				reportReassigned(pass, parameters, statement.X)
			case *ast.UnaryExpr:
				// &parameter.Field and &parameter[index] point into the value, a write through it rather than a
				// new value for the name, so only the bare name's address counts.
				if statement.Op == token.AND {
					if identifier, parameter := parameterNamed(pass, parameters, statement.X); parameter != nil {
						pass.Report(analysis.Diagnostic{
							Pos:      statement.Pos(),
							End:      statement.End(),
							Category: parameterAddressTakenMessage.Id,
							Message:  parameterAddressTakenMessage.Render(map[string]string{"name": identifier.Name}),
						})
					}
				}
			}
			return true
		})
	}
	return nil, nil
}

// parametersOf is every parameter and receiver of a function declared or written as a literal in file, by object,
// so a use resolves to one by identity and never by its spelling. Named results are left out: Go names a result so
// the body can assign it. A function literal inside another function sees that function's parameters too, and an
// assignment to one from inside it is the same reassignment, which the identity catches without knowing which
// function encloses it.
func parametersOf(pass *analysis.Pass, file *ast.File) map[*types.Var]bool {
	parameters := map[*types.Var]bool{}
	collect := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if parameter, isVar := pass.TypesInfo.Defs[name].(*types.Var); isVar {
					parameters[parameter] = true
				}
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch function := node.(type) {
		case *ast.FuncDecl:
			collect(function.Recv)
			collect(function.Type.Params)
		case *ast.FuncLit:
			collect(function.Type.Params)
		}
		return true
	})
	return parameters
}

// reportReassigned flags expression when it is a parameter's bare name. Anything else, a field, an index, a
// dereference or a blank, is a write somewhere the name still points, or no write at all.
func reportReassigned(pass *analysis.Pass, parameters map[*types.Var]bool, expression ast.Expr) {
	identifier, parameter := parameterNamed(pass, parameters, expression)
	if parameter == nil {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: parameterReassignedMessage.Id,
		Message:  parameterReassignedMessage.Render(map[string]string{"name": identifier.Name}),
	})
}

// parameterNamed is the identifier and parameter when expression, through any parentheses, is a name that resolves
// to one of parameters, and nil otherwise. A nil expression, a range with no key or value, is no name.
func parameterNamed(
	pass *analysis.Pass,
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
	parameter, isVar := pass.TypesInfo.Uses[identifier].(*types.Var)
	if !isVar || !parameters[parameter] {
		return nil, nil
	}
	return identifier, parameter
}
