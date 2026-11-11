package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
	"github.com/system-inc/cohere/internal/lint/ecmascript/module"
	"github.com/system-inc/cohere/internal/lint/ecmascript/property"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

const correctnessRequireDrainedChildOutputId = "childOutputNeverDrained"

// correctnessRequireDrainedChildOutputText is the rule's message, whose wording lives in
// `policy/messages/correctness-require-drained-child-output.json`.
var correctnessRequireDrainedChildOutputText = policy.MessageOf(
	"nexus/correctness-require-drained-child-output",
	correctnessRequireDrainedChildOutputId,
)

// correctnessRequireDrainedChildOutputMessage names the stream left unread and the event waited on,
// so the finding at the wait says which of the child's pipes fills.
func correctnessRequireDrainedChildOutputMessage(stream string, event string) rule.Message {
	return rule.Message{
		Id: correctnessRequireDrainedChildOutputId,
		Description: correctnessRequireDrainedChildOutputText.Render(map[string]string{
			"stream": stream,
			"event":  event,
		}),
	}
}

// CorrectnessRequireDrainedChildOutput reports a wait for a spawned child's `'close'` or `'exit'`
// when the child's stdout or stderr is a pipe nothing reads before that wait.
//
//	invalid: const child = spawn('ps', ['-eo', 'pid']); await once(child, 'close');
//	invalid: const child = spawn('ps'); if(verbose) child.stdout.on('data', print); await once(child, 'close');
//	invalid: const child = spawn('ps'); child.stdout.on('data', read); child.on('close', resolve);   (stderr)
//	valid:   const child = spawn('ps'); child.stdout.on('data', read); child.stderr.pipe(log);
//	         await once(child, 'close');
//	valid:   const child = spawn('ps', [], { stdio: 'ignore' }); await once(child, 'close');
//	valid:   const child = spawn('ps', [], { stdio: ['ignore', 'inherit', 'inherit'] }); await once(child, 'exit');
//	valid:   execFile('ps', [], function(error, stdout) {});                 (execFile reads both streams)
//	valid:   const child = spawn('ps'); wire(child); await once(child, 'close');   (handed away)
//
// # Where it came from
//
// Kirk's Mac ran out of process slots on 2026-11-10. One cause was a Swift app that waited for a
// child to exit before reading its piped output: when `ps` printed more than the 64 KB pipe buffer,
// the child blocked on its write and the parent blocked on the wait, forever, and 80 such children
// piled up. node has the same hazard. A stream left as a pipe and never read fills, the child stops
// in `write`, and `'close'` (and, for a child that writes before exiting, `'exit'`) never fires.
// Built as task `#pte6ec9`.
//
// # Which children it follows
//
// A call whose resolved signature is `spawn` declared in `declare module "node:child_process"` (or
// `"child_process"`), read through the resolution `nexus/security-no-interpolated-shell-command`
// uses, so a named, renamed, destructured or namespace import is seen and a local `spawn` never is.
// `exec`, `execFile` and the `Sync` functions are not producers: node's `execFile` (which `exec`
// calls) reads both streams into buffers on every child, and the `Sync` functions buffer too.
// `fork` inherits stdout and stderr unless told `silent`, and is left out.
//
// The child must be stored in a `const` declared from the call, local to a function or to a module
// file and not exported. A child chained on (`spawn(...).on('close', done)`), held in a `let` or a
// property, or declared any other way is not followed: each is a missed finding.
//
// # Which streams are pipes
//
// Read from the `options` argument, the parameter of that name in the resolved signature. With no
// options, or options without `stdio`, both streams are pipes. `stdio: 'pipe'` or `'overlapped'`
// makes both pipes. An array literal makes slot 1 stdout and slot 2 stderr, a slot that is absent,
// a hole, `undefined`, `null`, `'pipe'` or `'overlapped'` being a pipe, which is node's own reading
// in `getValidStdio`. A value is judged by its type, so a `const` holding `'ignore'` reads as
// `'ignore'`. Anything this cannot pin to a pipe (options in a variable, a spread, a `string`, a
// stream object, a file descriptor) is not a pipe, so the stream is never reported.
//
// # What drains a stream, and what waits
//
// Each reference to the child is read outward through parentheses, `!`, `as` and `satisfies`:
//
//   - **Drains**: on `child.stdout` or `child.stderr`, a listener for `'data'` or `'readable'` (or
//     for an event that is not one string literal), `.pipe`, `.resume`, `.read`, an async iterator,
//     handing the stream to any function (`readline.createInterface({ input: child.stdout })`,
//     `pipeline`), storing it, and anything else that is not on the short neutral list. Counting
//     more as a drain only silences.
//   - **Neutral on a stream**: `.setEncoding`, `.pause`, a listener for another literal event
//     (`'end'`, `'error'`), the listener-removal methods, a statement on its own, a truth test.
//   - **Waits**: `child.on`, `.once`, `.addListener`, `.prependListener` or `.prependOnceListener`
//     with `'close'` or `'exit'`, and `once(child, 'close')` from `node:events`. An `events.once`
//     whose promise is awaited where it is made is an awaited wait.
//   - **Escapes**: the child handed to any other function, returned, stored, copied, exported,
//     destructured, `.stdio` read, an emitter method read without being called. A helper may drain
//     the streams, so an escape anywhere in the file silences the child.
//   - **Neutral on the child**: `.pid`, `.kill()`, `.unref()`, `.stdin`, a listener for any other
//     event, a truth test.
//
// # How "before the wait" is decided
//
// Through the control-flow graph of the function (or module) the child is declared in. For each
// stream that is a pipe:
//
//   - **An awaited wait** stops its path: the code after it runs only once the child closes, so a
//     drain there is too late. It is reported when some path reaches it from the declaration without
//     passing a drain of that stream.
//   - **A listener wait** blocks nothing by itself, and a drain attached after it in the same run is
//     in time. It is reported when some path reaches it without a drain, and goes on from it to a
//     normal exit of the function, to an awaited wait, or to the declaration again on the next turn
//     of a loop, still without one.
//
// So a drain attached on only one branch of an `if` before the wait leaves the other path undrained,
// and reports. A path that leaves through an uncaught `throw` is not an exit.
//
// A `new Promise(function(resolve) { ... })` executor runs at once, so references inside one count
// at the place the executor is written in the enclosing function, its drains before its waits, and
// its waits are awaited when that `new Promise` is awaited where it is made. Branches inside an
// executor are not followed, which can only miss a finding. A reference inside any other nested
// function may run at any time: a drain there counts as draining the stream everywhere, and a wait
// there is not a wait. Both can only silence.
//
// One finding per stream per child, at the first such wait in the source, naming the stream.
//
// # No fix
//
// Whether the output matters is the author's call: collect it, pipe it to a log, or pass `'ignore'`
// or `'inherit'` in stdio. Any one of them written in by a fixer would be wrong for most sites.
var CorrectnessRequireDrainedChildOutput = rule.Rule[rule.NoOptions]{
	Name:             "nexus/correctness-require-drained-child-output",
	NeedsTypeChecker: true,

	// Left on the default, Contents. The producer test resolves an import through to its declaration,
	// and the dispatch scan cannot prove that read stops short of a body, so a shape-keyed replay is not
	// claimed (TestRulesClaimShapesOnlyWhereTheScanAllowsIt).

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			// Telling node's `spawn` from any other function of that name is a question for the
			// checker. With none, the rule declines rather than guess by name.
			return nil
		}
		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				correctnessRequireDrainedChildOutputScanFile(ruleContext, sourceFile)
			},
		}
	},
}

// correctnessRequireDrainedChildOutputStreams is a set of the child's output streams.
type correctnessRequireDrainedChildOutputStreams uint8

const (
	correctnessRequireDrainedChildOutputStdout correctnessRequireDrainedChildOutputStreams = 1 << iota
	correctnessRequireDrainedChildOutputStderr
)

// correctnessRequireDrainedChildOutputStreamNames are the streams in the order they are reported, by
// the member that reads each.
var correctnessRequireDrainedChildOutputStreamNames = []struct {
	stream correctnessRequireDrainedChildOutputStreams
	name   string
}{
	{correctnessRequireDrainedChildOutputStdout, "stdout"},
	{correctnessRequireDrainedChildOutputStderr, "stderr"},
}

// correctnessRequireDrainedChildOutputWait is one wait for the child's end: the call that waits,
// the event, and whether its promise is awaited where it is made.
type correctnessRequireDrainedChildOutputWait struct {
	call    *ast.Node
	event   string
	awaited bool
}

// correctnessRequireDrainedChildOutputUse is what one reference does with the child.
type correctnessRequireDrainedChildOutputUse struct {
	escaped bool
	drained correctnessRequireDrainedChildOutputStreams
	waits   []correctnessRequireDrainedChildOutputWait
}

// correctnessRequireDrainedChildOutputChild is one followed child: its name, the root it is declared
// in, the streams that are pipes, and what the scan learns before the graph is built.
type correctnessRequireDrainedChildOutputChild struct {
	name   *ast.Node
	symbol *ast.Symbol
	root   *ast.Node
	piped  correctnessRequireDrainedChildOutputStreams

	// drainedAnywhere are the streams drained inside a nested function the graph cannot place.
	drainedAnywhere correctnessRequireDrainedChildOutputStreams
	// placed are the nodes the graph must walk for every use of the child to be counted.
	placed  []*ast.Node
	dropped bool
}

// correctnessRequireDrainedChildOutputEvent is one thing the graph recorded about a child.
type correctnessRequireDrainedChildOutputEvent struct {
	child   *correctnessRequireDrainedChildOutputChild
	declare bool
	drained correctnessRequireDrainedChildOutputStreams
	wait    *correctnessRequireDrainedChildOutputWait
}

func correctnessRequireDrainedChildOutputScanFile(ruleContext rule.Context, sourceFile *ast.Node) {
	if !strings.Contains(ruleContext.SourceFile.Text(), "child_process") {
		// Resolving every call's signature cost 3.6 seconds of CPU on ahra, 15% of all rules, for the
		// 3% of files that can reach the module at all. Any way of reaching it, an import, an
		// `import()` type or `getBuiltinModule`, names it in the file, so this gate decides nothing:
		// the producer is still judged by its declaration. A child_process re-exported through a
		// file of the project's own is the one way past it, and is a missed finding.
		return
	}
	childrenByRoot := map[*ast.Node][]*correctnessRequireDrainedChildOutputChild{}
	var roots []*ast.Node
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if node.Kind == ast.KindCallExpression &&
			securityNoInterpolatedShellCommandFunction(ruleContext, node) == "spawn" {
			if child := correctnessRequireDrainedChildOutputBind(ruleContext, node); child != nil {
				if _, seen := childrenByRoot[child.root]; !seen {
					roots = append(roots, child.root)
				}
				childrenByRoot[child.root] = append(childrenByRoot[child.root], child)
			}
		}
		node.ForEachChild(visit)
		return false
	}
	sourceFile.ForEachChild(visit)

	for _, root := range roots {
		correctnessRequireDrainedChildOutputAnalyzeRoot(ruleContext, root, childrenByRoot[root])
	}
}

// correctnessRequireDrainedChildOutputBind is the child a `spawn` call is stored in, when the call
// is the whole initializer of a `const` the file owns and at least one of its streams is a pipe.
func correctnessRequireDrainedChildOutputBind(
	ruleContext rule.Context,
	call *ast.Node,
) *correctnessRequireDrainedChildOutputChild {
	piped := correctnessRequireDrainedChildOutputPipedStreams(ruleContext, call)
	if piped == 0 {
		return nil
	}
	declaration := correctnessRequireChildProcessErrorListenerOutermost(call).Parent
	if declaration == nil || declaration.Kind != ast.KindVariableDeclaration ||
		correctnessRequireChildProcessErrorListenerOutermost(call) != declaration.AsVariableDeclaration().Initializer {
		return nil
	}
	name := declaration.Name()
	list := declaration.Parent
	if name == nil || name.Kind != ast.KindIdentifier || list == nil || list.Kind != ast.KindVariableDeclarationList ||
		list.Flags&ast.NodeFlagsConst == 0 {
		return nil
	}
	if statement := list.Parent; statement != nil && statement.Kind == ast.KindVariableStatement {
		if module.IsExported(statement) {
			return nil
		}
		if statement.Parent != nil && statement.Parent.Kind == ast.KindSourceFile &&
			!ast.IsExternalModule(ruleContext.SourceFile) {
			// A script file's top-level `const` is a global another file may read.
			return nil
		}
	}
	symbol := ruleContext.TypeChecker.GetSymbolAtLocation(name)
	root := control_flow_graph.RootOf(declaration)
	if symbol == nil || root == nil {
		return nil
	}
	return &correctnessRequireDrainedChildOutputChild{name: name, symbol: symbol, root: root, piped: piped}
}

// correctnessRequireDrainedChildOutputPipedStreams reads which of stdout and stderr a `spawn` call
// leaves as pipes, from its `options` argument. Whatever it cannot pin to a pipe is not one.
func correctnessRequireDrainedChildOutputPipedStreams(
	ruleContext rule.Context,
	call *ast.Node,
) correctnessRequireDrainedChildOutputStreams {
	both := correctnessRequireDrainedChildOutputStdout | correctnessRequireDrainedChildOutputStderr
	signature := ruleContext.TypeChecker.GetResolvedSignature(call)
	if signature == nil || signature.Declaration() == nil {
		return 0
	}
	parameters := signature.Declaration().Parameters()
	var options *ast.Node
	if arguments := call.AsCallExpression().Arguments; arguments != nil {
		for index, argument := range arguments.Nodes {
			if argument.Kind == ast.KindSpreadElement {
				return 0
			}
			if index < len(parameters) && parameters[index].Name() != nil &&
				parameters[index].Name().Kind == ast.KindIdentifier && parameters[index].Name().Text() == "options" {
				options = ast.SkipParentheses(argument)
			}
		}
	}
	if options == nil {
		return both
	}
	if options.Kind != ast.KindObjectLiteralExpression {
		return 0
	}
	var stdio *ast.Node
	for _, member := range options.AsObjectLiteralExpression().Properties.Nodes {
		if member.Kind == ast.KindSpreadAssignment || member.Name() == nil {
			return 0
		}
		memberName, named := property.Name(member.Name(), property.Textual)
		if !named {
			if member.Name().Kind == ast.KindComputedPropertyName {
				// `[key]: 'ignore'` may be `stdio`.
				return 0
			}
			continue
		}
		if memberName != "stdio" {
			continue
		}
		switch member.Kind {
		case ast.KindPropertyAssignment:
			stdio = ast.SkipParentheses(member.AsPropertyAssignment().Initializer)
		case ast.KindShorthandPropertyAssignment:
			stdio = member.Name()
		default:
			return 0
		}
	}
	if stdio == nil {
		return both
	}
	if stdio.Kind != ast.KindArrayLiteralExpression {
		if correctnessRequireDrainedChildOutputIsPipe(ruleContext, stdio, false) {
			return both
		}
		return 0
	}
	elements := stdio.AsArrayLiteralExpression().Elements.Nodes
	var piped correctnessRequireDrainedChildOutputStreams
	for slot, stream := range map[int]correctnessRequireDrainedChildOutputStreams{
		1: correctnessRequireDrainedChildOutputStdout,
		2: correctnessRequireDrainedChildOutputStderr,
	} {
		spread := false
		for index := 0; index < len(elements) && index <= slot; index++ {
			if elements[index].Kind == ast.KindSpreadElement {
				spread = true
			}
		}
		switch {
		case spread:
		case slot >= len(elements), elements[slot].Kind == ast.KindOmittedExpression:
			piped |= stream
		case correctnessRequireDrainedChildOutputIsPipe(ruleContext, ast.SkipParentheses(elements[slot]), true):
			piped |= stream
		}
	}
	return piped
}

// correctnessRequireDrainedChildOutputIsPipe says whether a stdio value's type is exactly one value
// node reads as a pipe: `'pipe'`, `'overlapped'`, and in an array slot `undefined` or `null`.
func correctnessRequireDrainedChildOutputIsPipe(ruleContext rule.Context, value *ast.Node, inSlot bool) bool {
	valueType := ruleContext.TypeChecker.GetTypeAtLocation(value)
	if valueType == nil || type_checking.IsUnionType(valueType) {
		return false
	}
	if inSlot && type_checking.IsTypeFlagSet(valueType, checker.TypeFlagsUndefined|checker.TypeFlagsNull) {
		return true
	}
	if !type_checking.IsTypeFlagSet(valueType, checker.TypeFlagsStringLiteral) {
		return false
	}
	text, _ := valueType.AsLiteralType().Value().(string)
	return text == "pipe" || text == "overlapped"
}

// correctnessRequireDrainedChildOutputEventName is the event a listener registration or an
// `events.once` names, when its argument's type is one string literal, and empty otherwise.
func correctnessRequireDrainedChildOutputEventName(ruleContext rule.Context, argument *ast.Node) string {
	if argument == nil {
		return ""
	}
	unwrapped := ast.SkipParentheses(argument)
	switch unwrapped.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return unwrapped.Text()
	}
	argumentType := ruleContext.TypeChecker.GetTypeAtLocation(unwrapped)
	if argumentType == nil || type_checking.IsUnionType(argumentType) ||
		!type_checking.IsTypeFlagSet(argumentType, checker.TypeFlagsStringLiteral) {
		return ""
	}
	text, _ := argumentType.AsLiteralType().Value().(string)
	return text
}

// correctnessRequireDrainedChildOutputArgument is a call's argument at an index, or nil.
func correctnessRequireDrainedChildOutputArgument(call *ast.Node, index int) *ast.Node {
	arguments := call.AsCallExpression().Arguments
	if arguments == nil || len(arguments.Nodes) <= index {
		return nil
	}
	return arguments.Nodes[index]
}

// correctnessRequireDrainedChildOutputIsAwaited says whether an expression's value is awaited where
// it is made, through the wrappers that pass it on unchanged.
func correctnessRequireDrainedChildOutputIsAwaited(expression *ast.Node) bool {
	parent := correctnessRequireChildProcessErrorListenerOutermost(expression).Parent
	return parent != nil && parent.Kind == ast.KindAwaitExpression
}

// correctnessRequireDrainedChildOutputIsNeutralRead says whether the parent of an expression only
// looks at it: a statement on its own, `void`, `typeof`, `!`, a condition, an equality, `instanceof`
// or the left of `&&`.
func correctnessRequireDrainedChildOutputIsNeutralRead(current *ast.Node, parent *ast.Node) bool {
	switch parent.Kind {
	case ast.KindExpressionStatement, ast.KindVoidExpression, ast.KindTypeOfExpression:
		return true
	case ast.KindPrefixUnaryExpression:
		return parent.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken
	case ast.KindIfStatement:
		return parent.AsIfStatement().Expression == current
	case ast.KindWhileStatement:
		return parent.AsWhileStatement().Expression == current
	case ast.KindDoStatement:
		return parent.AsDoStatement().Expression == current
	case ast.KindConditionalExpression:
		return parent.AsConditionalExpression().Condition == current
	case ast.KindBinaryExpression:
		binary := parent.AsBinaryExpression()
		switch binary.OperatorToken.Kind {
		case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken,
			ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
			return true
		case ast.KindInstanceOfKeyword, ast.KindAmpersandAmpersandToken:
			return binary.Left == current
		}
	}
	return false
}

// correctnessRequireDrainedChildOutputFollowChild reads what one reference does with the child,
// following the chain of emitter methods that hand the child back.
func correctnessRequireDrainedChildOutputFollowChild(
	ruleContext rule.Context,
	identifier *ast.Node,
) correctnessRequireDrainedChildOutputUse {
	var use correctnessRequireDrainedChildOutputUse
	current := identifier
	for {
		current = correctnessRequireChildProcessErrorListenerOutermost(current)
		parent := current.Parent
		if parent == nil {
			use.escaped = true
			return use
		}
		switch parent.Kind {
		case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
			if (parent.Kind == ast.KindPropertyAccessExpression &&
				parent.AsPropertyAccessExpression().Expression != current) ||
				(parent.Kind == ast.KindElementAccessExpression &&
					parent.AsElementAccessExpression().Expression != current) {
				use.escaped = true
				return use
			}
			member, named := property.AccessedName(parent, property.Textual)
			if !named {
				use.escaped = true
				return use
			}
			switch member {
			case "stdout":
				if correctnessRequireDrainedChildOutputStreamIsDrained(ruleContext, parent) {
					use.drained |= correctnessRequireDrainedChildOutputStdout
				}
				return use
			case "stderr":
				if correctnessRequireDrainedChildOutputStreamIsDrained(ruleContext, parent) {
					use.drained |= correctnessRequireDrainedChildOutputStderr
				}
				return use
			case "stdio":
				// `child.stdio[1]` is stdout by another name, followed nowhere.
				use.escaped = true
				return use
			}
			registers := correctnessRequireChildProcessErrorListenerRegistrations[member]
			if !registers && !correctnessRequireChildProcessErrorListenerReturnsEmitter[member] {
				// `.pid`, `.kill()`, `.unref()`, `.stdin`: none of them hands the child on or waits.
				return use
			}
			call := parent.Parent
			if call == nil || call.Kind != ast.KindCallExpression || call.AsCallExpression().Expression != parent {
				// The method itself handed somewhere, bound or called later.
				use.escaped = true
				return use
			}
			if registers {
				switch event := correctnessRequireDrainedChildOutputEventName(
					ruleContext,
					correctnessRequireDrainedChildOutputArgument(call, 0),
				); event {
				case "close", "exit":
					use.waits = append(use.waits, correctnessRequireDrainedChildOutputWait{call: call, event: event})
				}
			}
			current = call
			continue
		case ast.KindCallExpression:
			if correctnessRequireDrainedChildOutputArgument(parent, 0) == current &&
				correctnessRequireDrainedChildOutputIsEventsOnce(ruleContext, parent) {
				switch event := correctnessRequireDrainedChildOutputEventName(
					ruleContext,
					correctnessRequireDrainedChildOutputArgument(parent, 1),
				); event {
				case "close", "exit":
					use.waits = append(use.waits, correctnessRequireDrainedChildOutputWait{
						call:    parent,
						event:   event,
						awaited: correctnessRequireDrainedChildOutputIsAwaited(parent),
					})
				}
				return use
			}
		}
		if !correctnessRequireDrainedChildOutputIsNeutralRead(current, parent) {
			use.escaped = true
		}
		return use
	}
}

// correctnessRequireDrainedChildOutputStreamIsDrained says whether what is done with
// `child.stdout` or `child.stderr` may consume it. Everything off the short neutral list does.
func correctnessRequireDrainedChildOutputStreamIsDrained(ruleContext rule.Context, access *ast.Node) bool {
	current := access
	for {
		current = correctnessRequireChildProcessErrorListenerOutermost(current)
		parent := current.Parent
		if parent == nil {
			return true
		}
		if parent.Kind != ast.KindPropertyAccessExpression && parent.Kind != ast.KindElementAccessExpression {
			return !correctnessRequireDrainedChildOutputIsNeutralRead(current, parent)
		}
		if (parent.Kind == ast.KindPropertyAccessExpression &&
			parent.AsPropertyAccessExpression().Expression != current) ||
			(parent.Kind == ast.KindElementAccessExpression &&
				parent.AsElementAccessExpression().Expression != current) {
			return true
		}
		member, named := property.AccessedName(parent, property.Textual)
		call := parent.Parent
		if !named || call == nil || call.Kind != ast.KindCallExpression ||
			call.AsCallExpression().Expression != parent {
			return true
		}
		switch {
		case correctnessRequireChildProcessErrorListenerRegistrations[member]:
			switch correctnessRequireDrainedChildOutputEventName(
				ruleContext,
				correctnessRequireDrainedChildOutputArgument(call, 0),
			) {
			case "", "data", "readable":
				return true
			}
		case member == "setEncoding", member == "pause",
			correctnessRequireChildProcessErrorListenerReturnsEmitter[member]:
		default:
			return true
		}
		// Each of these returns the stream, so the chain goes on.
		current = call
	}
}

// correctnessRequireDrainedChildOutputIsEventsOnce says whether a call is `once` from `node:events`
// (or `"events"`), declared there as a function or as a static method of EventEmitter.
func correctnessRequireDrainedChildOutputIsEventsOnce(ruleContext rule.Context, call *ast.Node) bool {
	signature := ruleContext.TypeChecker.GetResolvedSignature(call)
	if signature == nil || signature.Declaration() == nil {
		return false
	}
	declaration := signature.Declaration()
	if declaration.Name() == nil || declaration.Name().Kind != ast.KindIdentifier ||
		declaration.Name().Text() != "once" {
		return false
	}
	for current := declaration.Parent; current != nil; current = current.Parent {
		if current.Kind == ast.KindModuleDeclaration && current.Name() != nil &&
			current.Name().Kind == ast.KindStringLiteral {
			switch current.Name().Text() {
			case "events", "node:events":
				return true
			}
			return false
		}
	}
	return false
}

// correctnessRequireDrainedChildOutputIsExecutor says whether a function is the executor of a
// `new Promise(...)` on the global Promise, which runs before the constructor returns.
func correctnessRequireDrainedChildOutputIsExecutor(ruleContext rule.Context, function *ast.Node) bool {
	if function.Kind != ast.KindArrowFunction && function.Kind != ast.KindFunctionExpression {
		return false
	}
	outer := correctnessRequireChildProcessErrorListenerOutermost(function)
	construction := outer.Parent
	if construction == nil || construction.Kind != ast.KindNewExpression {
		return false
	}
	arguments := construction.AsNewExpression().Arguments
	if arguments == nil || len(arguments.Nodes) == 0 || arguments.Nodes[0] != outer {
		return false
	}
	callee := ast.SkipParentheses(construction.AsNewExpression().Expression)
	if callee.Kind != ast.KindIdentifier {
		return false
	}
	symbol := ruleContext.TypeChecker.GetSymbolAtLocation(callee)
	if symbol == nil || symbol.Name != "Promise" || len(symbol.Declarations) == 0 {
		return false
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file == nil || !file.IsDeclarationFile || ast.IsExternalModule(file) {
			// The default library declares Promise in global script files; a module's own Promise
			// is some other constructor, whose callback may run whenever it likes.
			return false
		}
	}
	return true
}

// correctnessRequireDrainedChildOutputPlace is where the graph of the root meets a reference: the
// reference itself in the root, the outermost Promise executor around it when every function between
// them is one, and nil when it sits in a function that may run at any time.
func correctnessRequireDrainedChildOutputPlace(
	ruleContext rule.Context,
	root *ast.Node,
	reference *ast.Node,
) *ast.Node {
	placed := reference
	for current := reference.Parent; current != nil && current != root; current = current.Parent {
		if control_flow_graph.IsRoot(current) || current.Kind == ast.KindClassDeclaration ||
			current.Kind == ast.KindClassExpression {
			if !correctnessRequireDrainedChildOutputIsExecutor(ruleContext, current) {
				return nil
			}
			placed = current
		}
	}
	return placed
}

// correctnessRequireDrainedChildOutputInType says whether a reference sits in a type position below
// the root, where it names a type and runs nothing.
func correctnessRequireDrainedChildOutputInType(node *ast.Node, root *ast.Node) bool {
	for current := node.Parent; current != nil && current != root; current = current.Parent {
		if current.Kind == ast.KindTypeQuery {
			return true
		}
	}
	return false
}

// correctnessRequireDrainedChildOutputEvents turns one use into the events the graph records, its
// drains before its waits, since a stream drained and then waited on in one breath is drained in
// time.
func correctnessRequireDrainedChildOutputEvents(
	child *correctnessRequireDrainedChildOutputChild,
	use correctnessRequireDrainedChildOutputUse,
	awaitedExecutor bool,
) (drains []correctnessRequireDrainedChildOutputEvent, waits []correctnessRequireDrainedChildOutputEvent) {
	if use.drained != 0 {
		drains = append(drains, correctnessRequireDrainedChildOutputEvent{child: child, drained: use.drained})
	}
	for index := range use.waits {
		wait := use.waits[index]
		wait.awaited = wait.awaited || awaitedExecutor
		waits = append(waits, correctnessRequireDrainedChildOutputEvent{child: child, wait: &wait})
	}
	return drains, waits
}

// correctnessRequireDrainedChildOutputAnalyzeRoot reads every reference to the children declared in
// one root, builds the root's graph once for all of them, and reports each undrained stream.
func correctnessRequireDrainedChildOutputAnalyzeRoot(
	ruleContext rule.Context,
	root *ast.Node,
	children []*correctnessRequireDrainedChildOutputChild,
) {
	bySymbol := map[*ast.Symbol]*correctnessRequireDrainedChildOutputChild{}
	byName := map[*ast.Node]*correctnessRequireDrainedChildOutputChild{}
	names := map[string]bool{}
	for _, child := range children {
		bySymbol[child.symbol] = child
		byName[child.name] = child
		names[child.name.Text()] = true
	}

	atReference := map[*ast.Node][]correctnessRequireDrainedChildOutputEvent{}
	executorDrains := map[*ast.Node][]correctnessRequireDrainedChildOutputEvent{}
	executorWaits := map[*ast.Node][]correctnessRequireDrainedChildOutputEvent{}
	var collect func(node *ast.Node) bool
	collect = func(node *ast.Node) bool {
		if node.Kind == ast.KindIdentifier && names[node.Text()] && byName[node] == nil &&
			!correctnessRequireDrainedChildOutputInType(node, root) {
			var child *correctnessRequireDrainedChildOutputChild
			escaped := false
			switch parent := node.Parent; {
			case parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment:
				// `{ child }` stores it in an object.
				child = bySymbol[ruleContext.TypeChecker.GetShorthandAssignmentValueSymbol(parent)]
				escaped = true
			case parent != nil && parent.Kind == ast.KindExportSpecifier:
				child = bySymbol[ruleContext.TypeChecker.GetExportSpecifierLocalTargetSymbol(parent)]
				escaped = true
			default:
				child = bySymbol[ruleContext.TypeChecker.GetSymbolAtLocation(node)]
			}
			if child != nil && !child.dropped {
				use := correctnessRequireDrainedChildOutputFollowChild(ruleContext, node)
				place := correctnessRequireDrainedChildOutputPlace(ruleContext, root, node)
				switch {
				case escaped || use.escaped:
					child.dropped = true
				case place == nil:
					// A function that may run at any time: its drain counts everywhere, its wait nowhere.
					child.drainedAnywhere |= use.drained
				case place == node:
					drains, waits := correctnessRequireDrainedChildOutputEvents(child, use, false)
					atReference[node] = append(drains, waits...)
					child.placed = append(child.placed, node)
				default:
					awaited := correctnessRequireDrainedChildOutputIsAwaited(
						correctnessRequireChildProcessErrorListenerOutermost(place).Parent,
					)
					drains, waits := correctnessRequireDrainedChildOutputEvents(child, use, awaited)
					executorDrains[place] = append(executorDrains[place], drains...)
					executorWaits[place] = append(executorWaits[place], waits...)
					child.placed = append(child.placed, place)
				}
			}
		}
		node.ForEachChild(collect)
		return false
	}
	collect(root)

	type builder = control_flow_graph.Builder[correctnessRequireDrainedChildOutputEvent]
	walked := map[*ast.Node]bool{}
	graph := control_flow_graph.Build(root, control_flow_graph.Hooks[correctnessRequireDrainedChildOutputEvent]{
		Expression: func(graphBuilder *builder, node *ast.Node) {
			drains, isExecutor := executorDrains[node]
			waits, hasWaits := executorWaits[node]
			if !isExecutor && !hasWaits {
				return
			}
			walked[node] = true
			for _, event := range drains {
				graphBuilder.Emit(event)
			}
			for _, event := range waits {
				graphBuilder.Emit(event)
			}
		},
		Read: func(graphBuilder *builder, node *ast.Node) {
			events, seen := atReference[node]
			if !seen {
				return
			}
			walked[node] = true
			for _, event := range events {
				graphBuilder.Emit(event)
			}
		},
		Write: func(graphBuilder *builder, node *ast.Node) {
			if child := byName[node]; child != nil {
				graphBuilder.Emit(correctnessRequireDrainedChildOutputEvent{child: child, declare: true})
			}
		},
	})

	final := make([]bool, len(graph.Blocks))
	for _, block := range graph.FinalBlocks {
		final[block.Index()] = true
	}
	for _, child := range children {
		if child.dropped {
			continue
		}
		unwalked := false
		for _, placed := range child.placed {
			if !walked[placed] {
				unwalked = true
			}
		}
		if unwalked {
			// A use the graph never met could be the drain, so the child is not judged.
			continue
		}
		for _, stream := range correctnessRequireDrainedChildOutputStreamNames {
			if child.piped&stream.stream == 0 || child.drainedAnywhere&stream.stream != 0 {
				continue
			}
			if wait := correctnessRequireDrainedChildOutputFirstUndrainedWait(
				graph,
				final,
				child,
				stream.stream,
			); wait != nil {
				ruleContext.ReportNode(wait.call, correctnessRequireDrainedChildOutputMessage(stream.name, wait.event))
			}
		}
	}
}

// correctnessRequireDrainedChildOutputPosition is a place in the graph: an event index in a block.
type correctnessRequireDrainedChildOutputPosition struct {
	block *control_flow_graph.Block[correctnessRequireDrainedChildOutputEvent]
	start int
}

// correctnessRequireDrainedChildOutputFirstUndrainedWait returns the first wait in the source that
// some path reaches with the stream undrained: an awaited one directly, a listener one when the path
// goes on from it to an exit, an awaited wait, or the declaration again, still undrained.
func correctnessRequireDrainedChildOutputFirstUndrainedWait(
	graph *control_flow_graph.Graph[correctnessRequireDrainedChildOutputEvent],
	final []bool,
	child *correctnessRequireDrainedChildOutputChild,
	stream correctnessRequireDrainedChildOutputStreams,
) *correctnessRequireDrainedChildOutputWait {
	var starts []correctnessRequireDrainedChildOutputPosition
	for _, block := range graph.Blocks {
		if !block.Reachable {
			continue
		}
		for index, event := range block.Events {
			if event.child == child && event.declare {
				starts = append(starts, correctnessRequireDrainedChildOutputPosition{block: block, start: index + 1})
			}
		}
	}

	// The waits reachable from a declaration with the stream not yet drained.
	var candidates []correctnessRequireDrainedChildOutputPosition
	correctnessRequireDrainedChildOutputWalk(
		starts,
		func(block *control_flow_graph.Block[correctnessRequireDrainedChildOutputEvent], index int) bool {
			event := block.Events[index]
			if event.child != child {
				return true
			}
			if event.declare || event.drained&stream != 0 {
				return false
			}
			if event.wait != nil {
				candidates = append(
					candidates,
					correctnessRequireDrainedChildOutputPosition{block: block, start: index},
				)
				// The code after an awaited wait runs only once the child has closed.
				return !event.wait.awaited
			}
			return true
		},
		nil,
	)

	var first *correctnessRequireDrainedChildOutputWait
	for _, candidate := range candidates {
		wait := candidate.block.Events[candidate.start].wait
		if first != nil && first.call.Pos() <= wait.call.Pos() {
			continue
		}
		undrained := wait.awaited
		if !undrained {
			correctnessRequireDrainedChildOutputWalk([]correctnessRequireDrainedChildOutputPosition{
				{block: candidate.block, start: candidate.start + 1},
			},
				func(block *control_flow_graph.Block[correctnessRequireDrainedChildOutputEvent], index int) bool {
					event := block.Events[index]
					if event.child != child || undrained {
						return !undrained
					}
					if event.declare || (event.wait != nil && event.wait.awaited) {
						// A fresh child on the next turn of a loop, or a wait that blocks: this child
						// was waited on and never drained.
						undrained = true
						return false
					}
					return event.drained&stream == 0
				},
				func(block *control_flow_graph.Block[correctnessRequireDrainedChildOutputEvent]) {
					if final[block.Index()] {
						undrained = true
					}
				})
		}
		if undrained {
			first = wait
		}
	}
	return first
}

// correctnessRequireDrainedChildOutputWalk visits the events reachable from the starts, in order
// along each path. visitEvent returns whether the path goes on past that event; completeBlock, when
// given, runs for every block a path walks to its end. Each block is entered from its top at most
// once, which is enough because the state a path carries is only whether it is still going.
func correctnessRequireDrainedChildOutputWalk(
	starts []correctnessRequireDrainedChildOutputPosition,
	visitEvent func(block *control_flow_graph.Block[correctnessRequireDrainedChildOutputEvent], index int) bool,
	completeBlock func(block *control_flow_graph.Block[correctnessRequireDrainedChildOutputEvent]),
) {
	queue := append([]correctnessRequireDrainedChildOutputPosition(nil), starts...)
	entered := map[int]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		stopped := false
		for index := current.start; index < len(current.block.Events); index++ {
			if !visitEvent(current.block, index) {
				stopped = true
				break
			}
		}
		if stopped {
			continue
		}
		if completeBlock != nil {
			completeBlock(current.block)
		}
		for _, successor := range current.block.Successors {
			if successor == nil || !successor.Reachable || entered[successor.Index()] {
				continue
			}
			entered[successor.Index()] = true
			queue = append(queue, correctnessRequireDrainedChildOutputPosition{block: successor})
		}
	}
}
