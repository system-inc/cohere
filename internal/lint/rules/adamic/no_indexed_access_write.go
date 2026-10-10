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
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.SourceFile == nil {
			return nil
		}
		walker := flow.WalkerFor(ctx)
		var sourceRoot *checker.Type
		judge := func(pair flow.Pair) (bool, bool) {
			if !flow.IsTypeParameterIndexedAccess(pair.Target) {
				return false, true
			}
			if pair.Source.Flags()&checker.TypeFlagsNever != 0 {
				return false, false
			}
			if projectsIndexedAccess(ctx.TypeChecker, sourceRoot, pair.Target, pair.Path) {
				return false, false
			}
			same := pair.Source.Flags()&checker.TypeFlagsIndexedAccess != 0 &&
				checker.Checker_isTypeIdenticalTo(ctx.TypeChecker, pair.Source, pair.Target)
			return !same, false
		}
		return walker.Listeners(func(site flow.Site) {
			if site.Spread || site.Method || readsIndexedAccess(ctx.TypeChecker, site.Node, site.Target) {
				return
			}
			sourceRoot = site.Source
			found, wrong := walker.Walk(site, judge)
			if !wrong {
				return
			}
			constraint := checker.Checker_getBaseConstraintOfType(ctx.TypeChecker, found.Target)
			constraintText := "unknown"
			if constraint != nil {
				constraintText = type_checking.StableTypeText(ctx.TypeChecker, constraint)
			}
			ctx.ReportNode(site.Node, rule.Message{
				Id: "indexedAccessWrite",
				Description: indexedAccessWriteText.Render(map[string]string{
					"slot":       type_checking.StableTypeText(ctx.TypeChecker, found.Target),
					"source":     type_checking.StableTypeText(ctx.TypeChecker, found.Source),
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
	node = ast.SkipParentheses(node)
	access := target.AsIndexedAccessType()
	var receiver *ast.Node
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		property := node.AsPropertyAccessExpression()
		key := access.IndexType()
		if key.Flags()&checker.TypeFlagsStringLiteral == 0 || key.AsLiteralType().Value() != property.Name().Text() {
			return false
		}
		receiver = property.Expression
	case ast.KindElementAccessExpression:
		element := node.AsElementAccessExpression()
		if !checker.Checker_isTypeIdenticalTo(typeChecker, typeChecker.GetTypeAtLocation(element.ArgumentExpression), access.IndexType()) {
			return false
		}
		receiver = element.Expression
	default:
		return false
	}
	return checker.Checker_isTypeIdenticalTo(typeChecker, typeChecker.GetTypeAtLocation(receiver), access.ObjectType()) ||
		readsIndexedAccess(typeChecker, receiver, access.ObjectType())
}

// The walk expands a T source through its constraint. Its property still has
// type T['value'], even when that constraint's property is number. Preserve
// the original source for indexed slots reached through the same property path.
func projectsIndexedAccess(typeChecker *checker.Checker, source, target *checker.Type, path []flow.Step) bool {
	if source == nil || len(path) == 0 {
		return false
	}
	for i := len(path) - 1; i >= 0; i-- {
		if target.Flags()&checker.TypeFlagsIndexedAccess == 0 || path[i].Kind != flow.StepProperty {
			return false
		}
		access := target.AsIndexedAccessType()
		key := access.IndexType()
		if key.Flags()&checker.TypeFlagsStringLiteral == 0 || key.AsLiteralType().Value() != path[i].Name {
			return false
		}
		target = access.ObjectType()
	}
	return checker.Checker_isTypeIdenticalTo(typeChecker, source, target)
}
