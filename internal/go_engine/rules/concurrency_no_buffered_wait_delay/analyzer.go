// Package concurrency_no_buffered_wait_delay is the Go engine's rule against exec.Cmd.WaitDelay on a command whose
// output goes to memory, ported from Adamic's waitdelay.
//
// When a command's Stdout or Stderr is anything but an *os.File, exec copies it through a pipe of its own, and with
// WaitDelay set, Wait closes those reads a fixed time after the process exits and reports exec.ErrWaitDelay if the
// copying had not finished. So a run that is slow to drain its output, or whose descendant still holds a pipe, is red
// by chance, and on a shared runner one such red holds up other people's changes. An *os.File goes to the child as it
// is, with nothing to copy, so WaitDelay there only bounds the wait. The cure is the Capture function of the package
// the cure option names, which reads each stream to its end with no timer.
//
// The rule reads one top-level declaration at a time. A WaitDelay is flagged when the same declaration gives that
// command an output that isn't an *os.File, calls its Output or CombinedOutput, hands it to the cure package's Run or
// CombinedOutput, or returns it, since a caller may buffer what it is handed: Adamic's markdownblocks helper set
// WaitDelay and returned the command, and its caller gave it bytes.Buffer outputs. The cure package's Run wraps every
// output in a writer of its own, never an *os.File, and its CombinedOutput gives the command a bytes.Buffer, so exec
// copies through pipes under both; its Capture gives the command *os.File pipes, so a command handed to it is not
// buffered. A line carrying // WaitDelay: <reason>, on it or in the comment just above, is a use someone has thought
// about, and the reason is what the next reader needs.
package concurrency_no_buffered_wait_delay

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/concurrency-no-buffered-wait-delay"

// cureFlag is the option naming the cure package's import path.
const cureFlag = "cure"

// NewAnalyzer builds the rule's analyzer with a flag set of its own. The rule reads its cure option from the
// analyzer running it, so a test can run a configured and an unconfigured analyzer side by side without one
// setting a flag the other reads.
func NewAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "concurrency_no_buffered_wait_delay",
		Doc: "Flags exec.Cmd.WaitDelay on a command whose output is buffered, or whose callers may buffer it, since " +
			"Wait then reports exec.ErrWaitDelay by chance when a slow run drains late, unless a // WaitDelay: " +
			"<reason> comment ends its line or sits directly above it. The cure option names the import path of " +
			"the package whose Capture is the cure and whose Run and CombinedOutput buffer; the rule reports " +
			"nothing when cure is empty.",
		Requires: []*analysis.Analyzer{positions.Analyzer},
		Run:      run,
	}
	// The cure package differs by repository, so the rule hardcodes none, and with no cure to name, a finding would
	// tell its reader to reach for nothing; an empty cure turns the rule off.
	analyzer.Flags.String(cureFlag, "", "import path of the package whose Capture is the cure for a buffered WaitDelay")
	return analyzer
}

// Analyzer runs the rule over one package.
var Analyzer = NewAnalyzer()

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{
	Name:      Name,
	Origin:    rule.OriginHouse,
	TypeAware: true,
	Analyzer:  Analyzer,
	// ConfiguredBy lets the engine list the rule as not configured rather than quiet, since it reports nothing
	// until cure names the cure package.
	ConfiguredBy: []string{cureFlag},
}

var bufferedWaitDelayMessage = policy.MessageOf(Name, "bufferedWaitDelay")

var bufferedWaitDelayWithoutReasonMessage = policy.MessageOf(Name, "bufferedWaitDelayWithoutReason")

// waitDelayMarker opens the comment that excuses a WaitDelay, Adamic's spelling, so a reason written for Adamic's
// rule carries over.
const waitDelayMarker = "WaitDelay:"

// excuse is what the comments on or above a line say about the WaitDelay there.
type excuse int

const (
	excuseNone excuse = iota
	// excuseWithoutReason is a marker with nothing after its colon, which tells the next reader nothing.
	excuseWithoutReason
	excuseWithReason
)

func run(pass *analysis.Pass) (any, error) {
	cure := ""
	// The flag is declared by NewAnalyzer, the only way the rule's analyzer is built, but an analyzer assembled some
	// other way has no cure to read, which is the same as an empty one.
	if cureOption := pass.Analyzer.Flags.Lookup(cureFlag); cureOption != nil {
		cure = cureOption.Value.String()
	}
	if cure == "" {
		return nil, nil
	}
	// The package that holds the cure bounds its own reads as it sees fit, and an external test package's path
	// carries _test while belonging to the cure as much as the package does.
	if strings.TrimSuffix(pass.Pkg.Path(), "_test") == cure {
		return nil, nil
	}
	table, isTable := pass.ResultOf[positions.Analyzer].(*positions.Table)
	if !isTable {
		return nil, fmt.Errorf("reading the line table: positions gave %T", pass.ResultOf[positions.Analyzer])
	}
	for _, file := range pass.Files {
		excuses := excusedLines(table, file)
		for _, declaration := range file.Decls {
			check(pass, table, cure, declaration, excuses)
		}
	}
	return nil, nil
}

// command names an exec.Cmd the way a declaration's code reaches it: the variable it starts from, and the fields
// walked from there, so command.WaitDelay and command.Stdout are the same command, and job.command.WaitDelay another.
type command struct {
	root types.Object
	path string
}

// delay is one place WaitDelay is set. literal is the exec.Cmd composite literal that sets it, or nil for an
// assignment; reached says whether the code names the command, which a literal handed straight to a call doesn't.
type delay struct {
	identifier *ast.Ident
	command    command
	reached    bool
	literal    *ast.CompositeLit
}

// check reads one top-level declaration whole, function literals inside it included, since a closure that buffers
// the command its function set WaitDelay on is the same mistake. A WaitDelay's line is read from table, without the
// FileSet's lock.
func check(pass *analysis.Pass, table *positions.Table, cure string, declaration ast.Decl, excuses map[int]excuse) {
	var delays []delay
	buffered := map[command]bool{}
	returned := map[command]bool{}
	literalCommands := map[*ast.CompositeLit]command{}
	literalBuffered := map[*ast.CompositeLit]bool{}
	literalReturned := map[*ast.CompositeLit]bool{}
	// A literal is visited after the statement that holds it, so the statement says which command it is first.
	nameLiteral := func(target ast.Expr, value ast.Expr) {
		literal := commandLiteral(pass, value)
		if literal == nil {
			return
		}
		if named, isNamed := commandOf(pass, target); isNamed {
			literalCommands[literal] = named
		}
	}
	ast.Inspect(declaration, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for index, target := range node.Lhs {
				if len(node.Lhs) == len(node.Rhs) {
					nameLiteral(target, node.Rhs[index])
				}
				selector, isSelector := ast.Unparen(target).(*ast.SelectorExpr)
				if !isSelector {
					continue
				}
				switch commandField(pass, selector) {
				case "WaitDelay":
					named, reached := commandOf(pass, selector.X)
					delays = append(delays, delay{identifier: selector.Sel, command: named, reached: reached})
				case "Stdout", "Stderr":
					if !buffers(assignedType(pass, node, index)) {
						continue
					}
					if named, isNamed := commandOf(pass, selector.X); isNamed {
						buffered[named] = true
					}
				}
			}
		case *ast.ValueSpec:
			for index, target := range node.Names {
				if index < len(node.Values) {
					nameLiteral(target, node.Values[index])
				}
			}
		case *ast.ReturnStmt:
			for _, result := range node.Results {
				if literal := commandLiteral(pass, result); literal != nil {
					literalReturned[literal] = true
				}
				if named, isNamed := commandOf(pass, result); isNamed && isCommand(pass.TypesInfo.TypeOf(result)) {
					returned[named] = true
				}
			}
		case *ast.CompositeLit:
			if !isCommand(pass.TypesInfo.TypeOf(node)) {
				return true
			}
			for _, element := range node.Elts {
				pair, isPair := element.(*ast.KeyValueExpr)
				if !isPair {
					continue
				}
				key, isIdentifier := pair.Key.(*ast.Ident)
				if !isIdentifier {
					continue
				}
				field, isVariable := pass.TypesInfo.Uses[key].(*types.Var)
				if !isVariable || !isCommandField(field) {
					continue
				}
				switch field.Name() {
				case "WaitDelay":
					named, reached := literalCommands[node]
					delays = append(delays, delay{identifier: key, command: named, reached: reached, literal: node})
				case "Stdout", "Stderr":
					if buffers(pass.TypesInfo.Types[pair.Value]) {
						literalBuffered[node] = true
					}
				}
			}
		case *ast.CallExpr:
			if cureBuffers(cure, typeutil.StaticCallee(pass.TypesInfo, node)) {
				for _, argument := range node.Args {
					if literal := commandLiteral(pass, argument); literal != nil {
						literalBuffered[literal] = true
					}
					named, isNamed := commandOf(pass, argument)
					if isNamed && isCommand(pass.TypesInfo.TypeOf(argument)) {
						buffered[named] = true
					}
				}
			}
			selector, isSelector := ast.Unparen(node.Fun).(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			switch commandMethod(pass, selector) {
			case "Output", "CombinedOutput":
				if named, isNamed := commandOf(pass, selector.X); isNamed {
					buffered[named] = true
				}
			}
		}
		return true
	})
	for _, delay := range delays {
		flagged := delay.literal != nil && (literalBuffered[delay.literal] || literalReturned[delay.literal])
		if delay.reached && (buffered[delay.command] || returned[delay.command]) {
			flagged = true
		}
		if flagged {
			report(pass, cure, delay.identifier, excuses[table.Line(delay.identifier.Pos())])
		}
	}
}

// excusedLines are the lines a // WaitDelay: comment covers, with what it says: its own line, for a comment at the
// end of a line, and the line just after its comment group, so a reason may wrap onto lines of its own above the
// code. The marker opens the comment, after its slashes and spaces, so a sentence that only mentions it excuses
// nothing, and a block comment is not read, as Adamic reads none. Each comment's lines are read from table, without
// the FileSet's lock.
func excusedLines(table *positions.Table, file *ast.File) map[int]excuse {
	lines := map[int]excuse{}
	cover := func(line int, given excuse) {
		lines[line] = max(lines[line], given)
	}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text, isLineComment := strings.CutPrefix(comment.Text, "//")
			if !isLineComment {
				continue
			}
			reason, hasMarker := strings.CutPrefix(strings.TrimSpace(text), waitDelayMarker)
			if !hasMarker {
				continue
			}
			given := excuseWithReason
			if strings.TrimSpace(reason) == "" {
				given = excuseWithoutReason
			}
			cover(table.Line(comment.Pos()), given)
			cover(table.Line(group.End())+1, given)
		}
	}
	return lines
}

// commandOf names the command an expression reaches, through parentheses, & and *, or reports false when the
// expression is something no later line could name again, such as a call's result or an element of a slice.
func commandOf(pass *analysis.Pass, expression ast.Expr) (command, bool) {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		object := pass.TypesInfo.ObjectOf(expression)
		if object == nil {
			return command{}, false
		}
		return command{root: object}, true
	case *ast.SelectorExpr:
		selection, isSelection := pass.TypesInfo.Selections[expression]
		if !isSelection {
			// A qualified identifier: a package-level variable of another package.
			object := pass.TypesInfo.ObjectOf(expression.Sel)
			if object == nil {
				return command{}, false
			}
			return command{root: object}, true
		}
		if selection.Kind() != types.FieldVal {
			return command{}, false
		}
		inner, isNamed := commandOf(pass, expression.X)
		if !isNamed {
			return command{}, false
		}
		return command{root: inner.root, path: inner.path + "." + expression.Sel.Name}, true
	case *ast.StarExpr:
		return commandOf(pass, expression.X)
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return commandOf(pass, expression.X)
		}
	}
	return command{}, false
}

// commandLiteral is the exec.Cmd composite literal an expression is, through parentheses and &, or nil.
func commandLiteral(pass *analysis.Pass, expression ast.Expr) *ast.CompositeLit {
	unwrapped := ast.Unparen(expression)
	if unary, isUnary := unwrapped.(*ast.UnaryExpr); isUnary && unary.Op == token.AND {
		unwrapped = ast.Unparen(unary.X)
	}
	literal, isLiteral := unwrapped.(*ast.CompositeLit)
	if !isLiteral || !isCommand(pass.TypesInfo.TypeOf(literal)) {
		return nil
	}
	return literal
}

// assignedType is the type of the value an assignment gives its index'th target, a call's result included.
func assignedType(pass *analysis.Pass, assignment *ast.AssignStmt, index int) types.TypeAndValue {
	if len(assignment.Lhs) == len(assignment.Rhs) {
		return pass.TypesInfo.Types[assignment.Rhs[index]]
	}
	if len(assignment.Rhs) == 1 {
		if tuple, isTuple := pass.TypesInfo.TypeOf(assignment.Rhs[0]).(*types.Tuple); isTuple && index < tuple.Len() {
			return types.TypeAndValue{Type: tuple.At(index).Type()}
		}
	}
	return types.TypeAndValue{}
}

// buffers says a value given as Stdout or Stderr makes exec copy the output itself: anything but nil or an *os.File.
// A value whose type is unknown is not counted, since the rule would be guessing. An io.Writer may hold an *os.File,
// but the rule reads the type the code wrote, and exec copies anything else.
func buffers(value types.TypeAndValue) bool {
	if value.Type == nil || value.IsNil() {
		return false
	}
	return !isOsFile(value.Type)
}

func isOsFile(candidate types.Type) bool {
	pointer, isPointer := types.Unalias(candidate).(*types.Pointer)
	if !isPointer {
		return false
	}
	return isNamed(pointer.Elem(), "os", "File")
}

// isCommand says candidate is exec.Cmd or a pointer to it.
func isCommand(candidate types.Type) bool {
	if candidate == nil {
		return false
	}
	dereferenced := candidate
	if pointer, isPointer := types.Unalias(candidate).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	return isNamed(dereferenced, "os/exec", "Cmd")
}

func isNamed(candidate types.Type, packagePath string, name string) bool {
	named, isNamedType := types.Unalias(candidate).(*types.Named)
	if !isNamedType {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath && named.Obj().Name() == name
}

// isCommandField says field is one of exec.Cmd's own fields, by identity, so a struct of some other type with a field
// named WaitDelay is never mistaken for it.
func isCommandField(field *types.Var) bool {
	if !field.IsField() || field.Pkg() == nil || field.Pkg().Path() != "os/exec" {
		return false
	}
	object := field.Pkg().Scope().Lookup("Cmd")
	if object == nil {
		return false
	}
	structure, isStruct := object.Type().Underlying().(*types.Struct)
	if !isStruct {
		return false
	}
	for index := range structure.NumFields() {
		if structure.Field(index) == field {
			return true
		}
	}
	return false
}

// commandField is the name of the exec.Cmd field a selector reaches, an embedded exec.Cmd's included, or "".
func commandField(pass *analysis.Pass, selector *ast.SelectorExpr) string {
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.FieldVal {
		return ""
	}
	field, isVariable := selection.Obj().(*types.Var)
	if !isVariable || !isCommandField(field) {
		return ""
	}
	return field.Name()
}

// cureBuffers says callee is the cure package's Run or CombinedOutput, by the function object, so a function of the
// same name anywhere else, a package of the same name at another path included, is never mistaken for it. Capture
// is not among them: its outputs are *os.File pipes.
func cureBuffers(cure string, callee *types.Func) bool {
	if callee == nil || callee.Pkg() == nil || callee.Pkg().Path() != cure || callee.Signature().Recv() != nil {
		return false
	}
	return callee.Name() == "Run" || callee.Name() == "CombinedOutput"
}

// commandMethod is the name of the exec.Cmd method a selector calls, or "".
func commandMethod(pass *analysis.Pass, selector *ast.SelectorExpr) string {
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return ""
	}
	method, isFunc := selection.Obj().(*types.Func)
	if !isFunc {
		return ""
	}
	receiver := method.Signature().Recv()
	if receiver == nil || !isCommand(receiver.Type()) {
		return ""
	}
	return method.Name()
}

// report flags identifier unless its excuse gives a reason, with the message that names what is missing and the
// cure package to reach for.
func report(pass *analysis.Pass, cure string, identifier *ast.Ident, given excuse) {
	message := bufferedWaitDelayMessage
	switch given {
	case excuseWithReason:
		return
	case excuseWithoutReason:
		message = bufferedWaitDelayWithoutReasonMessage
	case excuseNone:
	}
	pass.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"cure": cure}),
	})
}
