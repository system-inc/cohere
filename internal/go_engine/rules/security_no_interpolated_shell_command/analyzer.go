// Package security_no_interpolated_shell_command is the Go engine's rule against building a value into the script a
// shell runs.
//
// exec.Command("sh", "-c", fmt.Sprintf("cd %s && make", directory)) hands the shell one string, and the shell parses
// all of it as code, the value included: a space or a quote in directory breaks the command, and a $(...) or a
// backtick in it runs. Quoting by hand, %q included, is one escape away from the same bug, since %q leaves $ and
// backticks live inside the double quotes it adds. The repair keeps the script constant and passes the value after it
// as its own argument, which the shell reads as $1 and never parses, or runs the program directly with no shell.
//
// It ports nexus/security-no-interpolated-shell-command and cohere-swift/security-no-interpolated-shell-command. Go,
// like Swift, has no function that adds a shell on its own, so the shell is always the program, named by a constant
// whose last path element is sh, bash, zsh, dash, ksh or mksh, and its -c operand is the script. From there it is
// the twins' judgment:
//
//   - The script is built here: a call to fmt.Sprintf or a + concatenation of strings, through parentheses and local
//     variables that are never assigned again. An opaque script (a parameter, a field, another call's result) is not
//     judged, since it may be a whole script its caller wrote as a constant.
//   - A value spliced into it is not known to be free of shell syntax. Constants are text the author wrote. A
//     boolean, an integer or a float, and the text strconv formats one as, holds no shell syntax. Every other value
//     is text the shell will parse: a string, a named string type, an error, an interface, a slice, a struct.
//
// What it does not judge, every one a finding missed and never one invented: a script with << in its written text,
// since the twins read a quoted heredoc's body as data and this rule declines rather than parse heredocs; a value
// whose text comes from its own String, Error or Format method, a type parameter, or arguments spread into Sprintf;
// a script built with fmt.Sprint, strings.Join, a strings.Builder or a template; a local assigned more than once or
// whose address is taken; arguments given as a slice built elsewhere; an exec.Cmd written as a struct literal; and
// interpreters with their own grammar (python3 -c, osascript -e).
package security_no_interpolated_shell_command

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/inert_value"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/security-no-interpolated-shell-command"

// listener hears every call of the syntax dispatcher's walk.
var listener = rule.Listener{
	FileLocal: true, Nodes: []ast.Node{(*ast.CallExpr)(nil)}, Node: judgeCall}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "security_no_interpolated_shell_command", "Flags exec.Command and "+
	"exec.CommandContext running sh, bash, zsh, dash, ksh or mksh with a -c script built by fmt.Sprintf or + from a "+
	"value that is not a constant, a number or a boolean, since the shell parses that value as code. A script with "+
	"<< in it, or one not built at the call or in a local assigned once, is not judged.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var interpolatedShellCommandMessage = policy.MessageOf(Name, "interpolatedShellCommand")

// shellPrograms are the POSIX shells whose -c takes a script, the Swift twin's list.
var shellPrograms = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "mksh": true}

// longOptions take no argument, so the reading steps over them. Any other long option stops it.
var longOptions = map[string]bool{
	"--login": true, "--interactive": true, "--norc": true, "--noprofile": true, "--noediting": true,
	"--posix": true, "--restricted": true, "--verbose": true,
}

// longOptionsWithArgument take the next argument as their value.
var longOptionsWithArgument = map[string]bool{"--rcfile": true, "--init-file": true}

// depthLimit bounds the walk through Sprintf calls, concatenations and locals. A chain this long is never written by
// hand; past it the script is not judged, a finding missed and never one invented.
const depthLimit = 16

// piece is one stretch of a flattened script: text the author wrote, or a value whose text is known only at run time.
type piece struct {
	text  string
	value ast.Expr
	// unknown marks a value the rule cannot judge, such as arguments spread into Sprintf.
	unknown bool
}

// verdict is what the rule concludes about one value spliced into a script.
type verdict int

const (
	// safe is a value whose text holds no shell syntax the author did not write.
	safe verdict = iota
	// unsafe is text the shell will parse as code.
	unsafe
	// unknown is a value the rule cannot judge, so it says nothing about it.
	unknown
)

// judgeCall reports the script of node when it is a shell command whose script splices in a value the shell parses
// as code.
func judgeCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	programIndex, isCommand := programIndexOf(context, call)
	if !isCommand || len(call.Args) <= programIndex {
		return
	}
	program, isConstant := constantString(context, call.Args[programIndex])
	// A path names the same program, so /bin/zsh is judged as zsh.
	if !isConstant || !shellPrograms[path.Base(program)] {
		return
	}
	script := scriptOf(context, argumentsOf(call, programIndex))
	if script == nil || !isBuilt(context, script, 0) {
		return
	}
	if splicesUnsafeValue(context, script) {
		context.Report(analysis.Diagnostic{
			Pos:      script.Pos(),
			End:      script.End(),
			Category: interpolatedShellCommandMessage.Id,
			Message:  interpolatedShellCommandMessage.Render(nil),
		})
	}
}

// programIndexOf is where the program sits among a call's arguments when the call is os/exec's Command or
// CommandContext, resolved by the callee's object so a function of the same name in another package is never one.
func programIndexOf(context *rule.Context, call *ast.CallExpr) (int, bool) {
	if isFunction(context, call, "os/exec", "Command") {
		return 0, true
	}
	if isFunction(context, call, "os/exec", "CommandContext") {
		// The context comes first.
		return 1, true
	}
	return 0, false
}

// isFunction is whether call resolves to the package-level function name in the package at packagePath.
func isFunction(context *rule.Context, call *ast.CallExpr, packagePath string, name string) bool {
	function, isFunc := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	return isFunc && function.Pkg() != nil && function.Pkg().Path() == packagePath &&
		function.Signature().Recv() == nil && function.Name() == name
}

// argumentsOf is what a command is given after its program. A spread is read when it is a slice literal written in
// place; any other spread is a slice built elsewhere, so its arguments are unknown and the result is nil.
func argumentsOf(call *ast.CallExpr, programIndex int) []ast.Expr {
	expressions := call.Args[programIndex+1:]
	if !call.Ellipsis.IsValid() {
		return expressions
	}
	literal, isLiteral := ast.Unparen(expressions[len(expressions)-1]).(*ast.CompositeLit)
	if !isLiteral {
		return nil
	}
	for _, element := range literal.Elts {
		// An indexed element, []string{2: "x"}, places its value somewhere other than where it is written.
		if _, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
			return nil
		}
	}
	return literal.Elts
}

// constantString is expression's value when the type checker knows it as a constant string.
func constantString(context *rule.Context, expression ast.Expr) (string, bool) {
	value := context.TypesInfo.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

// scriptOf is the script a shell's arguments give it, read as the shell reads them: constant options until the first
// operand, and when a short option cluster among them holds c, that operand is the script. A -o or -O in a cluster
// takes the next argument as its value, -- or - ends the options, and a long option the rule does not know stops the
// reading. An argument known only at run time before the c may be an option that changes everything after it, and
// one after the c that starts like an option would make the script a later argument, so either stops the reading too.
func scriptOf(context *rule.Context, arguments []ast.Expr) ast.Expr {
	takesScript := false
	index := 0
	for index < len(arguments) {
		option, isConstant := constantString(context, arguments[index])
		if !isConstant {
			if !takesScript || startsLikeOption(context, arguments[index]) {
				return nil
			}
			break
		}
		if option == "--" || option == "-" {
			index++
			break
		}
		if len(option) < 2 || option[0] != '-' && option[0] != '+' {
			// The first operand: the script when a c came before it, otherwise a script file.
			break
		}
		if strings.HasPrefix(option, "--") {
			switch {
			case longOptionsWithArgument[option]:
				index += 2
			case longOptions[option]:
				index++
			default:
				return nil
			}
			continue
		}
		letters := option[1:]
		// A + turns an option off, so +c asks for no script.
		if option[0] == '-' && strings.Contains(letters, "c") {
			takesScript = true
		}
		if strings.ContainsAny(letters, "oO") {
			index += 2
		} else {
			index++
		}
	}
	if !takesScript || index >= len(arguments) {
		return nil
	}
	return arguments[index]
}

// startsLikeOption is whether a value known only at run time opens with text the author wrote that begins with - or
// +, which the shell would read as an option rather than the script.
func startsLikeOption(context *rule.Context, expression ast.Expr) bool {
	pieces, isFlattened := flatten(context, expression, 0, nil)
	if !isFlattened {
		return true
	}
	for _, flattened := range pieces {
		if flattened.value != nil || flattened.unknown {
			return false
		}
		if flattened.text != "" {
			return flattened.text[0] == '-' || flattened.text[0] == '+'
		}
	}
	return false
}

// isBuilt is whether a script is built here: a call to fmt.Sprintf or a + concatenation that is not a constant,
// through parentheses and locals assigned once. Anything else is an opaque script this rule does not judge.
func isBuilt(context *rule.Context, expression ast.Expr, depth int) bool {
	if depth > depthLimit {
		return false
	}
	unwrapped := ast.Unparen(expression)
	if context.TypesInfo.Types[unwrapped].Value != nil {
		return false
	}
	switch built := unwrapped.(type) {
	case *ast.CallExpr:
		return isFunction(context, built, "fmt", "Sprintf")
	case *ast.BinaryExpr:
		return built.Op == token.ADD
	case *ast.Ident:
		if initializer := onlyValue(context, built); initializer != nil {
			return isBuilt(context, initializer, depth+1)
		}
	}
	return false
}

// splicesUnsafeValue is whether a built script holds a value the shell parses as code. A << in the text the author
// wrote may open a heredoc, whose body the twins read as data when its delimiter is quoted, so the rule declines
// such a script rather than judge values the shell may never parse.
func splicesUnsafeValue(context *rule.Context, script ast.Expr) bool {
	pieces, isFlattened := flatten(context, script, 0, nil)
	if !isFlattened {
		return false
	}
	for _, flattened := range pieces {
		if strings.Contains(flattened.text, "<<") {
			return false
		}
	}
	for _, flattened := range pieces {
		if flattened.value != nil && !flattened.unknown && judge(context, flattened.value) == unsafe {
			return true
		}
	}
	return false
}

// flatten appends an expression's pieces in order: the text of constants, and the pieces of every fmt.Sprintf call,
// + concatenation of strings and local assigned once it is built from. A Sprintf's format is one piece of text with
// its verbs in it, and its arguments follow it, since which verb takes which argument changes no verdict: a string
// under any verb still prints its text. Anything else is one value. It reports false when the walk runs too deep.
func flatten(context *rule.Context, expression ast.Expr, depth int, pieces []piece) ([]piece, bool) {
	if depth > depthLimit {
		return nil, false
	}
	unwrapped := ast.Unparen(expression)
	gathered := pieces
	if value := context.TypesInfo.Types[unwrapped].Value; value != nil {
		// A numeric constant handed to Sprintf prints digits, which hold no shell syntax and no <<.
		if value.Kind() == constant.String {
			gathered = append(gathered, piece{text: constant.StringVal(value)})
		}
		return gathered, true
	}
	switch built := unwrapped.(type) {
	case *ast.CallExpr:
		if isFunction(context, built, "fmt", "Sprintf") && len(built.Args) > 0 {
			if built.Ellipsis.IsValid() {
				// The arguments are a slice built elsewhere, whose elements and their types are not seen here.
				return append(gathered, piece{value: built, unknown: true}), true
			}
			for _, argument := range built.Args {
				var isFlattened bool
				if gathered, isFlattened = flatten(context, argument, depth+1, gathered); !isFlattened {
					return nil, false
				}
			}
			return gathered, true
		}
	case *ast.BinaryExpr:
		if built.Op == token.ADD && isString(context.TypesInfo.TypeOf(built)) {
			var isFlattened bool
			if gathered, isFlattened = flatten(context, built.X, depth+1, gathered); !isFlattened {
				return nil, false
			}
			return flatten(context, built.Y, depth+1, gathered)
		}
	case *ast.Ident:
		if initializer := onlyValue(context, built); initializer != nil {
			return flatten(context, initializer, depth+1, gathered)
		}
	}
	return append(gathered, piece{value: unwrapped}), true
}

// judge is what a value spliced into a script holds. A boolean, integer or float, and the text strconv formats one
// as, prints digits, a sign, a point and letters, none of them shell syntax: package inert_value's judgment, which the
// SQL rule shares. A number or boolean whose type prints through a String, Error or Format method of its own is not
// inert, and its text this rule cannot see, so it is not judged, nor is a type parameter. Everything else is text
// the shell parses: a string, a named string type, an error, an interface that may hold a string, a slice, a struct
// or a pointer, which print their contents.
func judge(context *rule.Context, value ast.Expr) verdict {
	if inert_value.Is(context.TypesInfo, value) {
		return safe
	}
	valueType := context.TypesInfo.TypeOf(value)
	if valueType == nil {
		return unknown
	}
	if _, isTypeParameter := types.Unalias(valueType).(*types.TypeParam); isTypeParameter {
		return unknown
	}
	// Past inert_value, a number or a boolean is one that prints through a method of its own.
	basic, isBasic := valueType.Underlying().(*types.Basic)
	if isBasic && basic.Info()&(types.IsBoolean|types.IsNumeric) != 0 {
		return unknown
	}
	return unsafe
}

// isString is whether a type is a string or a type whose underlying type is one.
func isString(candidate types.Type) bool {
	basic, isBasic := candidate.Underlying().(*types.Basic)
	return isBasic && basic.Info()&types.IsString != 0
}

// onlyValue is the initializer of the local variable an identifier names when that initializer is the only value it
// ever holds: declared with := or var and one value for each name, never assigned again, and its address never taken.
// A package-level variable may be assigned from any file, so it is never followed. Nil otherwise.
func onlyValue(context *rule.Context, identifier *ast.Ident) ast.Expr {
	variable, isVariable := context.TypesInfo.Uses[identifier].(*types.Var)
	if !isVariable || variable.IsField() || variable.Pkg() == nil || variable.Parent() == nil ||
		variable.Parent() == variable.Pkg().Scope() {
		return nil
	}
	var file *ast.File
	for _, candidate := range context.Files {
		if candidate.FileStart <= variable.Pos() && variable.Pos() < candidate.FileEnd {
			file = candidate
		}
	}
	if file == nil {
		return nil
	}
	var initializer ast.Expr
	isAssignedAgain := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			for index, target := range statement.Lhs {
				name, isName := ast.Unparen(target).(*ast.Ident)
				if !isName {
					continue
				}
				if context.TypesInfo.Defs[name] == variable && statement.Tok == token.DEFINE &&
					len(statement.Lhs) == len(statement.Rhs) {
					initializer = statement.Rhs[index]
				}
				// A redeclaration in a later :=, `script, readError := ...`, records the name as a use.
				if context.TypesInfo.Uses[name] == variable {
					isAssignedAgain = true
				}
			}
		case *ast.ValueSpec:
			for index, name := range statement.Names {
				if context.TypesInfo.Defs[name] == variable && len(statement.Names) == len(statement.Values) {
					initializer = statement.Values[index]
				}
			}
		case *ast.UnaryExpr:
			// A pointer to the variable may be written through anywhere.
			if name, isName := ast.Unparen(statement.X).(*ast.Ident); isName && statement.Op == token.AND &&
				context.TypesInfo.Uses[name] == variable {
				isAssignedAgain = true
			}
		case *ast.RangeStmt:
			for _, target := range []ast.Expr{statement.Key, statement.Value} {
				if name, isName := target.(*ast.Ident); isName && context.TypesInfo.Uses[name] == variable {
					isAssignedAgain = true
				}
			}
		}
		return true
	})
	if isAssignedAgain {
		return nil
	}
	return initializer
}
