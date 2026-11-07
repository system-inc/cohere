package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// noUncheckedCastText is the rule's message, whose wording lives in `policy/messages/no-unchecked-cast.json`.
var noUncheckedCastText = policy.MessageOf("adamic/no-unchecked-cast", "uncheckedCast")

// noUncheckedCastUnderTscText is uncheckedCast when no runtime check is emitted, from the same file: the
// default's last sentence, that Adamic checks a union downcast, is false on tsc and points at the wrong fix.
var noUncheckedCastUnderTscText = policy.MessageOf("adamic/no-unchecked-cast", "uncheckedCastUnderTsc")

// noUncheckedDowncastText is the message for a union downcast when no runtime check is emitted, from the same file.
var noUncheckedDowncastText = policy.MessageOf("adamic/no-unchecked-cast", "uncheckedDowncast")

// uncheckedCastText is the message for a cast nothing could check, chosen from the option here and nowhere
// else: Adamic's names the union downcast it checks as the way out, and tsc's names a type guard instead.
func uncheckedCastText(settings NoUncheckedCastOptions) policy.MessageHandle {
	if settings.RuntimeChecksEmitted {
		return noUncheckedCastText
	}
	return noUncheckedCastUnderTscText
}

// NoUncheckedCastOptions says whether the program's compiler backs a union downcast with a runtime check.
type NoUncheckedCastOptions struct {
	// RuntimeChecksEmitted is true for a program Adamic compiles, which checks a union downcast at runtime
	// and panics when it fails, and false for one that runs on tsc, which emits nothing for a cast, so every
	// union downcast is as unchecked as any other narrowing. True when absent: Adamic's meaning.
	RuntimeChecksEmitted bool `json:"runtimeChecksEmitted"`
}

// DefaultNoUncheckedCastOptions is the rule's meaning with nothing configured: Adamic's, where a union
// downcast is checked.
func DefaultNoUncheckedCastOptions() NoUncheckedCastOptions {
	return NoUncheckedCastOptions{RuntimeChecksEmitted: true}
}

// DecodeNoUncheckedCastOptions reads the rule's options over its defaults. Hand-rolled rather than
// rule.DecodeOptionsInto, which would decode an absent runtimeChecksEmitted to false and turn a bare
// severity into the strict reading.
func DecodeNoUncheckedCastOptions(raw []byte) (NoUncheckedCastOptions, error) {
	options := DefaultNoUncheckedCastOptions()
	if len(raw) == 0 {
		return options, nil
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &options); unmarshalOptionsError != nil {
		return options, unmarshalOptionsError
	}
	return options, nil
}

// CheckedDowncastNote is the note a cast Adamic compiles to a runtime check records: counted under
// --coverage, never a finding, so readiness does not fail on it.
const CheckedDowncastNote = "checked downcast"

/*
 * NoUncheckedCast reports a cast the runtime could not check (#drbrp8c).
 *
 *     invalid: load() as User                  unknown to an interface: no tag to confirm it
 *     invalid: value as unknown as User        the outer cast is the unchecked one
 *     invalid: JSON.parse(text) as User        any, cast to anything, is a guess
 *     valid:   shape as Circle                 a union member with a discriminant: a checked downcast
 *     valid:   dog as Animal                   an upcast; invariant-mutable judges what it widens
 *     valid:   [1, 2] as const
 *
 * # What is allowed, and what Adamic does with it
 *
 * - `as const`, and a cast to the same type.
 * - An upcast: the source is assignable to the target. Nothing is claimed that tsc did not prove, and
 *   flow.Walker offers it to invariant-mutable, nominal-class and no-optional-widening as a site.
 * - A checked downcast: the source is a union, the target keeps some of its members, and the runtime can
 *   tell every kept member from every dropped one, by a property whose unit literal differs between them,
 *   by typeof, or by instanceof. Adamic compiles it to a check that panics (adamic docs/0.1.md, decision
 *   5), so it is no hole. It is counted as a note, CheckedDowncastNote, and not reported.
 *
 * # runtimeChecksEmitted
 *
 * The checked downcast is checked only where a compiler emits the check. tsc emits none, so in a project
 * that runs on tsc `value as string`, where value is `string | true`, is as unchecked as `load() as User`.
 * With `runtimeChecksEmitted: false` the rule says so: every checked downcast is reported, with a message
 * naming the type guard to write instead (#mdccedj). Measured on ahra, www-phi-health, api-phi-health and
 * www-ahra-ai, typescript-eslint's no-unsafe-type-assertion reported 71 such sites this rule passed, and
 * 28 of them were real bugs. Absent, it is true, and the rule reads as it always has.
 *
 * Everything else is reported, `as unknown as T` and `as NonNullable<T>` included. A source typed `any`
 * is assignable to every target, so it is caught before the upcast test: the cast is the only claim the
 * value's type ever gets.
 *
 * The checked forms are this rule's judgment for now. #bw3xg7c decides Adamic's escape hatches against
 * the readiness survey's counts, and may widen or narrow them; checkedDowncast is the one place to change.
 *
 * # No fix
 *
 * The repair is where the value comes from, which the cast hides.
 */
var NoUncheckedCast = rule.Rule[NoUncheckedCastOptions]{
	Name:             "adamic/no-unchecked-cast",
	NeedsTypeChecker: true,

	Run: func(ruleContext rule.Context, options rule.Given[NoUncheckedCastOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil || ruleContext.SourceFile == nil {
			return nil
		}
		settings, configured := options.Read()
		if !configured {
			settings = DefaultNoUncheckedCastOptions()
		}
		typeChecker := ruleContext.TypeChecker
		report := func(node *ast.Node, text policy.MessageHandle, source *checker.Type, target *checker.Type) {
			ruleContext.ReportNode(node, rule.Message{
				Id: text.Id,
				Description: text.Render(map[string]string{
					"source": typeChecker.TypeToString(source),
					"target": typeChecker.TypeToString(target),
				}),
			})
		}
		judge := func(node *ast.Node) {
			typeNode := node.Type()
			if typeNode == nil || ast.IsConstTypeReference(typeNode) {
				return
			}
			source := typeChecker.GetTypeAtLocation(node.Expression())
			target := checker.Checker_getTypeFromTypeNode(typeChecker, typeNode)
			if source == nil || target == nil || source == target {
				return
			}
			// An `any` anywhere in the source (`any`, `any[]`, `Promise<any>`) is assignable to everything, so
			// the upcast test would pass it: the cast is the only claim the value's type gets, and nothing
			// checks it. The consumers showed `result.rows as RowType[]` on an `any[]` passing every rule.
			if !type_checking.CanBeUnsafeAssignment(typeChecker, source) {
				// A fresh object literal's type carries its freshness, and tsc refuses a fresh literal with a key
				// its target lacks, so `{ ...init, duplex: 'half' } as RequestInit` would read as no upcast.
				// Dropping a key the target does not declare is width subtyping, which Adamic's relation
				// accepts (adamic docs/rulings.md:14), so the literal is widened first, as typescript-eslint's
				// no-unsafe-type-assertion widens it. A shared key of the wrong type still fails.
				assignableSource := source
				if type_checking.IsObjectType(source) &&
					checker.Type_objectFlags(source)&checker.ObjectFlagsObjectLiteral != 0 {
					assignableSource = checker.Checker_getWidenedType(typeChecker, source)
				}
				if checker.Checker_isTypeAssignableTo(typeChecker, assignableSource, target) {
					return
				}
				if checkedDowncast(typeChecker, source, target) {
					if settings.RuntimeChecksEmitted {
						ruleContext.Note(CheckedDowncastNote)
						return
					}
					report(node, noUncheckedDowncastText, source, target)
					return
				}
			} else if target.Flags()&(checker.TypeFlagsAny|checker.TypeFlagsUnknown) != 0 ||
				type_checking.IsTypeUnknownArrayType(target, typeChecker) {
				// Only an any or unknown at the target's top passes: one held below it, as `BaseModule<any>[]`
				// or `GraphQLObjectType<any, any, any>`, is still reported, since Adamic refuses explicit any at
				// every position, type arguments included (adamic docs/rulings.md:39).
				return
			}
			report(node, uncheckedCastText(settings), source, target)
		}
		return rule.Listeners{
			ast.KindAsExpression:            judge,
			ast.KindTypeAssertionExpression: judge,
		}
	},
}

// checkedDowncast is a cast from a union to some of its members that the runtime can check.
func checkedDowncast(typeChecker *checker.Checker, source *checker.Type, target *checker.Type) bool {
	if source.Flags()&checker.TypeFlagsUnion == 0 {
		return false
	}
	targetMembers := []*checker.Type{target}
	if target.Flags()&checker.TypeFlagsUnion != 0 {
		targetMembers = target.Types()
	}
	var kept []*checker.Type
	var dropped []*checker.Type
	for _, member := range source.Types() {
		if containsIdentical(typeChecker, targetMembers, member) {
			kept = append(kept, member)
		} else {
			dropped = append(dropped, member)
		}
	}
	// Every target member must be a source member, or the cast claims something the union never held.
	if len(kept) != len(targetMembers) || len(kept) == 0 {
		return false
	}
	for _, keep := range kept {
		for _, drop := range dropped {
			if !distinguishable(typeChecker, keep, drop) {
				return false
			}
		}
	}
	return true
}

func containsIdentical(typeChecker *checker.Checker, types []*checker.Type, wanted *checker.Type) bool {
	for _, candidate := range types {
		if candidate == wanted || checker.Checker_isTypeIdenticalTo(typeChecker, candidate, wanted) {
			return true
		}
	}
	return false
}

// distinguishable is two union members a runtime check can tell apart: by typeof, by a property whose
// unit literal differs, or by instanceof a class one is and the other is not.
func distinguishable(typeChecker *checker.Checker, left *checker.Type, right *checker.Type) bool {
	if typeofClass(left) != typeofClass(right) {
		return true
	}
	if typeofClass(left) != "object" {
		// Two members of one primitive class: literals of it, told apart by ===.
		return isUnitLiteral(left) && isUnitLiteral(right)
	}
	if flow.IsClassInstance(left) && !flow.IsClassInstance(right) ||
		!flow.IsClassInstance(left) && flow.IsClassInstance(right) {
		return true
	}
	if flow.IsClassInstance(left) && flow.IsClassInstance(right) && classSymbol(left) != classSymbol(right) &&
		!derivesFrom(typeChecker, declaredClassType(typeChecker, left), classSymbol(right), map[*ast.Symbol]bool{}) &&
		!derivesFrom(typeChecker, declaredClassType(typeChecker, right), classSymbol(left), map[*ast.Symbol]bool{}) {
		return true
	}
	for _, property := range checker.Checker_getPropertiesOfType(typeChecker, left) {
		other := checker.Checker_getPropertyOfType(typeChecker, right, property.Name)
		if other == nil {
			continue
		}
		leftType := checker.Checker_getTypeOfSymbol(typeChecker, property)
		rightType := checker.Checker_getTypeOfSymbol(typeChecker, other)
		if isUnitLiteral(leftType) && isUnitLiteral(rightType) &&
			!checker.Checker_isTypeIdenticalTo(typeChecker, leftType, rightType) {
			return true
		}
	}
	return false
}

// typeofClass is what `typeof` would say of a value of the type, with null told apart from object.
func typeofClass(valueType *checker.Type) string {
	flags := valueType.Flags()
	switch {
	case flags&checker.TypeFlagsStringLike != 0:
		return "string"
	case flags&checker.TypeFlagsNumberLike != 0:
		return "number"
	case flags&checker.TypeFlagsBooleanLike != 0:
		return "boolean"
	case flags&checker.TypeFlagsBigIntLike != 0:
		return "bigint"
	case flags&checker.TypeFlagsESSymbolLike != 0:
		return "symbol"
	case flags&(checker.TypeFlagsUndefined|checker.TypeFlagsVoid) != 0:
		return "undefined"
	case flags&checker.TypeFlagsNull != 0:
		return "null"
	}
	return "object"
}

func isUnitLiteral(valueType *checker.Type) bool {
	return valueType.Flags()&(checker.TypeFlagsStringLiteral|checker.TypeFlagsNumberLiteral|
		checker.TypeFlagsBooleanLiteral|checker.TypeFlagsEnumLiteral|checker.TypeFlagsUndefined|
		checker.TypeFlagsNull) != 0
}
