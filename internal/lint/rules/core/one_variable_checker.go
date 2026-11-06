package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// oneVariableChecker holds what one file's walk needs.
type oneVariableChecker struct {
	context  rule.Context
	settings OneVariableSettings
	scopes   oneVariableScopes
}

// groupFor returns the settings governing a declaration kind.
func (checker *oneVariableChecker) groupFor(kind oneVariableKind) oneVariableGroup {
	switch kind {
	case oneVariableKindLet:
		return checker.settings.Let
	case oneVariableKindConst:
		return checker.settings.Const
	case oneVariableKindUsing:
		return checker.settings.Using
	case oneVariableKindAwaitUsing:
		return checker.settings.AwaitUsing
	}
	return checker.settings.Variable
}

// recordTypes is upstream's `recordTypes`, marking what this scope has now seen.
//
// Only an `always` half records. A `never` or `consecutive` half leaves the scope flags untouched,
// which is why a `consecutive` rule's verdict depends on the immediately preceding statement rather
// than on anything the scope accumulated.
//
// Mutates: scope the flags are the scope's running record, which later statements in it read back
func (checker *oneVariableChecker) recordTypes(kind oneVariableKind, declarators []*ast.Node,
	scope *oneVariableStateFlags) {
	group := checker.groupFor(kind)
	for _, declarator := range declarators {
		if declarator.AsVariableDeclaration().Initializer == nil {
			if modeIs(group.Uninitialized, OneVariableAlways) {
				scope.Uninitialized = true
			}
			continue
		}
		if !modeIs(group.Initialized, OneVariableAlways) {
			continue
		}
		if checker.settings.SeparateRequires && oneVariableIsRequire(declarator) {
			scope.Required = true
		} else {
			scope.Initialized = true
		}
	}
}

// hasOnlyOneStatement is upstream's function of the same name.
//
// It answers "may this statement declare, given what the scope has already seen", and it records on
// the way out when the answer is yes. The recording is the reason this cannot be split into a pure
// predicate: the third `var` in a scope is judged against a flag the second one set.
func (checker *oneVariableChecker) hasOnlyOneStatement(kind oneVariableKind, declarators []*ast.Node) bool {
	counts := oneVariableCountDeclarations(declarators)
	group := checker.groupFor(kind)
	scope := checker.scopes.current(kind)
	if scope == nil {
		return true
	}
	hasRequires := oneVariableAnyRequire(declarators)

	if modeIs(group.Uninitialized, OneVariableAlways) && modeIs(group.Initialized, OneVariableAlways) {
		if scope.Uninitialized || scope.Initialized {
			if !hasRequires {
				return false
			}
		}
	}
	if counts.Uninitialized > 0 {
		if modeIs(group.Uninitialized, OneVariableAlways) && scope.Uninitialized {
			return false
		}
	}
	if counts.Initialized > 0 {
		if modeIs(group.Initialized, OneVariableAlways) && scope.Initialized {
			if !hasRequires {
				return false
			}
		}
	}
	if scope.Required && hasRequires {
		return false
	}
	checker.recordTypes(kind, declarators, scope)
	return true
}

// checkDeclarationList is upstream's `checkVariableDeclaration`.
//
// The three blocks run in upstream's order and are not exclusive: one statement can report a
// `splitRequires` and a `combine` and a `split`, which is why 23 corpus cases expect more than one
// finding on a single input.
func (checker *oneVariableChecker) checkDeclarationList(list *ast.Node) {
	kind := oneVariableKindOf(list)
	group := checker.groupFor(kind)

	// Upstream's `if (!options[key]) return;`, which skips a declaration kind the config never
	// named. Removing this is an equivalent mutant today and the sweep says so, for a reason
	// established by enumeration rather than by the survival: every read of a group's two halves
	// goes through `modeIs`, which answers false for nil, so an unconfigured group already
	// declines at every one of the fourteen comparison sites.
	//
	// Kept because that redundancy is a property of `modeIs` rather than of this rule, and a
	// future change there would make this guard load bearing without anything saying so.
	// `TestOneVariableSkipsDeclarationKindsTheConfigDidNotName` pins the behaviour directly, so the
	// contract survives even though no corpus row can discriminate the guard itself.
	if !group.configured() {
		return
	}

	statement := oneVariableStatementOf(list)
	container := statement.Parent
	declarators := oneVariableDeclarators(list)
	counts := oneVariableCountDeclarations(declarators)
	mixedRequires := oneVariableAnyRequire(declarators) && !oneVariableAllRequire(declarators)

	if modeIs(group.Initialized, OneVariableAlways) &&
		checker.settings.SeparateRequires && mixedRequires {
		checker.context.ReportNode(statement,
			oneVariableRender(messageOneVariableSplitRequires, kind))
	}

	// consecutive
	//
	// Upstream's index expression is `(parent.body && parent.body.length > 0 &&
	// parent.body.indexOf(node)) || 0`, whose `|| 0` makes a node that is first in its list (index
	// 0) and a node with no list at all indistinguishable. Both mean "no previous sibling to merge
	// with", so the guard below is `> 0` and reproduces it exactly.
	siblings := oneVariableStatementList(container)
	nodeIndex := oneVariableIndexOf(siblings, statement)
	if oneVariableIsExported(statement) {
		// Upstream's `parent` here is the ExportNamedDeclaration wrapping the declaration, and
		// that node carries no `body` array at all, so `parent.body && ...` is falsy and the whole
		// index expression collapses to 0. Both the consecutive check and the join repair are
		// therefore dead for an exported declaration upstream. See `oneVariableIsExported`.
		siblings, nodeIndex = nil, 0
	}
	if nodeIndex > 0 {
		previous := siblings[nodeIndex-1]
		previousList := oneVariableDeclarationListOfStatement(previous)
		if previousList != nil && oneVariableKindOf(previousList) == kind {
			previousDeclarators := oneVariableDeclarators(previousList)
			combined := append(append([]*ast.Node{}, declarators...), previousDeclarators...)
			if !(oneVariableAnyRequire(combined) && !oneVariableAllRequire(combined)) {
				previousCounts := oneVariableCountDeclarations(previousDeclarators)
				checker.reportConsecutive(statement, list, kind, group, counts, previousCounts,
					siblings, nodeIndex)
			}
		}
	}

	// always
	if !checker.hasOnlyOneStatement(kind, declarators) {
		checker.reportAlways(statement, list, kind, group, counts, siblings, nodeIndex)
	}

	// never
	//
	// Upstream skips a `for` statement's own initializer, because splitting `for (var i = 0, j = 0;;)`
	// would move `j` outside the head and change what the loop means. `parent.init !== node` is the
	// test, and here the list is the init when its parent is the for statement.
	if !oneVariableIsForStatementInitializer(list) {
		if counts.Uninitialized+counts.Initialized > 1 {
			checker.reportNever(statement, list, kind, group, counts)
		}
	}
}

// reportConsecutive emits the combine family for the `consecutive` modes.
func (checker *oneVariableChecker) reportConsecutive(statement *ast.Node, list *ast.Node,
	kind oneVariableKind, group oneVariableGroup, counts oneVariableCounts, previousCounts oneVariableCounts,
	siblings []*ast.Node, nodeIndex int) {
	switch {
	case modeIs(group.Initialized, OneVariableConsecutive) &&
		modeIs(group.Uninitialized, OneVariableConsecutive):
		checker.reportJoin(statement, list, messageOneVariableCombine, kind, siblings, nodeIndex)
	case modeIs(group.Initialized, OneVariableConsecutive) &&
		counts.Initialized > 0 && previousCounts.Initialized > 0:
		checker.reportJoin(statement, list, messageOneVariableCombineInitialized, kind, siblings, nodeIndex)
	case modeIs(group.Uninitialized, OneVariableConsecutive) &&
		counts.Uninitialized > 0 && previousCounts.Uninitialized > 0:
		checker.reportJoin(statement, list, messageOneVariableCombineUninitialized, kind, siblings, nodeIndex)
	}
}

// reportAlways emits the combine family for the `always` modes.
func (checker *oneVariableChecker) reportAlways(statement *ast.Node, list *ast.Node,
	kind oneVariableKind, group oneVariableGroup, counts oneVariableCounts,
	siblings []*ast.Node, nodeIndex int) {
	if modeIs(group.Initialized, OneVariableAlways) && modeIs(group.Uninitialized, OneVariableAlways) {
		checker.reportJoin(statement, list, messageOneVariableCombine, kind, siblings, nodeIndex)
		return
	}
	if modeIs(group.Initialized, OneVariableAlways) && counts.Initialized > 0 {
		checker.reportJoin(statement, list, messageOneVariableCombineInitialized, kind, siblings, nodeIndex)
	}
	if modeIs(group.Uninitialized, OneVariableAlways) && counts.Uninitialized > 0 {
		// A for-in/for-of head is skipped entirely, report and all. See the helper's comment.
		if oneVariableIsForInOrOfLeft(list) {
			return
		}
		checker.reportJoin(statement, list, messageOneVariableCombineUninitialized, kind, siblings, nodeIndex)
	}
}

// reportNever emits the split family.
func (checker *oneVariableChecker) reportNever(statement *ast.Node, list *ast.Node,
	kind oneVariableKind, group oneVariableGroup, counts oneVariableCounts) {
	switch {
	case modeIs(group.Initialized, OneVariableNever) && modeIs(group.Uninitialized, OneVariableNever):
		checker.reportSplit(statement, list, messageOneVariableSplit, kind)
	case modeIs(group.Initialized, OneVariableNever) && counts.Initialized > 0:
		checker.reportSplit(statement, list, messageOneVariableSplitInitialized, kind)
	case modeIs(group.Uninitialized, OneVariableNever) && counts.Uninitialized > 0:
		checker.reportSplit(statement, list, messageOneVariableSplitUninitialized, kind)
	}
}

// reportJoin reports a combine finding, with upstream's `joinDeclarations` repair when it applies.
//
// # Two edits that only mean something together
//
// Upstream's fixer is a generator yielding two or three edits: turn the previous statement's
// terminator into a comma, drop the `using` token when the kind is `await using`, and delete this
// statement's declaration keyword. They are disjoint spans, and `internal/edit`'s ProposalsFrom
// flattens a diagnostic's fixes into independent proposals, so the engine judges each separately.
//
// Measured against the real engine rather than assumed: both halves survive overlap resolution, and
// a rival fix spanning the gap between them loses to this pair rather than splitting it, because
// these sort first and claim their bytes.
//
// What that measurement does not buy is safety against the one-byte gap between the two spans. A
// rival fix landing strictly inside it would take one half and leave the other, and applying only
// the keyword deletion yields `var bar = true; baz = false;` -- which parses, and silently declares
// an implicit global. The engine's parse-failure guard cannot see that, so it is the one shape
// worth naming here. It has no live caller today: this is the first shipped rule to report more
// than one fix on one diagnostic, and `ProposalsFrom`'s own comment says grouping should be built
// when a rule genuinely needs it rather than warned about. That is a finding for the shelf and it
// is in the report, not something this rule can fix from here.
func (checker *oneVariableChecker) reportJoin(statement *ast.Node, list *ast.Node,
	message rule.Message, kind oneVariableKind, siblings []*ast.Node, nodeIndex int) {
	rendered := oneVariableRender(message, kind)

	fixes := checker.joinFixes(statement, list, kind, siblings, nodeIndex)
	if len(fixes) == 0 {
		checker.context.ReportNode(statement, rendered)
		return
	}
	checker.context.ReportNodeWithFixes(statement, rendered, fixes...)
}

// joinFixes builds upstream's `joinDeclarationsFixer`, or nothing when it declines.
//
// Upstream's generator yields nothing unless the previous sibling is a declaration of the same
// kind. The consecutive arm has already established that, but the `always` arm calls the same fixer
// without checking, so a combine reported on a statement whose predecessor is something else
// carries no repair at all. That is a report without a fix rather than a bug, and 26 corpus cases
// record it as `output: null`.
//
// The same-kind check here is therefore not redundant with the caller's. The isolating shape is
// `function f() { var a = 1; let b = 2; var c = 3; }`, where the third statement reports because
// the function scope already holds a `var` while its immediate predecessor is a `let`. Measured:
// upstream returns `output: null`. No corpus case writes it, so a mutant dropping this check
// survives all 296; see `TestOneVariableJoinDeclinesAgainstADifferentKindPredecessor`.
func (checker *oneVariableChecker) joinFixes(statement *ast.Node, list *ast.Node,
	kind oneVariableKind, siblings []*ast.Node, nodeIndex int) []rule.Fix {
	if nodeIndex <= 0 || nodeIndex-1 >= len(siblings) {
		return nil
	}
	previous := siblings[nodeIndex-1]
	previousList := oneVariableDeclarationListOfStatement(previous)
	if previousList == nil || oneVariableKindOf(previousList) != kind {
		return nil
	}

	// An ambient declaration is declined, which is this port's own decision and a measured one.
	// `declare let a: number; declare let b: number;` was joined to `declare let a: number;
	// declare,  b: number;`, because the token before this statement's keyword is its own `declare`
	// rather than the previous statement's semicolon. That does not parse. Upstream's installed
	// build writes `declare let a: number,\n let b: number;`, which does not parse either.
	//
	// Repairing it properly would mean deleting the second `declare` too, and is only meaning-
	// preserving when both statements are ambient: joining an ambient binding into a real one, or the
	// reverse, changes whether a variable exists at runtime. Reporting without a repair is the subset
	// that can be shown correct.
	if oneVariableIsAmbient(statement) || oneVariableIsAmbient(previous) {
		return nil
	}

	sourceFile := checker.context.SourceFile
	if sourceFile == nil {
		return nil
	}
	text := sourceFile.Text()

	// `type` is upstream's first token of the declaration -- the `var`/`let`/`const`/`using`
	// keyword, or `await` for an `await using`.
	//
	// Taken off the list rather than the statement. The two positions differ only for an exported
	// declaration, where the statement's first token is `export` and the list's is the keyword,
	// and the caller has already returned for those -- so a mutation swapping them survives, and
	// the sweep says so. The list is what upstream reads (its node begins at the keyword, with the
	// export wrapper above it), and reading the statement here would make this function's
	// correctness depend on a guard in another function rather than on anything local.
	keyword := scanner.GetRangeOfTokenAtPosition(sourceFile, list.Pos())

	// `beforeType` is the token before it, which is the previous statement's terminator when there
	// is one. Found by scanning back over trivia from the keyword rather than by taking the previous
	// statement's End(), because a statement's End() sits past its own terminator and a list's does
	// not, so the two disagree by exactly the byte this fix has to replace.
	beforeEnd := oneVariablePreviousTokenEnd(text, keyword.Pos(), previous.Pos())
	if beforeEnd < 0 {
		return nil
	}

	fixes := []rule.Fix{}
	if beforeEnd > 0 && text[beforeEnd-1] == ';' {
		fixes = append(fixes, rule.ReplaceRange(core.NewTextRange(beforeEnd-1, beforeEnd), ","))
	} else {
		// Upstream's `insertTextAfter`, for a previous statement with no semicolon (automatic semicolon insertion).
		fixes = append(fixes, rule.ReplaceRange(core.NewTextRange(beforeEnd, beforeEnd), ","))
	}

	// `await using` is two tokens, and upstream removes the second one separately before blanking
	// the first. Without this the repair leaves a stray `using` behind.
	if kind == oneVariableKindAwaitUsing {
		usingToken := scanner.GetRangeOfTokenAtPosition(sourceFile, keyword.End())
		fixes = append(fixes, rule.RemoveRange(core.NewTextRange(usingToken.Pos(), usingToken.End())))
	}

	fixes = append(fixes, rule.RemoveRange(core.NewTextRange(keyword.Pos(), keyword.End())))
	return fixes
}

// reportSplit reports a split finding, with upstream's `splitDeclarations` repair when it applies.
func (checker *oneVariableChecker) reportSplit(statement *ast.Node, list *ast.Node,
	message rule.Message, kind oneVariableKind) {
	rendered := oneVariableRender(message, kind)

	fixes := checker.splitFixes(statement, list, kind)
	if len(fixes) == 0 {
		checker.context.ReportNode(statement, rendered)
		return
	}
	checker.context.ReportNodeWithFixes(statement, rendered, fixes...)
}

// splitFixes builds upstream's `splitDeclarations`.
//
// Upstream returns null -- declining the repair entirely -- when the statement is not a member of a
// statement list, because splitting `if (foo) var x, y;` would move `y` outside the `if` and change
// what the code means. That is a decline of the fix, not of the report.
//
// Each comma becomes `; <kind> `, with an `export ` prefix when the statement is exported, and the
// three shapes upstream distinguishes are about where the comment and the newline go.
//
// A `declare ` prefix is carried too, which upstream does not do. Measured on the installed build:
// `declare let a: number, b: number;` splits to `declare let a: number; let b: number;`, turning an
// ambient binding into one that exists at runtime, and `export declare const a: number, b: number;`
// splits to `export declare const a: number; export const b: number;`, which does not compile
// because a non-ambient `const` needs an initializer. Upstream's estree hangs `declare` on the
// declaration as a flag it never consults here; ours is a modifier on the statement, beside
// `export`, and both have to be repeated on every statement the split creates.
func (checker *oneVariableChecker) splitFixes(statement *ast.Node, list *ast.Node,
	kind oneVariableKind) []rule.Fix {
	if !oneVariableIsInStatementList(statement) {
		return nil
	}
	sourceFile := checker.context.SourceFile
	if sourceFile == nil {
		return nil
	}
	text := sourceFile.Text()

	modifierPrefix := ""
	if oneVariableIsExported(statement) {
		modifierPrefix = "export "
	}
	if oneVariableIsAmbient(statement) {
		modifierPrefix += "declare "
	}

	fixes := []rule.Fix{}
	for _, declarator := range oneVariableDeclarators(list) {
		comma := scanner.GetRangeOfTokenAtPosition(sourceFile, declarator.End())
		if comma.Pos() >= len(text) || text[comma.Pos()] != ',' {
			// Upstream returns null for a declarator with no token after it, and skips one whose
			// following token is not a comma -- the last declarator, whose next token is the
			// terminator.
			continue
		}

		// `afterComma` is the next token including comments, which is what decides between the
		// three shapes below.
		afterCommaPosition := oneVariableNextTokenOrCommentStart(text, comma.End())
		if afterCommaPosition < 0 {
			continue
		}

		// `var x,y` -- nothing at all between the comma and the next declarator.
		if afterCommaPosition == comma.End() {
			fixes = append(fixes, rule.ReplaceRange(
				core.NewTextRange(comma.Pos(), comma.End()),
				"; "+modifierPrefix+kind.text()+" "))
			continue
		}

		// A newline, or a comment, between the comma and what follows. Upstream keeps everything
		// from the comma's end up to the last comment's start verbatim and puts the new statement
		// head after it, so `var f, /* c */ l` becomes `var f; /* c */ var l`.
		if oneVariableCrossesLine(text, comma.End(), afterCommaPosition) ||
			oneVariableStartsComment(text, afterCommaPosition) {
			lastCommentStart := oneVariableLastCommentRunStart(text, comma.End())
			fixes = append(fixes, rule.ReplaceRange(
				core.NewTextRange(comma.Pos(), lastCommentStart),
				";"+text[comma.End():lastCommentStart]+modifierPrefix+kind.text()+" "))
			continue
		}

		// Ordinary whitespace on one line: `var x, y` -- the space after the comma is kept by the
		// replacement text ending without one.
		fixes = append(fixes, rule.ReplaceRange(
			core.NewTextRange(comma.Pos(), comma.End()),
			"; "+modifierPrefix+kind.text()))
	}
	return fixes
}
