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
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/concurrency-no-buffered-wait-delay"

// cureFlag is the option naming the cure package's import path.
const cureFlag = "cure"

// analyzerName is the name of the rule's analyzer.
const analyzerName = "concurrency_no_buffered_wait_delay"

// doc is the rule's documentation.
const doc = "Flags exec.Cmd.WaitDelay on a command whose output is buffered, or whose callers may buffer it, since " +
	"Wait then reports exec.ErrWaitDelay by chance when a slow run drains late, unless a // WaitDelay: <reason> " +
	"comment ends its line or sits directly above it. The cure option names the import path of the package whose " +
	"Capture is the cure and whose Run and CombinedOutput buffer; the rule reports nothing when cure is empty."

// listener reads the package's cure and each file's excused lines as the syntax dispatcher's walk enters the file,
// hears every top-level declaration and every node in one that sets, buffers or hands on a command, and judges each
// declaration's WaitDelays as the walk reaches the next declaration or leaves the file.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{positions.Analyzer},
	FileEnter: enterFile,
	Nodes: []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.GenDecl)(nil),
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
		(*ast.ReturnStmt)(nil),
		(*ast.CompositeLit)(nil),
		(*ast.CallExpr)(nil),
	},
	Node:     gather,
	FileExit: exitFile,
}

// NewAnalyzer builds the rule's analyzer with a flag set of its own, on a syntax dispatcher of its own, since a
// dispatcher names each rule once. The rule reads its cure option from the analyzer running it, so a test can run a
// configured and an unconfigured analyzer side by side without one setting a flag the other reads.
func NewAnalyzer() *analysis.Analyzer {
	return newAnalyzer(dispatch.NewSyntax())
}

// newAnalyzer registers the rule's listener on dispatcher and declares the cure option on the analyzer it returns,
// the flags the listener's context reads.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(Name, analyzerName, doc, &listener)
	// The cure package differs by repository, so the rule hardcodes none, and with no cure to name, a finding would
	// tell its reader to reach for nothing; an empty cure turns the rule off.
	analyzer.Flags.String(cureFlag, "", "import path of the package whose Capture is the cure for a buffered WaitDelay")
	return analyzer
}

// Analyzer runs the rule over one package, reporting what the shared syntax dispatcher's walk found for it.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{
	Name:      Name,
	Origin:    rule.OriginHouse,
	TypeAware: true,
	Analyzer:  Analyzer,
	Listener:  &listener,
	// ConfiguredBy lets the engine list the rule as not configured rather than quiet, since it reports nothing
	// until cure names the cure package.
	ConfiguredBy: []string{cureFlag},
}

var bufferedWaitDelayMessage = policy.MessageOf(Name, "bufferedWaitDelay")

var bufferedWaitDelayWithoutReasonMessage = policy.MessageOf(Name, "bufferedWaitDelayWithoutReason")

// excuse is what the comments on or above a line say about the WaitDelay there.
type excuse int

const (
	excuseNone excuse = iota
	// excuseWithoutReason is a marker with nothing after its colon, which tells the next reader nothing.
	excuseWithoutReason
	excuseWithReason
)

// packageState is what the rule reads once for its package and once for each file, and gathers over the top-level
// declaration being walked, held in its context's state.
type packageState struct {
	// cureRead is whether cure and table have been read, as the walk enters the package's first file.
	cureRead bool
	// cure is the cure package's import path, or "" when the rule judges nothing in the package: no cure is named, or
	// the package is the cure's own.
	cure string
	// table is the package's line table.
	table *positions.Table
	// excuses is excusedLines of the file being walked.
	excuses map[int]excuse
	// declaration is what the top-level declaration being walked holds of its commands, or nil before the file's
	// first one.
	declaration *declarationCommands
}

// declarationCommands is what one top-level declaration does with its commands, function literals inside it
// included, since a closure that buffers the command its function set WaitDelay on is the same mistake.
type declarationCommands struct {
	delays          []delay
	buffered        map[command]bool
	returned        map[command]bool
	literalCommands map[*ast.CompositeLit]command
	literalBuffered map[*ast.CompositeLit]bool
	literalReturned map[*ast.CompositeLit]bool
}

// enterFile reads the cure option and the line table the first time, and file's excused lines.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if !state.cureRead {
		state.cureRead = true
		// The flag is declared by newAnalyzer on every analyzer the rule is built as, so the option is always there.
		state.cure = context.Option(cureFlag).String()
		// The package that holds the cure bounds its own reads as it sees fit, and an external test package's path
		// carries _test while belonging to the cure as much as the package does.
		if strings.TrimSuffix(context.Package.Path(), "_test") == state.cure {
			state.cure = ""
		}
		if state.cure == "" {
			return
		}
		table, isTable := context.ResultOf(positions.Analyzer).(*positions.Table)
		if !isTable {
			// Invariant: positions' result is always its Table, so another is a mistake in the engine, and the
			// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
			panic(fmt.Sprintf("reading the line table: positions gave %T", context.ResultOf(positions.Analyzer)))
		}
		state.table = table
	}
	if state.cure == "" {
		return
	}
	state.excuses = excusedLines(state.table, file)
	state.declaration = nil
}

// exitFile judges the file's last top-level declaration.
func exitFile(context *rule.Context, _ *ast.File) {
	state := rule.State[packageState](context)
	if state.cure == "" {
		return
	}
	judgeDeclaration(context, state)
	state.declaration = nil
}

// gather reads node into what its top-level declaration does with its commands. A top-level declaration, the only
// node whose one ancestor is the file, judges the declaration before it and opens its own.
func gather(context *rule.Context, node ast.Node, stack []ast.Node) {
	state := rule.State[packageState](context)
	if state.cure == "" {
		return
	}
	if len(stack) == 1 {
		judgeDeclaration(context, state)
		state.declaration = &declarationCommands{
			buffered:        map[command]bool{},
			returned:        map[command]bool{},
			literalCommands: map[*ast.CompositeLit]command{},
			literalBuffered: map[*ast.CompositeLit]bool{},
			literalReturned: map[*ast.CompositeLit]bool{},
		}
		return
	}
	commands := state.declaration
	if commands == nil {
		return
	}
	// A literal is visited after the statement that holds it, so the statement says which command it is first.
	nameLiteral := func(target ast.Expr, value ast.Expr) {
		literal := commandLiteral(context, value)
		if literal == nil {
			return
		}
		if named, isNamed := commandOf(context, target); isNamed {
			commands.literalCommands[literal] = named
		}
	}
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
			switch commandField(context, selector) {
			case "WaitDelay":
				named, reached := commandOf(context, selector.X)
				commands.delays = append(commands.delays, delay{identifier: selector.Sel, command: named,
					reached: reached})
			case "Stdout", "Stderr":
				if !buffers(assignedType(context, node, index)) {
					continue
				}
				if named, isNamed := commandOf(context, selector.X); isNamed {
					commands.buffered[named] = true
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
			if literal := commandLiteral(context, result); literal != nil {
				commands.literalReturned[literal] = true
			}
			if named, isNamed := commandOf(context, result); isNamed && isCommand(context.TypesInfo.TypeOf(result)) {
				commands.returned[named] = true
			}
		}
	case *ast.CompositeLit:
		if !isCommand(context.TypesInfo.TypeOf(node)) {
			return
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
			field, isVariable := context.TypesInfo.Uses[key].(*types.Var)
			if !isVariable || !isCommandField(context, field) {
				continue
			}
			switch field.Name() {
			case "WaitDelay":
				named, reached := commands.literalCommands[node]
				commands.delays = append(commands.delays, delay{identifier: key, command: named, reached: reached,
					literal: node})
			case "Stdout", "Stderr":
				if buffers(context.TypesInfo.Types[pair.Value]) {
					commands.literalBuffered[node] = true
				}
			}
		}
	case *ast.CallExpr:
		if cureBuffers(state.cure, typeutil.StaticCallee(context.TypesInfo, node)) {
			for _, argument := range node.Args {
				if literal := commandLiteral(context, argument); literal != nil {
					commands.literalBuffered[literal] = true
				}
				named, isNamed := commandOf(context, argument)
				if isNamed && isCommand(context.TypesInfo.TypeOf(argument)) {
					commands.buffered[named] = true
				}
			}
		}
		selector, isSelector := ast.Unparen(node.Fun).(*ast.SelectorExpr)
		if !isSelector {
			return
		}
		switch commandMethod(context, selector) {
		case "Output", "CombinedOutput":
			if named, isNamed := commandOf(context, selector.X); isNamed {
				commands.buffered[named] = true
			}
		}
	}
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

// judgeDeclaration reports each WaitDelay the top-level declaration just walked sets on a command it buffers or
// returns. A WaitDelay's line is read from the package's line table, without the FileSet's lock.
func judgeDeclaration(context *rule.Context, state *packageState) {
	commands := state.declaration
	if commands == nil {
		return
	}
	for _, delay := range commands.delays {
		flagged := delay.literal != nil && (commands.literalBuffered[delay.literal] ||
			commands.literalReturned[delay.literal])
		if delay.reached && (commands.buffered[delay.command] || commands.returned[delay.command]) {
			flagged = true
		}
		if flagged {
			report(context, state.cure, delay.identifier, state.excuses[state.table.Line(delay.identifier.Pos())])
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
			reason, hasMarker := strings.CutPrefix(strings.TrimSpace(text), marker.WaitDelay)
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
func commandOf(context *rule.Context, expression ast.Expr) (command, bool) {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		object := context.TypesInfo.ObjectOf(expression)
		if object == nil {
			return command{}, false
		}
		return command{root: object}, true
	case *ast.SelectorExpr:
		selection, isSelection := context.TypesInfo.Selections[expression]
		if !isSelection {
			// A qualified identifier: a package-level variable of another package.
			object := context.TypesInfo.ObjectOf(expression.Sel)
			if object == nil {
				return command{}, false
			}
			return command{root: object}, true
		}
		if selection.Kind() != types.FieldVal {
			return command{}, false
		}
		inner, isNamed := commandOf(context, expression.X)
		if !isNamed {
			return command{}, false
		}
		return command{root: inner.root, path: inner.path + "." + expression.Sel.Name}, true
	case *ast.StarExpr:
		return commandOf(context, expression.X)
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return commandOf(context, expression.X)
		}
	}
	return command{}, false
}

// commandLiteral is the exec.Cmd composite literal an expression is, through parentheses and &, or nil.
func commandLiteral(context *rule.Context, expression ast.Expr) *ast.CompositeLit {
	unwrapped := ast.Unparen(expression)
	if unary, isUnary := unwrapped.(*ast.UnaryExpr); isUnary && unary.Op == token.AND {
		unwrapped = ast.Unparen(unary.X)
	}
	literal, isLiteral := unwrapped.(*ast.CompositeLit)
	if !isLiteral || !isCommand(context.TypesInfo.TypeOf(literal)) {
		return nil
	}
	return literal
}

// assignedType is the type of the value an assignment gives its index'th target, a call's result included.
func assignedType(context *rule.Context, assignment *ast.AssignStmt, index int) types.TypeAndValue {
	if len(assignment.Lhs) == len(assignment.Rhs) {
		return context.TypesInfo.Types[assignment.Rhs[index]]
	}
	if len(assignment.Rhs) == 1 {
		tuple, isTuple := context.TypesInfo.TypeOf(assignment.Rhs[0]).(*types.Tuple)
		if isTuple && index < tuple.Len() {
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
// named WaitDelay is never mistaken for it. The look into os/exec is recorded in the context's reads (#b2t9xmf).
func isCommandField(context *rule.Context, field *types.Var) bool {
	if !field.IsField() || field.Pkg() == nil || field.Pkg().Path() != "os/exec" {
		return false
	}
	object := context.LookupIn(field.Pkg(), "Cmd")
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
func commandField(context *rule.Context, selector *ast.SelectorExpr) string {
	selection, isSelection := context.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.FieldVal {
		return ""
	}
	field, isVariable := selection.Obj().(*types.Var)
	if !isVariable || !isCommandField(context, field) {
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
func commandMethod(context *rule.Context, selector *ast.SelectorExpr) string {
	selection, isSelection := context.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return ""
	}
	method, isFunction := selection.Obj().(*types.Func)
	if !isFunction {
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
func report(context *rule.Context, cure string, identifier *ast.Ident, given excuse) {
	message := bufferedWaitDelayMessage
	switch given {
	case excuseWithReason:
		return
	case excuseWithoutReason:
		message = bufferedWaitDelayWithoutReasonMessage
	case excuseNone:
	}
	context.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"cure": cure}),
	})
}
