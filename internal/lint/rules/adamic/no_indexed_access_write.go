package adamic

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/checking/flow"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

var indexedAccessWriteText = policy.MessageOf("adamic/no-indexed-access-write", "indexedAccessWrite")

// NoIndexedAccessWrite proves a generic body for every permitted instantiation (#xzpba0r,
// Adamic step 16). A constraint permits number, but T['value'] may be 1 at the call.
// Only a value whose own type is that indexed access can fill the slot. The supplied
// tsc 6.0.3 / Node 24 probe instantiates 1, stores 2, and throws reading names[one].
// This is a separate rule: a readonly primitive slot has no mutable container to widen.
// The wider ruling about all parameter-retaining relations awaits a Node probe per shape.
var NoIndexedAccessWrite = rule.Rule{
	Name:             "adamic/no-indexed-access-write",
	NeedsTypeChecker: true,
	Run: func(ruleContext rule.Context, options any) rule.Listeners {
		if ruleContext.TypeChecker == nil || ruleContext.SourceFile == nil {
			return nil
		}
		walker := flow.WalkerFor(ruleContext)
		var sourceRoot *checker.Type
		judge := func(pair flow.Pair) (bool, bool) {
			if !flow.IsTypeParameterIndexedAccess(pair.Target) {
				return false, true
			}
			if pair.Source.Flags()&checker.TypeFlagsNever != 0 {
				return false, false
			}
			if projectsIndexedAccess(ruleContext.TypeChecker, sourceRoot, pair.Target, pair.Path) ||
				returnsIndexedAccess(ruleContext.TypeChecker, sourceRoot, pair.Target, pair.Path) {
				return false, false
			}
			same := pair.Source.Flags()&checker.TypeFlagsIndexedAccess != 0 &&
				checker.Checker_isTypeIdenticalTo(ruleContext.TypeChecker, pair.Source, pair.Target)
			return !same, false
		}
		return walker.Listeners(func(site flow.Site) {
			// Spreads can copy a concrete property into T[K]; contextual methods can
			// return one or accept one through a bivariant parameter. Their Node probes
			// are spread.ts, method-return.ts and method-argument.ts (#xzpba0r).
			if readsIndexedAccess(ruleContext.TypeChecker, site.Node, site.Target) {
				return
			}
			sourceRoot = site.Source
			found, wrong := walker.WalkWith(site, judge, flow.WalkOptions{Methods: true})
			if !wrong {
				return
			}
			constraint := checker.Checker_getBaseConstraintOfType(ruleContext.TypeChecker, found.Target)
			constraintText := "unknown"
			if constraint != nil {
				constraintText = type_checking.StableTypeText(ruleContext.TypeChecker, constraint)
			}
			ruleContext.ReportNode(site.Node, rule.Message{
				Id: "indexedAccessWrite",
				Description: indexedAccessWriteText.Render(map[string]string{
					"slot":       type_checking.StableTypeText(ruleContext.TypeChecker, found.Target),
					"source":     type_checking.StableTypeText(ruleContext.TypeChecker, found.Source),
					"constraint": constraintText,
				}),
			})
		})
	},
}

// A dot read is reported by tsgo as the constraint's property type (number),
// even though its receiver is T. Recover the indexed access from the read's
// receiver and key, including nested reads; a read from the constraint itself
// cannot pass this check.
func readsIndexedAccess(typeChecker *checker.Checker, node *ast.Node, target *checker.Type) bool {
	if node == nil || !flow.IsTypeParameterIndexedAccess(target) {
		return false
	}
	unwrapped := ast.SkipParentheses(node)
	access := target.AsIndexedAccessType()
	var receiver *ast.Node
	switch unwrapped.Kind {
	case ast.KindPropertyAccessExpression:
		property := unwrapped.AsPropertyAccessExpression()
		key := access.IndexType()
		if key.Flags()&checker.TypeFlagsStringLiteral == 0 || key.AsLiteralType().Value() != property.Name().Text() {
			return false
		}
		receiver = property.Expression
	case ast.KindElementAccessExpression:
		element := unwrapped.AsElementAccessExpression()
		if !checker.Checker_isTypeIdenticalTo(
			typeChecker,
			typeChecker.GetTypeAtLocation(element.ArgumentExpression),
			access.IndexType(),
		) {
			return false
		}
		receiver = element.Expression
	default:
		return false
	}
	return checker.Checker_isTypeIdenticalTo(
		typeChecker,
		typeChecker.GetTypeAtLocation(receiver),
		access.ObjectType(),
	) ||
		readsIndexedAccess(typeChecker, receiver, access.ObjectType())
}

// The walk expands a T source through its constraint. Its property still has
// type T['value'], even when that constraint's property is number. Preserve
// the original source for indexed slots reached through the same property path.
func projectsIndexedAccess(typeChecker *checker.Checker, source, target *checker.Type, path []flow.Step) bool {
	if source == nil || len(path) == 0 {
		return false
	}
	walked := target
	for index := len(path) - 1; index >= 0; index-- {
		if walked.Flags()&checker.TypeFlagsIndexedAccess == 0 || path[index].Kind != flow.StepProperty {
			return false
		}
		access := walked.AsIndexedAccessType()
		key := access.IndexType()
		if key.Flags()&checker.TypeFlagsStringLiteral == 0 || key.AsLiteralType().Value() != path[index].Name {
			return false
		}
		walked = access.ObjectType()
	}
	return checker.Checker_isTypeIdenticalTo(typeChecker, source, walked)
}

// An inferred function returning source.value has a number return type in tsgo,
// just as the read itself does. Preserve that read only when every return of
// the actual implementation has the indexed type; nested functions do not count.
func returnsIndexedAccess(typeChecker *checker.Checker, source, target *checker.Type, path []flow.Step) bool {
	if source == nil || len(path) == 0 || path[len(path)-1].Kind != flow.StepReturn {
		return false
	}
	walked := source
	for _, step := range path[:len(path)-1] {
		if step.Kind != flow.StepProperty {
			return false
		}
		property := checker.Checker_getPropertyOfType(typeChecker, walked, step.Name)
		if property == nil {
			return false
		}
		walked = checker.Checker_getTypeOfSymbol(typeChecker, property)
	}
	signatures := checker.Checker_getSignaturesOfType(typeChecker, walked, checker.SignatureKindCall)
	if len(signatures) != 1 {
		return false
	}
	declaration := signatures[0].Declaration()
	if declaration == nil || declaration.Body() == nil {
		return false
	}
	isIndexedReturn := func(expression *ast.Node) bool {
		if expression == nil {
			return false
		}
		own := typeChecker.GetTypeAtLocation(expression)
		return own != nil && (own.Flags()&checker.TypeFlagsNever != 0 ||
			checker.Checker_isTypeIdenticalTo(typeChecker, own, target) ||
			readsIndexedAccess(typeChecker, expression, target))
	}
	// An expression-bodied arrow has exactly one return: its body expression.
	if !ast.IsBlock(declaration.Body()) {
		return isIndexedReturn(declaration.Body())
	}
	seen, valid := 0, true
	var visit func(*ast.Node) bool
	visit = func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			return false
		}
		if node.Kind == ast.KindReturnStatement {
			seen++
			valid = isIndexedReturn(node.AsReturnStatement().Expression)
			return !valid
		}
		node.ForEachChild(visit)
		return !valid
	}
	declaration.Body().ForEachChild(visit)
	return seen > 0 && valid
}
