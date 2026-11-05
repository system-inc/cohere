package core

import (
	"encoding/json"
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/comments"
	"github.com/system-inc/cohere/internal/lint/rule"
)

const messageLogicalAssignmentAssignmentId = "assignment"
const messageLogicalAssignmentUseLogicalOperatorId = "useLogicalOperator"
const messageLogicalAssignmentLogicalId = "logical"
const messageLogicalAssignmentConvertLogicalId = "convertLogical"
const messageLogicalAssignmentIfId = "if"
const messageLogicalAssignmentConvertIfId = "convertIf"
const messageLogicalAssignmentUnexpectedId = "unexpected"
const messageLogicalAssignmentSeparateId = "separate"

// logicalAssignmentAssignmentMessage is the `always` finding on `a = a || b`.
func logicalAssignmentAssignmentMessage(operator string) rule.Message {
	return rule.Message{
		Id: messageLogicalAssignmentAssignmentId,
		Description: fmt.Sprintf("Assignment (=) can be replaced with operator assignment (%s). "+
			"Writing the target on both sides makes a reader compare the two spellings to see "+
			"they match, and the shorthand also stops evaluating the target twice.", operator),
	}
}

// logicalAssignmentUseLogicalOperatorMessage is the suggestion offered beside that finding.
func logicalAssignmentUseLogicalOperatorMessage(operator string) rule.Message {
	return rule.Message{
		Id:          messageLogicalAssignmentUseLogicalOperatorId,
		Description: fmt.Sprintf("Convert this assignment to use the operator %s.", operator),
	}
}

// logicalAssignmentLogicalMessage is the `always` finding on `a || (a = b)`.
func logicalAssignmentLogicalMessage(operator string) rule.Message {
	return rule.Message{
		Id: messageLogicalAssignmentLogicalId,
		Description: fmt.Sprintf("Logical expression can be replaced with an assignment (%s). "+
			"The guarded assignment spells out a short circuit the operator already means.",
			operator),
	}
}

// logicalAssignmentConvertLogicalMessage is the suggestion offered beside that finding.
func logicalAssignmentConvertLogicalMessage(operator string) rule.Message {
	return rule.Message{
		Id: messageLogicalAssignmentConvertLogicalId,
		Description: fmt.Sprintf(
			"Replace this logical expression with an assignment with the operator %s.", operator),
	}
}

// logicalAssignmentIfMessage is the `always` finding on `if (a) a = b`, which fires only when
// `enforceForIfStatements` is set.
func logicalAssignmentIfMessage(operator string) rule.Message {
	return rule.Message{
		Id: messageLogicalAssignmentIfId,
		Description: fmt.Sprintf(
			"'if' statement can be replaced with a logical operator assignment with operator %s. "+
				"The condition and the assignment target are the same reference, which is what "+
				"the operator already tests.", operator),
	}
}

// logicalAssignmentConvertIfMessage is the suggestion offered beside that finding.
func logicalAssignmentConvertIfMessage(operator string) rule.Message {
	return rule.Message{
		Id: messageLogicalAssignmentConvertIfId,
		Description: fmt.Sprintf(
			"Replace this 'if' statement with a logical assignment with operator %s.", operator),
	}
}

// logicalAssignmentUnexpectedMessage is the `never` finding.
func logicalAssignmentUnexpectedMessage(operator string) rule.Message {
	return rule.Message{
		Id: messageLogicalAssignmentUnexpectedId,
		Description: fmt.Sprintf("Unexpected logical operator assignment (%s) shorthand. This "+
			"project writes the test and the assignment separately, so the short circuit is "+
			"visible rather than folded into one token.", operator),
	}
}

// logicalAssignmentSeparateMessage is the suggestion offered beside that finding.
func logicalAssignmentSeparateMessage() rule.Message {
	return rule.Message{
		Id:          messageLogicalAssignmentSeparateId,
		Description: "Separate the logical assignment into an assignment with a logical operator.",
	}
}

// LogicalAssignmentSetting is which of the two things this rule enforces.
type LogicalAssignmentSetting string

const (
	// LogicalAssignmentAlways requires the shorthand where one exists. Upstream's default.
	LogicalAssignmentAlways LogicalAssignmentSetting = "always"

	// LogicalAssignmentNever forbids it.
	LogicalAssignmentNever LogicalAssignmentSetting = "never"
)

// LogicalAssignmentOperatorsOptions carries upstream's two positional options.
//
// Require is a pointer for the reason `operator-assignment` states: upstream's
// `defaultOptions: ["always"]` means an absent option requires the shorthand, while the zero value
// of a plain setting is the empty string, which matches neither arm and makes the rule silent on
// every file it is meant to catch. A rule configured as a bare severity in the live config is
// exactly that case.
//
// EnforceForIfStatements defaults to `false`, so it is a plain bool: the zero value is already the
// upstream default and nothing is lost by an absent key.
//
// ReactCompiler is ours, not upstream's, and defaults to `true`: React Compiler refuses all three
// shorthands, so under `always` the rule is silent inside the functions the compiler compiles. A
// project that does not run the compiler sets it false to have those findings back. The default is
// the safe side because the fixer applies `||=` to a bare identifier unattended, and in a compiled
// component that rewrite costs the component its compilation. See the rule's doc comment.
type LogicalAssignmentOperatorsOptions struct {
	Require                *LogicalAssignmentSetting
	EnforceForIfStatements bool
	ReactCompiler          bool
}

// DefaultLogicalAssignmentOperatorsSettings is upstream's `["always"]` with the if-statement check
// off, and the React Compiler assumed on.
func DefaultLogicalAssignmentOperatorsSettings() LogicalAssignmentOperatorsOptions {
	always := LogicalAssignmentAlways
	return LogicalAssignmentOperatorsOptions{Require: &always, ReactCompiler: true}
}

// DecodeLogicalAssignmentOperatorsOptions turns upstream's option list into options.
//
// Upstream's schema is a two-element positional list whose first element is a string and whose
// second is an object, and whose `never` arm forbids the second element entirely. The rule registers
// with `DecodeOptionList`, so the list arrives whole. That is why this is hand-rolled rather than
// `rule.DecodeOptionsInto`: the generic helper decodes one object, and the default is not the zero
// value.
//
// An unrecognised mode is an error rather than a quiet fallback, because falling back would
// enforce a mode nobody asked for and say nothing about it. So is a key the second element's schema
// does not declare, and a third element.
func DecodeLogicalAssignmentOperatorsOptions(list []byte) (LogicalAssignmentOperatorsOptions, error) {
	positional, optionElementsError := rule.OptionElements(list, 2)
	if optionElementsError != nil {
		return DefaultLogicalAssignmentOperatorsSettings(), optionElementsError
	}

	settings := DefaultLogicalAssignmentOperatorsSettings()
	if len(positional) == 0 {
		return settings, nil
	}

	var mode string
	if decodeError := json.Unmarshal(positional[0], &mode); decodeError != nil {
		return DefaultLogicalAssignmentOperatorsSettings(), decodeError
	}
	setting := LogicalAssignmentSetting(mode)
	switch setting {
	case LogicalAssignmentAlways, LogicalAssignmentNever:
		settings.Require = &setting
	default:
		return DefaultLogicalAssignmentOperatorsSettings(),
			fmt.Errorf("logical-assignment-operators takes \"always\" or \"never\", got %s", mode)
	}

	if len(positional) < 2 {
		return settings, nil
	}

	// Upstream's schema permits the second element only beside `always`, so a configuration
	// pairing it with `never` is refused here rather than silently ignored.
	if setting == LogicalAssignmentNever {
		return DefaultLogicalAssignmentOperatorsSettings(),
			fmt.Errorf("logical-assignment-operators takes no options beside \"never\"")
	}

	var extra struct {
		EnforceForIfStatements *bool `json:"enforceForIfStatements"`
		ReactCompiler          *bool `json:"reactCompiler"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(positional[1], &extra); unmarshalOptionsError != nil {
		return DefaultLogicalAssignmentOperatorsSettings(),
			fmt.Errorf("logical-assignment-operators element 2: %w", unmarshalOptionsError)
	}
	if extra.EnforceForIfStatements != nil {
		settings.EnforceForIfStatements = *extra.EnforceForIfStatements
	}
	if extra.ReactCompiler != nil {
		settings.ReactCompiler = *extra.ReactCompiler
	}
	return settings, nil
}

// logicalAssignmentShorthand maps a logical operator to the assignment token that folds it in.
var logicalAssignmentShorthand = map[ast.Kind]string{
	ast.KindBarBarToken:                   "||=",
	ast.KindAmpersandAmpersandToken:       "&&=",
	ast.KindQuestionQuestionToken:         "??=",
	ast.KindBarBarEqualsToken:             "||=",
	ast.KindAmpersandAmpersandEqualsToken: "&&=",
	ast.KindQuestionQuestionEqualsToken:   "??=",
}

// logicalAssignmentLongForm maps a logical assignment token back to the operator it folded in,
// which is what the `never` expansion writes.
var logicalAssignmentLongForm = map[ast.Kind]string{
	ast.KindBarBarEqualsToken:             "||",
	ast.KindAmpersandAmpersandEqualsToken: "&&",
	ast.KindQuestionQuestionEqualsToken:   "??",
}

// LogicalAssignmentOperators requires the logical assignment shorthand, or forbids it.
//
// Valid under the default `always`:
//
//	a ||= b
//	a = b || c            the target is not the leftmost operand
//	a = a.b || c          a different reference
//	a || (b = c)
//	if (a) b = c          a different reference again
//
// Invalid:
//
//	a = a || b            -> a ||= b
//	a.b = a.b ?? c        -> a.b ??= c, offered as a suggestion rather than applied
//	a || (a = b)          -> a ||= b
//	if (a) a = b          -> a &&= b, only with enforceForIfStatements
//
// Under `never` the judgment reverses: `a ||= b` reports and expands to `a = a || b`.
//
// # The three shapes, and why they are one rule
//
// All three spell the same thing: read a reference, and write to it only if the read came back a
// particular way. The shorthand exists because each long form evaluates the reference twice, and a
// reference evaluated twice is a reference that can answer differently the second time.
//
// # Parentheses, which upstream cannot see
//
// Upstream's selector for the second shape is
// `LogicalExpression[right.type="AssignmentExpression"]`, and its own comment says the right side
// has to be parenthesized or the source would not parse. Its parser folds the parentheses away, so
// the node it matches is the assignment itself. Ours keeps them: measured in the probe, the right
// side of `a || (a = b)` is a KindParenthesizedExpression wrapping the assignment. Matching without
// unwrapping first would find nothing at all, silently, on every one of upstream's 37 cases for
// that shape. This is the brief's parenthesis divergence in its costs-findings direction.
//
// # The getter judgment, which decides fix against suggestion
//
// The repair is applied unattended only when collapsing two reads into one cannot change what runs.
// A bare identifier is safe. A member access is not, because `a.b` may be a getter and the
// shorthand reads it once where the long form read it twice, so upstream offers those as
// suggestions instead. That split is not cosmetic here either: `ReportNodeWithFixes` is applied by
// the edit engine with nobody watching, and `ReportNodeWithSuggestions` is not.
//
// The one place a member access is fixed is the second shape, where upstream additionally accepts
// an access whose object is an identifier, `this` or `super` and whose key is not itself an access.
// `a.b || (a.b = c)` is fixed; `a.b.c || (a.b.c = d)` is only suggested.
//
// # What `with` does to that judgment
//
// Inside a non-strict `with` block an identifier is not necessarily a variable: it may resolve to a
// property of the scrutinee, which may be a getter. So upstream's `cannotBeGetter` returns false
// for an identifier inside a `with`, and the safe and unsafe cases invert. Our parser accepts
// `with` in a TypeScript file and produces a KindWithStatement with no parse diagnostic, measured
// in the probe, so the corpus's five `with` cases are reproducible rather than unrepresentable.
//
// Strictness is what turns the guard off, because `with` is illegal in strict code. Upstream asks
// its scope analysis; here the question is whether the file is a module or carries a "use strict"
// prologue, which is what logicalAssignmentIsStrict answers.
//
// # Silent inside React Compiler's functions, where ESLint is not
//
// React Compiler refuses all three shorthands, `Handle ||= operators in AssignmentExpression` on
// babel-plugin-react-compiler 1.0.0, in a hook body and inside an effect callback alike, while the
// same rewrite in a plain helper compiles. So under `always` the rule says nothing inside a function
// the compiler compiles, decided by the react shelf's `IsInsideComponentOrHook`, the predicate the
// react rules already share rather than a second component detector. ESLint reports there; those
// are its false positives, and three ahra sites carried them (UsersRolesPage.tsx:121,
// RestEndpointNodeContent.tsx:335, WebSocketViaSharedWorkerProviderInternal.tsx:309).
//
// cohere's configuration does not say whether the compiler is on, since it lives in a Next config as
// a JavaScript value, so the rule takes it as its own `reactCompiler` option, on by default. Every
// tree we lint runs the compiler, and the fixer applies `||=` to a bare identifier unattended, so
// the default is the side where an unattended rewrite cannot cost a component its compilation.
var LogicalAssignmentOperators = rule.Rule[LogicalAssignmentOperatorsOptions]{
	Name: "logical-assignment-operators",
	// Two judgments resolve a name: whether `undefined` is the global rather than a local shadowing
	// it, and whether `Boolean` is. Upstream asks eslint-scope; the same question is asked here
	// through ctx.TypeChecker in logicalAssignmentIsShadowedInThisFile.
	//
	// Six of upstream's clean cases turn on it, and every one of them reports without the checker,
	// so the declaration is load-bearing rather than precautionary. The nil-checker fallback reads
	// every such name as the global, which is the right answer for source that does not shadow it
	// and is why the untyped fixtures agree on everything else.
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,
	Run: func(ruleContext rule.Context, options rule.Given[LogicalAssignmentOperatorsOptions]) rule.Listeners {
		settings, _ := options.Read()
		if settings.Require == nil {
			settings = DefaultLogicalAssignmentOperatorsSettings()
		}

		state := &logicalAssignmentState{
			ctx:      ruleContext,
			isStrict: logicalAssignmentIsStrict(ruleContext.SourceFile),
			// Only under `always`: `never` reports the shorthand and expands it, which is the
			// rewrite the compiler wants rather than the one it refuses.
			skipCompiledFunctions: *settings.Require == LogicalAssignmentAlways && settings.ReactCompiler,
		}

		if *settings.Require == LogicalAssignmentNever {
			return rule.Listeners{
				ast.KindBinaryExpression: state.prohibit,
			}
		}

		listeners := rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				state.checkAssignment(node)
				state.checkLogical(node)
			},
		}
		if settings.EnforceForIfStatements {
			listeners[ast.KindIfStatement] = state.checkIfStatement
		}
		return listeners
	},
}

// logicalAssignmentIsStrict answers upstream's global-scope `isStrict`.
//
// Two things make code strict at the top level: being a module, and a "use strict" prologue. A
// module is strict by specification, and our parser records moduleness on the source file rather
// than making the rule derive it. The prologue is a leading expression statement whose expression
// is the string literal, which is checked before any other statement because a prologue directive
// must be first.
//
// Upstream reads the same answer from eslint-scope. The brief warns that a rule whose verdict
// depends on source type can disagree with an ESLint measurement for that reason alone, since
// ESLint's flat config defaults every file to `module` while we derive it from the text. That
// affects only the `with` cases, because nothing else here consults strictness, and `with` is
// illegal in a module either way.
func logicalAssignmentIsStrict(sourceFile *ast.SourceFile) bool {
	if sourceFile == nil {
		return true
	}
	if ast.IsExternalModule(sourceFile) {
		return true
	}
	for _, statement := range sourceFile.Statements.Nodes {
		if statement.Kind != ast.KindExpressionStatement {
			return false
		}
		expression := statement.AsExpressionStatement().Expression
		if expression == nil || expression.Kind != ast.KindStringLiteral {
			return false
		}
		if expression.Text() == "use strict" {
			return true
		}
	}
	return false
}

// logicalAssignmentUnwrapParentheses strips every layer of parentheses from an expression.
//
// A loop with its own nil check rather than `ast.SkipParentheses`, which dereferences its argument.
// Several callers here can be handed a nil operand from a recovered parse.
func logicalAssignmentUnwrapParentheses(node *ast.Node) *ast.Node {
	current := node
	for current != nil && current.Kind == ast.KindParenthesizedExpression {
		current = current.AsParenthesizedExpression().Expression
	}
	return current
}

// logicalAssignmentIsReference is upstream's `isReference`.
//
// `undefined` is excluded by name, because the rule reads a bare `undefined` as the value rather
// than as a place to assign to. That is what keeps `a = undefined || b` from being read as a
// self-referential assignment when the target happens to be named `undefined`.
func logicalAssignmentIsReference(expression *ast.Node) bool {
	if expression == nil {
		return false
	}
	switch expression.Kind {
	case ast.KindIdentifier:
		return expression.Text() != "undefined"
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return true
	}
	return false
}

// logicalAssignmentIsSameReference is upstream's `isSameReference` called with no third argument,
// so `disableStaticComputedKey` is `false`.
//
// The flag changes real answers rather than being a detail, and this rule needs it false: the
// corpus asserts that `a.b = a['b'] ?? c` reports, which requires a dot access and a bracket access
// with the same static key to compare equal. `operator-assignment` in this package needs the
// opposite setting and carries its own copy for that reason; `yoda` carries a third at this
// setting. A fourth copy is added here rather than sharing yoda's because these are rule-local
// judgments about which flag upstream passed, and a shared helper would have to carry the flag as a
// parameter, which is one function two rules can silently disagree about the default of.
func logicalAssignmentIsSameReference(left *ast.Node, right *ast.Node) bool {
	unwrappedLeft := logicalAssignmentUnwrapParentheses(left)
	unwrappedRight := logicalAssignmentUnwrapParentheses(right)
	if unwrappedLeft == nil || unwrappedRight == nil {
		return false
	}

	// An optional access is never the same reference as a plain one, which upstream gets for free
	// from its ChainExpression wrapper and we have to state. See logicalAssignmentIsOptionalChain
	// for the measurement; without this the rule reports four of upstream's clean cases.
	if logicalAssignmentIsOptionalChain(unwrappedLeft) != logicalAssignmentIsOptionalChain(unwrappedRight) {
		return false
	}

	// The static-key equivalence, checked before the kind comparison because it is exactly what
	// lets a property access equal an element access.
	if logicalAssignmentIsAccess(unwrappedLeft) && logicalAssignmentIsAccess(unwrappedRight) {
		if name, known := logicalAssignmentStaticPropertyName(unwrappedLeft); known {
			otherName, otherKnown := logicalAssignmentStaticPropertyName(unwrappedRight)
			return otherKnown && name == otherName &&
				logicalAssignmentIsSameReference(
					logicalAssignmentAccessObject(unwrappedLeft), logicalAssignmentAccessObject(unwrappedRight))
		}
	}

	if unwrappedLeft.Kind != unwrappedRight.Kind {
		return false
	}

	switch unwrappedLeft.Kind {
	case ast.KindThisKeyword, ast.KindSuperKeyword:
		return true

	case ast.KindIdentifier, ast.KindPrivateIdentifier:
		return unwrappedLeft.Text() == unwrappedRight.Text()

	case ast.KindNonNullExpression:
		// TypeScript only, so upstream's switch has no arm for it and cannot: ESLint core's parser
		// rejects `a!.b = a!.b || c` outright with "Unexpected token !", so there is no upstream
		// verdict to reproduce and this is a decision rather than a port.
		//
		// Two identically written non-null assertions name the same place, because `!` is erased at
		// compile time and asserts nothing about the runtime value. Comparing through it is
		// therefore the same judgment the identifier arm makes, and declining would silently lose
		// findings on a shape that is ordinary in this tree while upstream's corpus can never show
		// it. The alternative reading, that `!` makes two references distinct, would also make
		// `a!.b = a!.b || c` unreportable while `a.b = a.b || c` reports, which is a difference in
		// type syntax rather than in what the code does.
		return logicalAssignmentIsSameReference(unwrappedLeft.AsNonNullExpression().Expression,
			unwrappedRight.AsNonNullExpression().Expression)

	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindRegularExpressionLiteral:
		// Upstream compares cooked literal values, and `Text()` is the cooked value for a string
		// and the canonical rendering for a number.
		return unwrappedLeft.Text() == unwrappedRight.Text()

	case ast.KindPropertyAccessExpression:
		leftAccess := unwrappedLeft.AsPropertyAccessExpression()
		rightAccess := unwrappedRight.AsPropertyAccessExpression()
		return logicalAssignmentIsSameReference(leftAccess.Name(), rightAccess.Name()) &&
			logicalAssignmentIsSameReference(leftAccess.Expression, rightAccess.Expression)

	case ast.KindElementAccessExpression:
		leftAccess := unwrappedLeft.AsElementAccessExpression()
		rightAccess := unwrappedRight.AsElementAccessExpression()
		return logicalAssignmentIsSameReference(
			leftAccess.ArgumentExpression, rightAccess.ArgumentExpression) &&
			logicalAssignmentIsSameReference(leftAccess.Expression, rightAccess.Expression)
	}
	return false
}

// logicalAssignmentIsOptionalChain answers whether an access is written with `?.` anywhere along
// its spine.
//
// This has no direct counterpart in upstream's code, and that is the point. Upstream wraps a whole
// optional chain in a ChainExpression node, and its `isSameReference` unwraps that wrapper on one
// side only when the two node types differ, which makes `a?.b` and `a.b` compare equal there. Our
// parser has no wrapper: `?.` is a QuestionDotToken hanging off the access, and the access kind is
// otherwise identical, so a structural comparison answers equal for a reason upstream's never
// reaches.
//
// Measured against the installed build rather than reasoned about, because the two implementations
// arrive at the answer by different routes and only the answer matters:
//
//	a?.b || (a.b = b)     silent
//	a.b || (a.b = b)      reports
//	if (a?.b) a.b = c     silent
//	if (!a?.b) a.b = c    silent
//
// So upstream treats an optional access and a plain one as different references, and a port that
// compares them structurally over-reports on every mixed pair. Four of upstream's clean cases are
// exactly that pair, which is how this was caught.
func logicalAssignmentIsOptionalChain(node *ast.Node) bool {
	for current := logicalAssignmentUnwrapParentheses(node); current != nil; {
		switch current.Kind {
		case ast.KindPropertyAccessExpression:
			access := current.AsPropertyAccessExpression()
			if access.QuestionDotToken != nil {
				return true
			}
			current = logicalAssignmentUnwrapParentheses(access.Expression)
		case ast.KindElementAccessExpression:
			access := current.AsElementAccessExpression()
			if access.QuestionDotToken != nil {
				return true
			}
			current = logicalAssignmentUnwrapParentheses(access.Expression)
		case ast.KindCallExpression:
			call := current.AsCallExpression()
			if call.QuestionDotToken != nil {
				return true
			}
			current = logicalAssignmentUnwrapParentheses(call.Expression)
		case ast.KindNonNullExpression:
			current = logicalAssignmentUnwrapParentheses(current.AsNonNullExpression().Expression)
		default:
			return false
		}
	}
	return false
}

// logicalAssignmentIsAccess answers whether a node is a member access of either spelling.
func logicalAssignmentIsAccess(node *ast.Node) bool {
	return node != nil && (node.Kind == ast.KindPropertyAccessExpression ||
		node.Kind == ast.KindElementAccessExpression)
}

// logicalAssignmentAccessObject reads the object out of either access spelling.
func logicalAssignmentAccessObject(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		return node.AsPropertyAccessExpression().Expression
	case ast.KindElementAccessExpression:
		return node.AsElementAccessExpression().Expression
	}
	return nil
}

// logicalAssignmentStaticPropertyName is upstream's `getStaticPropertyName`, narrowed to the
// spellings this corpus writes.
//
// A dotted access answers with its name, and a bracket access answers when its subscript is a
// string, a number or a template with no substitutions. A subscript through a variable or a call
// answers nothing, which is what keeps `a[b()] = a[b()] ?? c` from being read as the same reference
// twice: two calls are not knowably the same place.
func logicalAssignmentStaticPropertyName(access *ast.Node) (string, bool) {
	switch access.Kind {
	case ast.KindPropertyAccessExpression:
		name := access.AsPropertyAccessExpression().Name()
		if name == nil || name.Kind == ast.KindPrivateIdentifier {
			return "", false
		}
		return name.Text(), true
	case ast.KindElementAccessExpression:
		argument := logicalAssignmentUnwrapParentheses(
			access.AsElementAccessExpression().ArgumentExpression)
		if argument == nil {
			return "", false
		}
		switch argument.Kind {
		case ast.KindStringLiteral, ast.KindNumericLiteral,
			ast.KindNoSubstitutionTemplateLiteral:
			return argument.Text(), true
		}
	}
	return "", false
}

// isInsideWithBlock is upstream's function of the same name: does this node sit in the body of a
// `with`, as opposed to in its scrutinee.
//
// The body test matters. `with (a = a || b) {}` puts the assignment in the scrutinee, where the
// `with` scope is not yet in effect, so an identifier there is an ordinary variable and the fix is
// safe. The corpus pins both sides of that.
func logicalAssignmentIsInsideWithBlock(node *ast.Node) bool {
	for current := node; current != nil; current = current.Parent {
		parent := current.Parent
		if parent == nil {
			return false
		}
		if parent.Kind == ast.KindWithStatement &&
			parent.AsWithStatement().Statement == current {
			return true
		}
	}
	return false
}

// logicalAssignmentIsSimpleBase answers upstream's `baseTypes` set: an identifier, `this`, or
// `super`.
//
// Upstream reads `node.object.type` with no parenthesis skipping, and its parser has already
// removed them. Ours has not, so `(a).b` would answer false without the unwrap and true with it.
// Measured against the installed build: `(a).b || ((a).b = c)` is fixed there, so the parentheses
// are transparent to this question and the unwrap reproduces upstream rather than widening it.
func logicalAssignmentIsSimpleBase(object *ast.Node) bool {
	unwrapped := logicalAssignmentUnwrapParentheses(object)
	if unwrapped == nil {
		return false
	}
	switch unwrapped.Kind {
	case ast.KindIdentifier, ast.KindThisKeyword, ast.KindSuperKeyword:
		return true
	}
	return false
}

// logicalAssignmentContainsOptionalChain answers whether an expression is an optional chain, which
// is upstream's ChainExpression wrapper.
//
// Upstream refuses a computed key that is a ChainExpression, and its wrapper sits at the outermost
// position of the whole chain, so `a[b?.c]` is refused while `a[b.c]` is refused too by the
// member-expression arm above. The corpus pins `a[b?.c] || (a[b?.c] = d)` as suggested rather than
// fixed.
func logicalAssignmentContainsOptionalChain(node *ast.Node) bool {
	for current := node; current != nil; {
		switch current.Kind {
		case ast.KindPropertyAccessExpression:
			access := current.AsPropertyAccessExpression()
			if access.QuestionDotToken != nil {
				return true
			}
			current = access.Expression
		case ast.KindElementAccessExpression:
			access := current.AsElementAccessExpression()
			if access.QuestionDotToken != nil {
				return true
			}
			current = access.Expression
		case ast.KindCallExpression:
			call := current.AsCallExpression()
			if call.QuestionDotToken != nil {
				return true
			}
			current = call.Expression
		case ast.KindParenthesizedExpression:
			current = current.AsParenthesizedExpression().Expression
		default:
			return false
		}
	}
	return false
}

// logicalAssignmentCommentsInside reports whether any comment sits inside a span.
//
// Every one of the four fixers declines when one does, because each rewrites a stretch of source
// that would swallow the comment. Upstream calls `sourceCode.getCommentsInside`; the shelf's
// per-file scan answers the same question and is cached across every rule that asks.
func logicalAssignmentCommentsInside(ruleContext rule.Context, from int, to int) bool {
	for _, comment := range comments.ForFile(ruleContext) {
		if comment.Range.Pos() >= from && comment.Range.End() <= to {
			return true
		}
	}
	return false
}

// logicalAssignmentIsLogicalOperator answers whether a token is one of the three non-assignment
// logical operators, which is what separates `a = a || b` from `a = a + b`.
func logicalAssignmentIsLogicalOperator(operator ast.Kind) bool {
	switch operator {
	case ast.KindBarBarToken, ast.KindAmpersandAmpersandToken, ast.KindQuestionQuestionToken:
		return true
	}
	return false
}

// logicalAssignmentLeftmostOperand is upstream's `getLeftmostOperand`.
//
// `a = a || b || c` parses left-associatively, so the target sits at the bottom of a left spine of
// same-operator logical expressions. Walking down it is what lets that input report. The walk stops
// at a parenthesized operand, because `a = (a || b) || c` groups deliberately and upstream declines
// to re-associate across an explicit grouping. That stop is why the walk cannot simply unwrap.
func logicalAssignmentLeftmostOperand(sourceFile *ast.SourceFile, node *ast.Node) *ast.Node {
	operator := node.AsBinaryExpression().OperatorToken.Kind
	left := node.AsBinaryExpression().Left
	for left != nil {
		// Upstream carries an explicit `isParenthesised` check here and this port does not, because
		// the kind test below already answers it. A parenthesized operand is a
		// KindParenthesizedExpression rather than a KindBinaryExpression, so it fails that test and
		// is returned as the wrapper either way, which is what makes the reference comparison fail
		// and produces upstream's silence on `a = (a || b) || c`.
		//
		// This was a surviving mutant before it was a simplification. Removing the explicit branch
		// changed no verdict across the whole corpus, and rather than write a fixture for it the
		// two spellings were run side by side over nine shapes chosen to separate them, including
		// `a = ((a || b)) || c` and `a = a || (b) || c`. They agree on all nine, because the only
		// input that could distinguish them is a node that is both parenthesized and binary, which
		// the parser cannot produce. Subsumed rather than unreachable: the branch is reached, and
		// the branch below answers identically.
		//
		// The deliberate non-unwrapping here is the load-bearing part and it is easy to lose while
		// simplifying. Unwrapping the operand would make `a = (a || b) || c` report, which upstream
		// treats as an explicit grouping it refuses to re-associate across.
		if left.Kind != ast.KindBinaryExpression ||
			left.AsBinaryExpression().OperatorToken == nil ||
			left.AsBinaryExpression().OperatorToken.Kind != operator {
			return left
		}
		left = left.AsBinaryExpression().Left
	}
	return left
}

// logicalAssignmentTestIsLogical answers upstream's `ifNode.test.type !== "LogicalExpression"`.
//
// Read off the test node without unwrapping parentheses, matching upstream, whose parser records a
// parenthesized test as the inner node but whose `test` here would be the wrapper. Measured against
// the installed build: `if ((a.b === null || a.b === undefined)) a.b = c` is offered as a
// suggestion there, the same as the unparenthesized spelling, so not unwrapping would diverge.
// Unwrapping reproduces it.
func logicalAssignmentTestIsLogical(test *ast.Node) bool {
	unwrapped := logicalAssignmentUnwrapParentheses(test)
	if unwrapped == nil || unwrapped.Kind != ast.KindBinaryExpression {
		return false
	}
	return logicalAssignmentIsLogicalOperator(unwrapped.AsBinaryExpression().OperatorToken.Kind)
}

// logicalAssignmentIsLogicalNot answers whether a node is `!x`.
func logicalAssignmentIsLogicalNot(node *ast.Node) bool {
	return node != nil && node.Kind == ast.KindPrefixUnaryExpression &&
		node.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken
}

// logicalAssignmentIsDoubleComparison is upstream's `isDoubleComparison`: `? === ? || ? === ?`.
func logicalAssignmentIsDoubleComparison(expression *ast.Node) bool {
	if expression == nil || expression.Kind != ast.KindBinaryExpression {
		return false
	}
	binary := expression.AsBinaryExpression()
	if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindBarBarToken {
		return false
	}
	return logicalAssignmentIsStrictComparison(binary.Left) &&
		logicalAssignmentIsStrictComparison(binary.Right)
}

// logicalAssignmentIsStrictComparison answers whether a node is `x === y`.
func logicalAssignmentIsStrictComparison(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindBinaryExpression {
		return false
	}
	operator := node.AsBinaryExpression().OperatorToken
	return operator != nil && operator.Kind == ast.KindEqualsEqualsEqualsToken
}

// logicalAssignmentIsNullLiteral answers upstream's `astUtils.isNullLiteral`.
func logicalAssignmentIsNullLiteral(node *ast.Node) bool {
	unwrapped := logicalAssignmentUnwrapParentheses(node)
	return unwrapped != nil && unwrapped.Kind == ast.KindNullKeyword
}

// logicalAssignmentIfFixWouldGlue answers upstream's automatic semicolon insertion guard.
//
// Upstream reads two tokens: the one before the `if`, and the first token of the body. It refuses
// when the previous token is neither `;` nor `{` and the body's first token is neither an
// identifier nor a keyword. Both halves are needed: `a` on its own line followed by `if (b) a = c`
// is safe because the body starts with an identifier, which cannot continue the previous
// expression, while `(a) = c` starts with a parenthesis, which can.
//
// Read off the source text rather than a token stream, because the two characters that matter are
// unambiguous at that level and building a token API for them would be the larger change. The
// previous non-whitespace character before the `if` is compared directly, and the body's first
// character decides the second half: an identifier or keyword starts with a letter, `_`, `$` or a
// unicode escape, and nothing else does.
func logicalAssignmentIfFixWouldGlue(text string, ifStart int, bodyStart int) bool {
	previous := -1
	for index := ifStart - 1; index >= 0; index-- {
		if !logicalAssignmentIsSpace(text[index]) {
			previous = index
			break
		}
	}
	if previous < 0 {
		// Nothing precedes the statement, so nothing can capture it.
		return false
	}
	if text[previous] == ';' || text[previous] == '{' || text[previous] == '}' {
		return false
	}
	if bodyStart >= len(text) {
		return false
	}
	return !logicalAssignmentStartsIdentifierOrKeyword(text[bodyStart])
}

// logicalAssignmentStartsIdentifierOrKeyword answers whether a character can begin an identifier or
// a keyword, which is upstream's two token-type tests.
func logicalAssignmentStartsIdentifierOrKeyword(character byte) bool {
	switch {
	case character >= 'a' && character <= 'z':
		return true
	case character >= 'A' && character <= 'Z':
		return true
	case character == '_' || character == '$':
		return true
	case character >= 0x80:
		// A non-ASCII lead byte begins a unicode identifier. Treated as safe, matching upstream,
		// whose tokenizer would report Identifier for it.
		return true
	}
	return false
}

// logicalAssignmentIsSpace answers whether a byte is source whitespace.
func logicalAssignmentIsSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// logicalAssignmentEndsWithSemicolon answers whether a semicolon sits between two offsets.
func logicalAssignmentEndsWithSemicolon(text string, from int, to int) bool {
	end := min(to, len(text))
	for index := from; index < end; index++ {
		if text[index] == ';' {
			return true
		}
		if !logicalAssignmentIsSpace(text[index]) {
			return false
		}
	}
	return false
}

// logicalAssignmentOperatorPrecedence gives the precedence of the logical operator a logical
// assignment folds in, on the same scale operatorAssignmentPrecedence uses.
func logicalAssignmentOperatorPrecedence(operator ast.Kind) int {
	switch operator {
	case ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken:
		return operatorAssignmentBinaryPrecedence(ast.KindBarBarToken)
	case ast.KindAmpersandAmpersandEqualsToken:
		return operatorAssignmentBinaryPrecedence(ast.KindAmpersandAmpersandToken)
	}
	return -1
}

// logicalAssignmentIsMixableLogical answers whether an operator is one `??` may not sit beside.
func logicalAssignmentIsMixableLogical(operator ast.Kind) bool {
	switch operator {
	case ast.KindBarBarToken, ast.KindAmpersandAmpersandToken, ast.KindQuestionQuestionToken:
		return true
	}
	return false
}

// logicalAssignmentIsSurroundedByParentheses is upstream's `astUtils.isParenthesised`.
//
// It asks a purely token-level question: is the token immediately before this node an opening
// parenthesis and the token immediately after it a closing one. It does not ask whether those
// parentheses belong to the node, and that distinction is the whole reason this is written as a
// text scan rather than as a check on the parent's kind.
//
// The case that forced it: `fn(a || (a = 0))` fixes to `fn(a ||= 0)` with no added parentheses,
// while `fn(1, a || (a = 0))` fixes to `fn(1, (a ||= 0))` with them. The logical expression is a
// call argument in both. What differs is that a sole argument is preceded by the call's own `(`,
// which upstream's token test accepts as surrounding it, and a second argument is preceded by a
// comma, which it does not.
//
// That is a coincidence of tokens rather than a rule about argument positions, and a port that
// reads it as the latter gets four shapes wrong in the direction of writing too few parentheses.
// This was written that way first and a surviving mutant found it: measured against the installed
// build, `[a || (a = b)]` wraps to `[(a ||= b)]`, `({x: a || (a = b)})` to `({x: (a ||= b)})`, and
// `var q = a || (a = b)` to `var q = (a ||= b)`, all of which the semantic reading left bare.
//
// Only the two adjacent tokens matter, so this scans outward over whitespace and stops. A comment
// between the parenthesis and the node would make the scan wrong, but the fixer has already
// declined by then, since a comment anywhere inside the reported node stops every repair here.
func logicalAssignmentIsSurroundedByParentheses(text string, from int, to int) bool {
	before := -1
	for index := from - 1; index >= 0; index-- {
		if !logicalAssignmentIsSpace(text[index]) {
			before = index
			break
		}
	}
	if before < 0 || text[before] != '(' {
		return false
	}
	for index := to; index < len(text); index++ {
		if logicalAssignmentIsSpace(text[index]) {
			continue
		}
		return text[index] == ')'
	}
	return false
}
