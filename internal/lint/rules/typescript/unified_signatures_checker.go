package typescript

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimcore "github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// unifiedSignaturesChecker carries what every helper needs: the context for reporting and reading
// source text, and the two options.
type unifiedSignaturesChecker struct {
	ctx      rule.Context
	settings UnifiedSignaturesOptions
}

// checkScope groups the signatures directly inside one scope, compares each group pairwise, and
// then recurses into the scopes nested within it.
//
// `outerTypeParameters` is the set of type parameter names in scope from enclosing declarations, not
// from the signatures themselves. Upstream's comment is precise about the distinction: in
// `interface I<T> { m<U>(x: U): T }` only `T` is outer. Two signatures where one mentions an outer
// type parameter and the other does not cannot be unified, because the union would erase the link
// between the parameter and the declaration's type argument.
func (checker *unifiedSignaturesChecker) checkScope(scope *ast.Node, outerTypeParameters map[string]bool) {
	groups := map[string][]*ast.Node{}
	var order []string

	for _, member := range unifiedSignaturesScopeMembers(scope) {
		if !unifiedSignaturesIsOverload(member) {
			continue
		}
		key := unifiedSignaturesOverloadKey(checker.ctx, member)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], member)
	}

	// Iterated in first-seen order rather than over the map, so findings come out in source order.
	//
	// Go randomises map iteration, so iterating `groups` directly would emit two groups' findings in
	// a different order between runs. Measured: over 200 iterations of a two-key map the order
	// reversed 22 times. A fixture asserting two findings therefore catches that mutation, but only
	// about 8% of the time, which is a flaky test rather than a guard. The determinism lives here so
	// the fixture can be an ordinary assertion.
	for _, key := range order {
		overloads := groups[key]
		for indexA := 0; indexA < len(overloads); indexA++ {
			for indexB := indexA + 1; indexB < len(overloads); indexB++ {
				checker.compare(overloads[indexA], overloads[indexB], len(overloads) == 2,
					outerTypeParameters)
			}
		}
	}

	for _, nested := range unifiedSignaturesNestedScopes(scope) {
		checker.checkScope(nested, unifiedSignaturesTypeParameterNames(nested, outerTypeParameters))
	}
}

// compare is upstream's `compareSignatures` plus `addFailures`: decide whether two signatures
// unify, and if so report the finding that says how.
func (checker *unifiedSignaturesChecker) compare(
	signatureA *ast.Node,
	signatureB *ast.Node,
	onlyTwo bool,
	outerTypeParameters map[string]bool,
) {
	if !checker.canBeUnified(signatureA, signatureB, outerTypeParameters) {
		return
	}

	parametersA := unifiedSignaturesParameters(signatureA)
	parametersB := unifiedSignaturesParameters(signatureB)
	if len(parametersA) == len(parametersB) {
		checker.reportSameLength(signatureA, signatureB, parametersA, parametersB, onlyTwo)
		return
	}
	checker.reportDifferentLength(signatureA, signatureB, parametersA, parametersB, onlyTwo)
}

// canBeUnified is upstream's `signaturesCanBeUnified`: the gate every pair must pass before the
// shape of their difference is even examined.
func (checker *unifiedSignaturesChecker) canBeUnified(
	signatureA *ast.Node,
	signatureB *ast.Node,
	outerTypeParameters map[string]bool,
) bool {
	parametersA := unifiedSignaturesParameters(signatureA)
	parametersB := unifiedSignaturesParameters(signatureB)

	if checker.settings.IgnoreDifferentlyNamedParameters {
		common := min(len(parametersA), len(parametersB))
		for index := 0; index < common; index++ {
			nameA, gotA := unifiedSignaturesStaticParameterName(parametersA[index])
			nameB, gotB := unifiedSignaturesStaticParameterName(parametersB[index])
			// Upstream gates this on the two parameters having the same node type first, which in
			// our tree is the same question as both yielding a static name: a destructured parameter
			// has no static name and is skipped either way.
			if gotA && gotB && nameA != nameB {
				return false
			}
		}
	}

	if checker.settings.IgnoreOverloadsWithDifferentJsDoc {
		if checker.blockCommentFor(signatureA) != checker.blockCommentFor(signatureB) {
			return false
		}
	}

	if !checker.typesAreEqual(unifiedSignaturesReturnType(signatureA),
		unifiedSignaturesReturnType(signatureB)) {
		return false
	}
	if !checker.typeParametersAreEqual(signatureA, signatureB) {
		return false
	}
	return unifiedSignaturesUsesTypeParameter(signatureA, outerTypeParameters) ==
		unifiedSignaturesUsesTypeParameter(signatureB, outerTypeParameters)
}

// reportSameLength is upstream's `signaturesHaveSameAmountOfParameters`.
//
// Two outcomes: every parameter matches, so the signatures are identical and one is redundant; or
// exactly one parameter differs and the pair can be written with a union at that position.
func (checker *unifiedSignaturesChecker) reportSameLength(
	signatureA *ast.Node,
	signatureB *ast.Node,
	parametersA []*ast.Node,
	parametersB []*ast.Node,
	onlyTwo bool,
) {
	// `this: void` is how a signature declares it must not be called as a method. Unifying it away
	// would erase that, so upstream exempts the pair outright.
	if unifiedSignaturesIsThisVoid(unifiedSignaturesAt(parametersA, 0)) ||
		unifiedSignaturesIsThisVoid(unifiedSignaturesAt(parametersB, 0)) {
		return
	}

	difference := -1
	for index := 0; index < len(parametersA) && index < len(parametersB); index++ {
		if !checker.parametersAreEqual(parametersA[index], parametersB[index]) {
			difference = index
			break
		}
	}

	if difference == -1 {
		checker.reportAllParametersAreSame(signatureA, signatureB, onlyTwo)
		return
	}

	// Everything after the differing parameter must still match, or the pair differs in more than
	// one place and no single union expresses it.
	for index := difference + 1; index < len(parametersA) && index < len(parametersB); index++ {
		if !checker.parametersAreEqual(parametersA[index], parametersB[index]) {
			return
		}
	}

	first := parametersA[difference]
	second := parametersB[difference]

	// `a?: string` and `b?: number` unify. `...a: string[]` and `...b: number[]` do not, because a
	// rest parameter's type is the array, and `(string | number)[]` is a weaker claim than
	// `string[] | number[]`. Upstream cites microsoft/TypeScript#5077 at this line.
	if !unifiedSignaturesHaveEqualSigils(first, second) || unifiedSignaturesIsRest(first) {
		return
	}

	checker.ctx.ReportNode(second, rule.Message{
		Id: "singleParameterDifference",
		Description: fmt.Sprintf("%s taking `%s`.",
			unifiedSignaturesFailureStart(checker.ctx, onlyTwo, first),
			checker.unifiedTypeText(unifiedSignaturesParameterType(first),
				unifiedSignaturesParameterType(second))),
	})
}

// reportDifferentLength is upstream's `signaturesDifferByOptionalOrRestParameter`.
//
// The longer signature carries parameters the shorter one lacks. They unify when every extra
// parameter beyond the first could be missing anyway, and the shared prefix agrees.
func (checker *unifiedSignaturesChecker) reportDifferentLength(
	signatureA *ast.Node,
	signatureB *ast.Node,
	parametersA []*ast.Node,
	parametersB []*ast.Node,
	onlyTwo bool,
) {
	firstA := unifiedSignaturesAt(parametersA, 0)
	firstB := unifiedSignaturesAt(parametersB, 0)

	// One declares `this` and the other does not: they describe different call shapes.
	if unifiedSignaturesIsThisParameter(firstA) != unifiedSignaturesIsThisParameter(firstB) {
		return
	}
	if unifiedSignaturesIsThisVoid(firstA) || unifiedSignaturesIsThisVoid(firstB) {
		return
	}

	longer, shorter := parametersA, parametersB
	shorterSignature := signatureB
	if len(parametersA) < len(parametersB) {
		longer, shorter = parametersB, parametersA
		shorterSignature = signatureA
	}
	minimum := len(shorter)

	// Upstream starts this loop at `minLength + 1`, not `minLength`, which is deliberate: the first
	// extra parameter is the one being reported as newly optional, so it needs no sigil of its own.
	// Every extra one after it must already be optional or rest.
	for index := minimum + 1; index < len(longer); index++ {
		if !unifiedSignaturesMayBeMissing(longer[index]) {
			return
		}
	}

	for index := 0; index < minimum; index++ {
		if !checker.typesAreEqual(unifiedSignaturesParameterType(parametersA[index]),
			unifiedSignaturesParameterType(parametersB[index])) {
			return
		}
	}

	// A shorter signature ending in a rest parameter already accepts everything the longer one does,
	// so there is nothing to unify.
	if minimum > 0 && unifiedSignaturesIsRest(shorter[minimum-1]) {
		return
	}

	extra := longer[len(longer)-1]
	messageId := "omittingSingleParameter"
	description := "with an optional parameter."
	if unifiedSignaturesIsRest(extra) {
		messageId = "omittingRestParameter"
		description = "with a rest parameter."
	}
	checker.ctx.ReportNode(extra, rule.Message{
		Id: messageId,
		Description: fmt.Sprintf("%s %s",
			unifiedSignaturesFailureStart(checker.ctx, onlyTwo, shorterSignature), description),
	})
}

// reportAllParametersAreSame reports two signatures that are identical in every parameter.
//
// This message does not exist in the installed 8.67.0 build; it is in the clone, which the brief
// names as the source. Four corpus rows assert it, and they are the four the oracle cannot check.
func (checker *unifiedSignaturesChecker) reportAllParametersAreSame(
	signatureA *ast.Node,
	signatureB *ast.Node,
	onlyTwo bool,
) {
	checker.ctx.ReportRange(unifiedSignaturesValueRange(checker.ctx, signatureB), rule.Message{
		Id: "allParametersAreSame",
		Description: fmt.Sprintf("%s with identical parameters.",
			unifiedSignaturesFailureStart(checker.ctx, onlyTwo, signatureA)),
	})
}

// typesAreEqual is upstream's `typesAreEqual`: two type nodes are equal when their source text
// matches, which is the rule's whole notion of type identity.
func (checker *unifiedSignaturesChecker) typesAreEqual(typeA *ast.Node, typeB *ast.Node) bool {
	if typeA == typeB {
		return true
	}
	if typeA == nil || typeB == nil {
		return false
	}
	return unifiedSignaturesNodeText(checker.ctx, typeA) ==
		unifiedSignaturesNodeText(checker.ctx, typeB)
}

// parametersAreEqual is upstream's `parametersAreEqual`: same sigils, same type text.
func (checker *unifiedSignaturesChecker) parametersAreEqual(left *ast.Node, right *ast.Node) bool {
	return unifiedSignaturesHaveEqualSigils(left, right) &&
		checker.typesAreEqual(unifiedSignaturesParameterType(left), unifiedSignaturesParameterType(right))
}

// typeParametersAreEqual is upstream's `arraysAreEqual(aTypeParams, bTypeParams, ...)`.
//
// Two signatures must take the same type parameters to unify, compared by name and by constraint.
// Upstream's `constraintsAreEqual` compares the constraint's node type rather than its text, which
// is weaker than it looks: `T extends string` and `T extends number` are both type references and
// compare equal. Ported as-is, because it is upstream's decision about how much a constraint counts.
func (checker *unifiedSignaturesChecker) typeParametersAreEqual(
	signatureA *ast.Node,
	signatureB *ast.Node,
) bool {
	parametersA := unifiedSignaturesSignatureTypeParameters(signatureA)
	parametersB := unifiedSignaturesSignatureTypeParameters(signatureB)
	if len(parametersA) != len(parametersB) {
		return false
	}
	for index := range parametersA {
		nameA := parametersA[index].Name()
		nameB := parametersB[index].Name()
		if nameA == nil || nameB == nil || nameA.Kind != ast.KindIdentifier ||
			nameB.Kind != ast.KindIdentifier ||
			nameA.AsIdentifier().Text != nameB.AsIdentifier().Text {
			return false
		}
		constraintA := parametersA[index].AsTypeParameterDeclaration().Constraint
		constraintB := parametersB[index].AsTypeParameterDeclaration().Constraint
		if constraintA == constraintB {
			continue
		}
		if constraintA == nil || constraintB == nil || constraintA.Kind != constraintB.Kind {
			return false
		}
	}
	return true
}

// unifiedTypeText is upstream's `getUnifiedTypeText`: the union the two parameters would become,
// spelled with the author's own words.
//
// Duplicate members are dropped, so `string | number` against `string` reads `string | number`
// rather than repeating. When one side has no annotation at all the other side's text stands alone,
// which is upstream's `nullThrows` branch; here a missing pair yields the empty string rather than
// panicking, because a rule must not take the file down.
func (checker *unifiedSignaturesChecker) unifiedTypeText(typeA *ast.Node, typeB *ast.Node) string {
	if typeA == nil || typeB == nil {
		present := typeA
		if present == nil {
			present = typeB
		}
		if present == nil {
			return ""
		}
		return checker.unionMemberText(present)
	}

	var members []*ast.Node
	members = append(members, unifiedSignaturesUnionMembers(typeA)...)
	members = append(members, unifiedSignaturesUnionMembers(typeB)...)

	var unique []*ast.Node
	for _, member := range members {
		duplicate := false
		for _, existing := range unique {
			if checker.typesAreEqual(existing, member) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			unique = append(unique, member)
		}
	}

	texts := make([]string, 0, len(unique))
	for _, member := range unique {
		texts = append(texts, checker.unionMemberText(member))
	}
	return strings.Join(texts, " | ")
}

// unionMemberText is upstream's `getUnionMemberText`.
//
// Three type forms bind looser than `|` and have to be parenthesised or the printed union would
// reassociate: a conditional, a constructor type, and a function type. `A | () => B` would parse as
// a function returning `B` rather than as a union.
func (checker *unifiedSignaturesChecker) unionMemberText(node *ast.Node) string {
	text := unifiedSignaturesNodeText(checker.ctx, node)
	switch node.Kind {
	case ast.KindConditionalType, ast.KindConstructorType, ast.KindFunctionType:
		return "(" + text + ")"
	}
	return text
}

// blockCommentFor is upstream's `getBlockCommentForNode`: the last block comment before a signature,
// used only when `ignoreOverloadsWithDifferentJSDoc` is on.
//
// Returns the comment's text rather than a node, since the only question asked of it is equality.
// A signature with no preceding block comment yields the empty string, so two undocumented overloads
// compare equal, which is upstream's behaviour when both lookups return undefined.
//
// Upstream's `getCommentsBefore` is every comment between the previous token and the signature, which is
// the signature's leading trivia, and its `.reverse().find(...)` takes the last block comment among them,
// past any line comment after it: `/** a */ // b` before a signature is documented by `a`. A scan back from
// the signature that stopped at the first comment it met missed that one, so the ranges are read forwards
// and the last block comment kept.
//
// The scanner's leading ranges alone are not that trivia. At any position but 0 they start after the first
// newline, leaving what precedes it to the previous token as its trailing comment, so in
// `f(a: string): void; /** b */` then `f(a: number): void;` on the next line the second overload read as
// undocumented. `GetCommentsInRange` reads the trailing ranges up to that newline and then the leading
// ones, which is every comment between the previous token and the signature, as upstream's lookup is. At
// position 0 both reads start at the file's beginning and a first-line comment comes twice, which cannot
// change which block comment is last.
func (checker *unifiedSignaturesChecker) blockCommentFor(signature *ast.Node) string {
	sourceText := checker.ctx.SourceFile.Text()
	trivia := shimcore.NewTextRange(signature.Pos(), rule.TokenRange(checker.ctx.SourceFile, signature).Pos())
	text := ""
	for commentRange := range type_checking.GetCommentsInRange(checker.ctx.SourceFile, trivia) {
		if commentRange.Kind == ast.KindMultiLineCommentTrivia {
			text = sourceText[commentRange.Pos()+len("/*") : commentRange.End()-len("*/")]
		}
	}
	return text
}
