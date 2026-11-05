package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// OneVarMode is one of upstream's three settings for a declaration group.
//
// PascalCase literals, per this repository's convention for string-literal unions. The wire
// spellings upstream uses are lowercase and are mapped in the decoder, so the enum a reader sees at
// a call site and the JSON a config carries stay separate concerns.
type OneVarMode string

const (
	// OneVarAlways requires one declaration statement per scope for the group.
	OneVarAlways OneVarMode = "Always"
	// OneVarNever requires one declarator per statement.
	OneVarNever OneVarMode = "Never"
	// OneVarConsecutive allows several statements but merges ones that are adjacent.
	OneVarConsecutive OneVarMode = "Consecutive"
)

// is answers whether a half is set to a particular mode, treating absent as "no".
//
// Every upstream comparison is `options[key].initialized === MODE_ALWAYS` against a value that may
// be `undefined`, and `undefined === "always"` is false. Folding that into one method keeps the
// three-way absent/equal/different logic in one place rather than at each of the fourteen sites
// that ask.
func modeIs(mode *OneVarMode, wanted OneVarMode) bool {
	return mode != nil && *mode == wanted
}

// OneVarSettings is the decoded option surface.
//
// Five declaration kinds because upstream carries five: `var`, `let`, `const`, `using` and
// `await using`. The last is spelled `awaitUsing` in the config, matching upstream's schema.
type OneVarSettings struct {
	Variable   oneVarGroup `json:"Var"`
	Let        oneVarGroup
	Const      oneVarGroup
	Using      oneVarGroup
	AwaitUsing oneVarGroup

	// SeparateRequires reproduces upstream's `!!mode.separateRequires`, which only has meaning
	// alongside an `initialized: "always"` group.
	SeparateRequires bool
}

// oneVarObjectOptions is the wire shape of the object form.
//
// Every field is a pointer for the same reason `oneVarGroup`'s are: upstream distinguishes an
// absent key from a present one with `Object.hasOwn`, and the two take different paths. A plain
// string field would decode an absent `initialized` to `""`, which matches no mode and reads as
// "configured to something invalid" rather than as "not configured".
//
// The two object arms of upstream's schema are decoded into one struct rather than two, because
// they are disjoint by `additionalProperties: false` and a config carrying keys from both is
// refused below. Decoding into one struct and then refusing the mixture keeps the refusal in a
// place that can explain itself.
type oneVarObjectOptions struct {
	SeparateRequires *bool   `json:"separateRequires"`
	Variable         *string `json:"var"`
	Let              *string `json:"let"`
	Const            *string `json:"const"`
	Using            *string `json:"using"`
	AwaitUsing       *string `json:"awaitUsing"`
	Initialized      *string `json:"initialized"`
	Uninitialized    *string `json:"uninitialized"`
}

// oneVarModeFromWire maps upstream's lowercase spelling onto the enum.
//
// A value outside the three is an error rather than a silent fallback. Upstream gets that refusal
// from `meta.schema`'s enum before the rule ever runs; there is no schema layer here, so a bad
// value would otherwise decode to a mode matching nothing and produce a rule that registers, runs,
// and enforces nothing. That is the inert-but-configured shape section 7c of the porting standard
// exists to refuse.
func oneVarModeFromWire(wire string) (OneVarMode, error) {
	switch wire {
	case "always":
		return OneVarAlways, nil
	case "never":
		return OneVarNever, nil
	case "consecutive":
		return OneVarConsecutive, nil
	}
	return "", fmt.Errorf("one-var takes \"always\", \"never\" or \"consecutive\", got %q", wire)
}

// DefaultOneVarSettings is what a bare `"error"` gets.
//
// Upstream's `defaultOptions: ["always"]`, which is the string form applied to all five kinds. A
// rule configured without options is handed nil by the options registry, and returning the default
// here rather than a zero value is what keeps such a rule from registering on every file while
// enforcing nothing.
func DefaultOneVarSettings() OneVarSettings {
	always := OneVarAlways
	group := oneVarGroup{Uninitialized: &always, Initialized: &always}
	return OneVarSettings{Variable: group, Let: group, Const: group, Using: group, AwaitUsing: group}
}

// DecodeOneVarOptions reads the string or object form off the config.
//
// Hand rolled rather than routed through `rule.DecodeOptionsInto` because the wire shape is a
// union: upstream's single schema element is `oneOf` a string enum and two object shapes, and no
// single Go struct decodes all three.
//
// # The variadic hazard does not apply here, and it is worth saying why
//
// `internal/lint/configuration/configuration.go` keeps only `tuple[1]`, so a rule whose upstream
// option surface is variadic loses everything after the first value -- the defect measured on
// `id-match`, whose flag object vanished while the rule went on looking configured. `one-var`'s
// schema declares exactly one element, so the single value arrives intact and there is no second
// spelling to accept. Checked against `meta.schema` in the clone rather than assumed.
//
// # Absent is not a mode, at three separate layers
//
// The object form's keys are optional and an absent one means "leave this kind unconfigured", which
// upstream expresses as `undefined` flowing into `options[key]` and being caught by `if
// (!options[key]) return;`. So absence survives as nil through the wire struct, through
// `oneVarGroup`, and into the comparisons. Collapsing any of the three to a zero value makes
// `{ "var": "never" }` govern `let` and `const` too, and no fixture written from the string form
// could see it.
func DecodeOneVarOptions(raw []byte) (OneVarSettings, error) {
	if len(raw) == 0 {
		return DefaultOneVarSettings(), nil
	}

	// The string form first, because it is the common one and because a JSON string cannot also
	// decode as an object, so trying it costs nothing and cannot misfire.
	var wire string
	if decodeError := json.Unmarshal(raw, &wire); decodeError == nil {
		mode, modeError := oneVarModeFromWire(wire)
		if modeError != nil {
			return DefaultOneVarSettings(), modeError
		}
		group := oneVarGroup{Uninitialized: &mode, Initialized: &mode}
		return OneVarSettings{Variable: group, Let: group, Const: group, Using: group, AwaitUsing: group}, nil
	}

	var object oneVarObjectOptions
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &object); unmarshalOptionsError != nil {
		return DefaultOneVarSettings(),
			fmt.Errorf("one-var takes a string or an object, %q: %w", string(raw), unmarshalOptionsError)
	}

	// Upstream's two object arms are `additionalProperties: false`, so a config naming a
	// declaration kind and an initialization state is rejected by the schema before the rule runs.
	// Reproduced as a refusal rather than by picking one arm, because either choice silently
	// enforces something the author did not write.
	namesKind := object.Variable != nil || object.Let != nil || object.Const != nil ||
		object.Using != nil || object.AwaitUsing != nil
	namesState := object.Initialized != nil || object.Uninitialized != nil
	if namesKind && namesState {
		return DefaultOneVarSettings(), fmt.Errorf(
			"one-var takes either declaration kinds (var/let/const/using/awaitUsing) or "+
				"initialization states (initialized/uninitialized), not both: %q", string(raw))
	}

	settings := OneVarSettings{}
	if object.SeparateRequires != nil {
		settings.SeparateRequires = *object.SeparateRequires
	}

	// Each named kind sets both halves of its group, which is upstream's
	// `options.var = { uninitialized: mode.var, initialized: mode.var }`.
	assignKind := func(wire *string, group *oneVarGroup) error {
		if wire == nil {
			return nil
		}
		mode, oneVariableModeFromWireError := oneVarModeFromWire(*wire)
		if oneVariableModeFromWireError != nil {
			return oneVariableModeFromWireError
		}
		group.Uninitialized = &mode
		group.Initialized = &mode
		return nil
	}
	for _, pair := range []struct {
		wire  *string
		group *oneVarGroup
	}{
		{object.Variable, &settings.Variable},
		{object.Let, &settings.Let},
		{object.Const, &settings.Const},
		{object.Using, &settings.Using},
		{object.AwaitUsing, &settings.AwaitUsing},
	} {
		if assignKindError := assignKind(pair.wire, pair.group); assignKindError != nil {
			return DefaultOneVarSettings(), assignKindError
		}
	}

	// The state form sets one half across all five kinds, which is upstream's pair of
	// `Object.hasOwn` blocks. Note it overwrites rather than merges, matching the order upstream
	// applies them in -- though the mixture is refused above, so the two never meet in practice.
	if object.Uninitialized != nil {
		mode, oneVariableModeFromWireError := oneVarModeFromWire(*object.Uninitialized)
		if oneVariableModeFromWireError != nil {
			return DefaultOneVarSettings(), oneVariableModeFromWireError
		}
		settings.Variable.Uninitialized = &mode
		settings.Let.Uninitialized = &mode
		settings.Const.Uninitialized = &mode
		settings.Using.Uninitialized = &mode
		settings.AwaitUsing.Uninitialized = &mode
	}
	if object.Initialized != nil {
		mode, oneVariableModeFromWireError := oneVarModeFromWire(*object.Initialized)
		if oneVariableModeFromWireError != nil {
			return DefaultOneVarSettings(), oneVariableModeFromWireError
		}
		settings.Variable.Initialized = &mode
		settings.Let.Initialized = &mode
		settings.Const.Initialized = &mode
		settings.Using.Initialized = &mode
		settings.AwaitUsing.Initialized = &mode
	}

	return settings, nil
}

// oneVarUpstreamSentence maps a message id to the sentence upstream renders for it.
//
// Upstream's seven messages are four distinct sentences plus three that differ only in the
// interpolated type, and the four specific ones name the initialization state explicitly. Keeping
// the templates in one table rather than at the report site is what makes them checkable against
// upstream's `meta.messages` by eye.
//
// `%s` is upstream's `{{type}}`, which is the declaration spelling.
var oneVarUpstreamSentence = map[string]string{
	"combineUninitialized": "Combine this with the previous '%s' statement with uninitialized variables.",
	"combineInitialized":   "Combine this with the previous '%s' statement with initialized variables.",
	"splitUninitialized":   "Split uninitialized '%s' declarations into multiple statements.",
	"splitInitialized":     "Split initialized '%s' declarations into multiple statements.",
	"combine":              "Combine this with the previous '%s' statement.",
	"split":                "Split '%s' declarations into multiple statements.",
	"splitRequires":        "Split requires to be separated into a single block.",
}

// oneVarRender builds the message a finding carries.
//
// The upstream sentence comes first and this tree's explanation follows it, so the text a reader
// sees opens with exactly what ESLint says and the fixtures can assert that prefix.
//
// `splitRequires` interpolates nothing, which is why the table's value for it carries no verb.
func oneVarRender(message rule.Message, kind oneVarKind) rule.Message {
	template, found := oneVarUpstreamSentence[message.Id]
	if !found {
		return message
	}
	sentence := template
	if strings.Contains(template, "%s") {
		sentence = fmt.Sprintf(template, kind.text())
	}
	return rule.Message{Id: message.Id, Description: sentence + " " + message.Description}
}

var messageOneVarCombineUninitialized = rule.Message{
	Id: "combineUninitialized",
	Description: "This declares uninitialized variables in a second statement where the scope " +
		"already has one. Reading the declarations of a scope means finding every statement that " +
		"declares, and one list per scope makes that a single place to look.",
}

var messageOneVarCombineInitialized = rule.Message{
	Id: "combineInitialized",
	Description: "This declares initialized variables in a second statement where the scope " +
		"already has one. Reading the declarations of a scope means finding every statement that " +
		"declares, and one list per scope makes that a single place to look.",
}

var messageOneVarSplitUninitialized = rule.Message{
	Id: "splitUninitialized",
	Description: "This declares several uninitialized variables in one statement. One binding per " +
		"statement makes each declaration its own line to move, comment or delete, and keeps a " +
		"diff that touches one variable from touching the others.",
}

var messageOneVarSplitInitialized = rule.Message{
	Id: "splitInitialized",
	Description: "This declares several initialized variables in one statement. One binding per " +
		"statement makes each declaration its own line to move, comment or delete, and keeps a " +
		"diff that touches one variable from touching the others.",
}

var messageOneVarSplitRequires = rule.Message{
	Id: "splitRequires",
	Description: "This mixes require calls with other initializers in one declaration. Separating " +
		"the requires keeps the module's dependencies readable as a block rather than interleaved " +
		"with ordinary locals.",
}

var messageOneVarCombine = rule.Message{
	Id: "combine",
	Description: "This declares variables in a second statement where the scope already has one. " +
		"Reading the declarations of a scope means finding every statement that declares, and one " +
		"list per scope makes that a single place to look.",
}

var messageOneVarSplit = rule.Message{
	Id: "split",
	Description: "This declares several variables in one statement. One binding per statement " +
		"makes each declaration its own line to move, comment or delete, and keeps a diff that " +
		"touches one variable from touching the others.",
}

const (
	oneVarKindVar oneVarKind = iota
	oneVarKindLet
	oneVarKindConst
	oneVarKindUsing
	oneVarKindAwaitUsing
)

// oneVarKindOf classifies a VariableDeclarationList by its flags.
//
// # The composite flag, which is the trap this function exists to avoid
//
// `NodeFlagsAwaitUsing` is not a distinct bit. `ast/nodeflags.go` defines it as
// `NodeFlagsConst | NodeFlagsUsing`, because those two never co-occur on a single spelling and the
// pair is free to mean the third thing. So `flags&NodeFlagsAwaitUsing != 0` is true for a plain
// `const` and for a plain `using` as well, and a test written that way classifies every `const` in
// the tree as `await using`. Measured directly with a probe rather than read off the constant's
// name:
//
//	var           Let=false Const=false Using=false
//	let           Let=true  Const=false Using=false
//	const         Let=false Const=true  Using=false   <- AwaitUsing mask is TRUE here
//	using         Let=false Const=false Using=true    <- and TRUE here
//	await using   Let=false Const=true  Using=true
//
// So the discriminator is both bits set together, tested before either alone. A mutant using the
// composite mask is caught by 89 assertions.
func oneVarKindOf(list *ast.Node) oneVarKind {
	flags := list.Flags
	isConst := flags&ast.NodeFlagsConst != 0
	isUsing := flags&ast.NodeFlagsUsing != 0
	switch {
	case isConst && isUsing:
		return oneVarKindAwaitUsing
	case isUsing:
		return oneVarKindUsing
	case isConst:
		return oneVarKindConst
	case flags&ast.NodeFlagsLet != 0:
		return oneVarKindLet
	}
	return oneVarKindVar
}

// oneVarCounts is upstream's `countDeclarations` result.
type oneVarCounts struct {
	Uninitialized int
	Initialized   int
}

// oneVarStateFlags is one scope's record of what it has already seen.
//
// Upstream keeps `{ initialized, uninitialized }` on a function frame and the same pair per
// declaration kind on a block frame, plus a `required` flag that only `separateRequires` sets.
type oneVarStateFlags struct {
	Initialized   bool
	Uninitialized bool
	Required      bool
}

// oneVarBlockFrame is upstream's `blockStack` entry.
//
// `var` is deliberately absent: it lives on the function frame, which is what makes a `var` in a
// nested block collide with one in the enclosing function while a `let` does not.
type oneVarBlockFrame struct {
	Let        oneVarStateFlags
	Const      oneVarStateFlags
	Using      oneVarStateFlags
	AwaitUsing oneVarStateFlags
}

// oneVarIsFunctionScope answers which nodes upstream gives a `startFunction` listener.
//
// Upstream registers Program, FunctionDeclaration, FunctionExpression, ArrowFunctionExpression and
// StaticBlock. ESTree wraps every method, constructor and accessor body in a FunctionExpression, so
// that one entry covers all of them. Our parser has no such wrapper -- a method is
// KindMethodDeclaration and is both the member and the function -- so the four member kinds are
// named here explicitly. Measured with a probe: `class C { m() {} }` produces
// KindMethodDeclaration directly under KindClassDeclaration, with no function node between.
//
// Omitting them puts a `var` inside a method on the enclosing frame, so two methods each declaring
// `var x` report the second as a duplicate. The whole 296-case corpus is blind to that -- it writes
// zero class methods, constructors, getters or setters -- which is why
// `TestOneVarStaysSilentOnTypeScriptShapesUpstreamCannotWrite` exists.
//
// A namespace body is deliberately not here. Measured: two sibling namespaces each declaring a
// `const` do report upstream, because a TSModuleBlock is not a function scope.
func oneVarIsFunctionScope(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindSourceFile,
		ast.KindFunctionDeclaration,
		ast.KindFunctionExpression,
		ast.KindArrowFunction,
		ast.KindMethodDeclaration,
		ast.KindConstructor,
		ast.KindGetAccessor,
		ast.KindSetAccessor,
		ast.KindClassStaticBlockDeclaration:
		return true
	}
	return false
}

// oneVarIsBlockScope answers which nodes upstream gives a `startBlock` listener.
//
// Upstream's list is BlockStatement, ForStatement, ForInStatement, ForOfStatement and
// SwitchStatement.
//
// # The static block's inner Block is deliberately excluded
//
// Upstream's `StaticBlock` node is both the function scope and the statement list, so it pushes one
// function frame and (via startFunction) one block frame. Our parser puts a KindBlock inside
// KindClassStaticBlockDeclaration, which would push a second block frame for the same statements.
//
// Removing this guard is an equivalent mutant, and that is established structurally rather than
// inferred from its survival. A probe printing the children of a KindClassStaticBlockDeclaration
// shows it has exactly one child, its Block, and that every statement lives inside that Block. So
// there is no position where a statement sits in the outer frame but not the inner one: the two
// frames span identical statement sets and no verdict can differ between them.
//
// Kept regardless, because it is upstream's stack depth rather than ours, and a divergence chosen
// for being unobservable today is the kind that stops being unobservable quietly. Not kept as a
// claim that any fixture discriminates it -- none does, and the sweep says so.
func oneVarIsBlockScope(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindBlock:
		if node.Parent != nil && node.Parent.Kind == ast.KindClassStaticBlockDeclaration {
			return false
		}
		return true
	case ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement,
		ast.KindSwitchStatement:
		return true
	}
	return false
}

// oneVarStatementOf returns the node that plays the part of ESTree's VariableDeclaration.
//
// ESTree's VariableDeclaration is the statement and holds every declarator. Ours splits that into a
// VariableStatement wrapping a VariableDeclarationList, except in a for-head where the list stands
// alone. So the statement-shaped node is the list's parent when that parent is a VariableStatement,
// and the list itself otherwise.
//
// This is the node upstream reports on and the node it indexes within `parent.body`, so getting it
// wrong moves every span and breaks every consecutive verdict at once.
func oneVarStatementOf(list *ast.Node) *ast.Node {
	if list.Parent != nil && list.Parent.Kind == ast.KindVariableStatement {
		return list.Parent
	}
	return list
}

// oneVarStatementList returns the siblings upstream's `parent.body` would hold, or nil.
//
// Upstream's consecutive check reads
//
//	(parent.body && parent.body.length > 0 && parent.body.indexOf(node)) || 0
//
// so this answers "what does `parent.body` contain", which is narrower than "is this a statement
// list".
//
// # A switch clause is deliberately absent, and it is the one container where the two disagree
//
// An ESTree SwitchCase keeps its statements in `.consequent`, not `.body`. So `parent.body` is
// undefined there, the whole index expression is 0, and the consecutive arm never fires inside a
// switch case at all. Measured against ESLint 10.10.0: two adjacent `let`s report `combine` in a
// block and report nothing in a `case` or a `default`, while the same clause still reports under
// `always` and `never`, both of which reach the scope stack rather than the sibling index.
//
// Ours calls that node a statement list, because it is one, so returning its statements here made
// the port report where upstream is silent. The corpus writes three switch cases and none of them
// is under `consecutive`, so nothing in it could see the divergence.
//
// `oneVarIsInStatementList` does include the clause, and that is not an inconsistency: it answers
// upstream's `isInStatementList`, which tests `astUtils.STATEMENT_LIST_PARENTS` -- a set that names
// SwitchCase explicitly. Two different questions, two different answers, one node.
//
// # Each kind gets its own arm, and that is not stylistic
//
// `As*()` accessors are interface conversions rather than casts, so calling `AsBlock()` on a
// KindSourceFile panics instead of answering nil. A shared `case KindA, KindB:` arm reaching for one
// accessor is exactly the shape that crashes, and the walk recovers per file rather than per rule,
// so one such panic costs every rule its verdict on that file.
func oneVarStatementList(container *ast.Node) []*ast.Node {
	if container == nil {
		return nil
	}
	switch container.Kind {
	case ast.KindSourceFile:
		return container.AsSourceFile().Statements.Nodes
	case ast.KindBlock:
		return container.AsBlock().Statements.Nodes
	case ast.KindModuleBlock:
		return container.AsModuleBlock().Statements.Nodes
	}
	return nil
}

// oneVarIsInStatementList answers upstream's `isInStatementList`, which gates the split fixer.
//
// Upstream tests whether the node's parent is one of Program, BlockStatement, StaticBlock or
// SwitchCase, which is exactly "this statement is a member of a statement list". `if (foo) var x, y;`
// fails it, and upstream declines to autofix there because splitting would move the second
// declaration outside the `if`.
//
// Upstream passes the ExportNamedDeclaration when there is one, because in ESTree that wrapper is
// the list member. Our parser has no such wrapper -- `export var a;` is a VariableStatement carrying
// an export modifier, sitting directly in the statement list -- so the statement node is already the
// right thing to test and the branch collapses.
//
// The switch clauses are here, unlike in `oneVarStatementList`. See that function's note.
func oneVarIsInStatementList(statement *ast.Node) bool {
	if statement == nil || statement.Parent == nil {
		return false
	}
	switch statement.Parent.Kind {
	case ast.KindSourceFile, ast.KindBlock, ast.KindModuleBlock,
		ast.KindCaseClause, ast.KindDefaultClause:
		return true
	}
	return false
}

// oneVarDeclarators returns the list's declarators.
func oneVarDeclarators(list *ast.Node) []*ast.Node {
	return list.AsVariableDeclarationList().Declarations.Nodes
}

// oneVarCountDeclarations is upstream's `countDeclarations`.
func oneVarCountDeclarations(declarators []*ast.Node) oneVarCounts {
	counts := oneVarCounts{}
	for _, declarator := range declarators {
		if declarator.AsVariableDeclaration().Initializer == nil {
			counts.Uninitialized++
		} else {
			counts.Initialized++
		}
	}
	return counts
}

// oneVarIsRequire is upstream's `isRequire`.
//
// Upstream reads `decl.init.callee.name === "require"`, which is a name test rather than a
// resolution: a locally declared `function require()` satisfies it, and so does a shadowed one.
// Reproduced as written, because the rule's judgment is the spelling. Measured against the
// installed rule, which reports on `function require(x) { return x; } var foo = require('foo'), bar;`.
//
// `callee.name` is `undefined` for any callee that is not an identifier -- a member access, a
// parenthesized expression -- and `undefined === "require"` is false, so those are not requires.
// Testing the kind first is what reproduces that without dereferencing something that has no name:
// measured, `a.require('foo')` is not a require upstream.
//
// Every one of the corpus's ten `separateRequires` cases calls `require` and nothing else, so a
// mutant answering true for every call survives all 296. See
// `TestOneVarRequireIsASpellingNotAResolution`.
func oneVarIsRequire(declarator *ast.Node) bool {
	initializer := declarator.AsVariableDeclaration().Initializer
	if initializer == nil || initializer.Kind != ast.KindCallExpression {
		return false
	}
	callee := initializer.AsCallExpression().Expression
	if callee == nil || callee.Kind != ast.KindIdentifier {
		return false
	}
	return callee.Text() == "require"
}

// oneVarAnyRequire and oneVarAllRequire are upstream's `.some(isRequire)` and `.every(isRequire)`.
//
// Note `every` on an empty slice is true in JavaScript, and Go's loop form gives the same answer,
// so a list with no declarators is "all requires" in both. That only ever combines with `some`
// being false, so the pair reads as "no requires" either way.
func oneVarAnyRequire(declarators []*ast.Node) bool {
	for _, declarator := range declarators {
		if oneVarIsRequire(declarator) {
			return true
		}
	}
	return false
}

func oneVarAllRequire(declarators []*ast.Node) bool {
	for _, declarator := range declarators {
		if !oneVarIsRequire(declarator) {
			return false
		}
	}
	return true
}

// oneVarIndexOf finds a statement among its siblings by node identity.
//
// Upstream uses `parent.body.indexOf(node)`, which is identity in JavaScript too. Position would
// also work here and is the wrong instrument: two nodes can share a Pos when one is synthesized,
// and identity is what upstream actually asks.
func oneVarIndexOf(siblings []*ast.Node, statement *ast.Node) int {
	for index, sibling := range siblings {
		if sibling == statement {
			return index
		}
	}
	return -1
}

// oneVarDeclarationListOfStatement returns the declaration list a statement holds, or nil.
//
// Upstream's test is `previousNode.type === "VariableDeclaration"`, which in ESTree is the
// statement itself. Ours has to step through the VariableStatement to reach the list where the kind
// flags live.
func oneVarDeclarationListOfStatement(statement *ast.Node) *ast.Node {
	if statement == nil || statement.Kind != ast.KindVariableStatement {
		return nil
	}
	// An exported previous sibling is an ExportNamedDeclaration upstream, whose `kind` is
	// `undefined` rather than "const", so `previousNode.kind === type` is false and both the
	// consecutive check and the join repair decline against it. Ours has no wrapper, so the export
	// modifier is what stands in for that node's identity.
	//
	// Measured: `export const a = 1; const b = 2;` under "always" reports and returns `output:
	// null` upstream, while without this the port repaired it to `export const a = 1,  b = 2;` --
	// which moves a binding into an export and changes the module's public surface. The corpus
	// cannot reach it: its exported cases are all under "never".
	if oneVarIsExported(statement) {
		return nil
	}
	list := statement.AsVariableStatement().DeclarationList
	if list == nil || list.Kind != ast.KindVariableDeclarationList {
		return nil
	}
	return list
}

// oneVarIsForStatementInitializer answers upstream's `parent.type !== "ForStatement" || parent.init !== node`.
//
// Only the classic three-part `for` head is excluded. A `for...in` or `for...of` head is a different
// parent kind and is not excluded here, which is upstream's behaviour and is why the `always` arm
// carries its own separate for-in/for-of guard.
func oneVarIsForStatementInitializer(list *ast.Node) bool {
	parent := list.Parent
	if parent == nil || parent.Kind != ast.KindForStatement {
		return false
	}
	return parent.AsForStatement().Initializer == list
}

// oneVarIsForInOrOfLeft answers upstream's `node.parent.left === node && (ForIn || ForOf)`.
//
// This guard sits inside the `always`/uninitialized arm only, and it suppresses the report entirely
// rather than just the fix. `for (var x in y) {}` declares an uninitialized binding that cannot be
// merged with anything, so reporting it would be unactionable.
func oneVarIsForInOrOfLeft(list *ast.Node) bool {
	parent := list.Parent
	if parent == nil {
		return false
	}
	// Each kind gets its own arm: `As*()` is an interface conversion and an accessor named for one
	// kind panics on a node of another. These two happen to share `AsForInOrOfStatement`, which is
	// why they may safely resolve to the same call -- an accessor named for either one alone would
	// panic on the other, which is the shape that crashes a whole file.
	switch parent.Kind {
	case ast.KindForInStatement:
		return parent.AsForInOrOfStatement().Initializer == list
	case ast.KindForOfStatement:
		return parent.AsForInOrOfStatement().Initializer == list
	}
	return false
}

// oneVarIsExported answers whether a statement carries an `export` modifier.
//
// ESTree wraps an exported declaration in an ExportNamedDeclaration and upstream tests
// `declaration.parent.type`. Our parser has no wrapper: the modifier hangs off the statement, which
// is why this reads modifiers rather than the parent kind.
//
// # The wrapper changes what the rule does, not only how it is spelled
//
// Upstream reaches an exported declaration's siblings through `declaration.parent.parent.body` in
// the fixer and `parent.body` in the consecutive check. For an export, `declaration.parent` is the
// ExportNamedDeclaration, which has no `body` array, so both expressions yield nothing:
// `Array.isArray(...) ? ... : []` gives `[]` in the fixer, and `(parent.body && ...) || 0` gives 0
// in the check.
//
// So upstream neither reports a consecutive finding on an exported declaration nor repairs a
// combine on one. Ours puts the exported statement directly in the enclosing statement list, where
// it has real siblings and a real index, so without the collapse this rule reports where upstream
// is silent and -- far worse -- repairs where upstream declines.
//
// Measured before it was fixed, driving both: upstream returns `output: null` for all four export
// shapes while this port produced
//
//	export const a = 1; export const b = 2;   ->   export const a = 1; export,  b = 2;
//
// which is not valid syntax. The corpus cannot see any of it: every one of its seven exported cases
// is under `never`, which reaches the split fixer and never the join. The split fixer very much
// does repair an export, which is why the collapse is scoped to the join.
func oneVarIsExported(statement *ast.Node) bool {
	modifiers := statement.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindExportKeyword {
			return true
		}
	}
	return false
}

// oneVarIsAmbient answers whether a statement carries a `declare` modifier.
//
// Read off the statement for the same reason `oneVarIsExported` is: our parser hangs the modifier
// there, beside `export`, rather than on the declaration list.
func oneVarIsAmbient(statement *ast.Node) bool {
	modifiers := statement.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindDeclareKeyword {
			return true
		}
	}
	return false
}

// oneVarPreviousTokenEnd scans back from a token start over trivia to the end of the token before it.
//
// Returns -1 when nothing but trivia lies between here and the floor, which means there is no
// previous token to attach a comma to.
//
// Scanning backward over a line comment is the case that makes this more than a whitespace skip: a
// `//` comment runs to the end of its line, so a backward scan that stopped at the first
// non-whitespace byte would stop on the comment's last character and treat it as a token. The loop
// therefore walks whole lines and re-examines each one for a comment start.
func oneVarPreviousTokenEnd(text string, from int, floor int) int {
	position := from
	for position > floor {
		// Skip whitespace directly before the cursor.
		for position > floor && (text[position-1] == ' ' || text[position-1] == '\t' ||
			text[position-1] == '\r' || text[position-1] == '\n') {
			position--
		}
		if position <= floor {
			return -1
		}
		// A block comment ending here.
		if position-2 >= floor && text[position-2] == '*' && text[position-1] == '/' {
			closing := position - 2
			opening := closing
			for opening-1 > floor && !(text[opening-1] == '*' && opening-2 >= floor &&
				text[opening-2] == '/') {
				opening--
			}
			if opening-2 >= floor && text[opening-2] == '/' {
				position = opening - 2
				continue
			}
			return position
		}
		// A line comment covering the cursor's line.
		lineStart := position
		for lineStart > floor && text[lineStart-1] != '\n' {
			lineStart--
		}
		commentAt := -1
		for scan := lineStart; scan+1 < position; scan++ {
			if text[scan] == '/' && text[scan+1] == '/' {
				commentAt = scan
				break
			}
		}
		if commentAt >= 0 {
			position = commentAt
			continue
		}
		return position
	}
	return -1
}

// oneVarNextTokenOrCommentStart returns where the next token or comment begins, or -1.
func oneVarNextTokenOrCommentStart(text string, from int) int {
	position := from
	for position < len(text) {
		switch text[position] {
		case ' ', '\t', '\r', '\n':
			position++
			continue
		}
		return position
	}
	return -1
}

// oneVarCrossesLine answers whether a newline sits in a span.
func oneVarCrossesLine(text string, from int, to int) bool {
	for position := from; position < to && position < len(text); position++ {
		if text[position] == '\n' {
			return true
		}
	}
	return false
}

// oneVarStartsComment answers whether a position begins a line or block comment.
func oneVarStartsComment(text string, position int) bool {
	return position+1 < len(text) && text[position] == '/' &&
		(text[position+1] == '/' || text[position+1] == '*')
}

// oneVarLastCommentRunStart walks the run of comments after a comma and returns where the token
// following them begins.
//
// Upstream's loop advances `lastComment` while it is a Line or Block comment, so it lands on the
// first non-comment token; the replacement then runs up to that token's start. Reproduced by
// skipping whitespace and comments alternately.
func oneVarLastCommentRunStart(text string, from int) int {
	position := from
	for position < len(text) {
		for position < len(text) && (text[position] == ' ' || text[position] == '\t' ||
			text[position] == '\r' || text[position] == '\n') {
			position++
		}
		if position+1 < len(text) && text[position] == '/' && text[position+1] == '/' {
			for position < len(text) && text[position] != '\n' {
				position++
			}
			continue
		}
		if position+1 < len(text) && text[position] == '/' && text[position+1] == '*' {
			position += 2
			for position+1 < len(text) && !(text[position] == '*' && text[position+1] == '/') {
				position++
			}
			position += 2
			continue
		}
		return position
	}
	return position
}

// OneVar enforces that variables are declared together or separately, per configuration.
//
//	valid:   function foo() { var bar, baz; }
//	valid:   var foo = 1, bar = 2;
//	invalid: function foo() { var bar; var baz; }      // "always"
//	invalid: var foo = 1, bar = 2;                     // "never"
//	invalid: var a; var b;                             // "consecutive"
//
// Ported from `one-var` in ESLint 10.10.0, read from the clone at `lib/rules/one-var.js`. One
// option (a string enum or one of two object shapes), seven messages, and `meta.fixable` is
// `"code"`.
//
// All 296 of upstream's corpus cases were extracted from its own tester and replayed through the
// ESLint Linter API before any code was written -- twice, once under upstream's own parser settings
// and once under the TypeScript parser with `sourceType: module` that cohere always has. The two
// agree on every case, so nothing here rests on a JavaScript-only reading.
//
// # The walk is manual, because the rule is stateful and there is no exit listener
//
// Upstream registers ten `:exit` handlers to pop two stacks. This tree's listeners are pre-order
// only, so the whole traversal runs inside one `KindSourceFile` listener that pushes and pops the
// same frames around the same constructs. That is the shipped shape for a gather-then-judge rule.
//
// # Five places our AST differs from ESTree, and four of them move a verdict
//
// ESTree's `VariableDeclaration` is the statement and holds the declarators. Ours splits it into a
// `VariableStatement` wrapping a `VariableDeclarationList`, and in a for-head the list stands alone
// with no statement above it. `oneVarStatementOf` picks whichever plays the statement's part, and
// getting it wrong moves every span and breaks every consecutive verdict at once.
//
// ESTree wraps an exported declaration in `ExportNamedDeclaration`, and that wrapper has no `body`
// array, so upstream neither reports a consecutive finding on an export nor repairs a combine on
// one. Ours has no wrapper at all. See `oneVarIsExported`, which carries the measurement and the
// invalid source the port produced before the collapse was added.
//
// An ESTree `SwitchCase` keeps its statements in `.consequent`, not `.body`, so upstream's
// consecutive arm never fires inside one. Ours calls that node a statement list, because it is one.
// See the note above `oneVarIsInStatementList`.
//
// ESTree wraps a method body in a `FunctionExpression`, so upstream's one listener covers methods,
// constructors and accessors. Ours has no wrapper -- a method is `KindMethodDeclaration` -- so those
// four kinds are named explicitly as function scopes.
//
// The fifth is inert and recorded anyway: our `ClassStaticBlockDeclaration` holds a `Block` where
// upstream's `StaticBlock` is itself the statement list. See `oneVarIsBlockScope`.
//
// # `await using` is two flags, never the constant named for it
//
// `NodeFlagsAwaitUsing` is defined as `NodeFlagsConst | NodeFlagsUsing`, so a mask against it is
// true for every plain `const`. See `oneVarKindOf`, which tests both bits together instead and
// carries the measured flag table.
var OneVar = rule.Rule[OneVarSettings]{
	Name: "one-var",
	Run: func(ruleContext rule.Context, options rule.Given[OneVarSettings]) rule.Listeners {
		settings, ok := options.Read()
		if !ok {
			// A rule configured as a bare `"error"` is handed nil, and `options.(T)` on nil yields
			// the zero value -- every group unconfigured, so the rule would register on every file
			// and enforce nothing. Upstream's `defaultOptions: ["always"]` is the answer.
			settings = DefaultOneVarSettings()
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				if ruleContext.SourceFile == nil {
					return
				}
				checker := &oneVarChecker{context: ruleContext, settings: settings}

				var walk func(*ast.Node)
				walk = func(current *ast.Node) {
					if current == nil {
						return
					}
					isFunction := oneVarIsFunctionScope(current)
					isBlock := !isFunction && oneVarIsBlockScope(current)
					if isFunction {
						checker.scopes.startFunction()
					} else if isBlock {
						checker.scopes.startBlock()
					}

					if current.Kind == ast.KindVariableDeclarationList {
						checker.checkDeclarationList(current)
					}

					current.ForEachChild(func(child *ast.Node) bool {
						walk(child)
						return false
					})

					if isFunction {
						checker.scopes.endFunction()
					} else if isBlock {
						checker.scopes.endBlock()
					}
				}
				walk(node)
			},
		}
	},
}
