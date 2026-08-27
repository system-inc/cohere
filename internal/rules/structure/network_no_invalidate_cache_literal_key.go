package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

const cacheKeyReasoning = "Cache keys are strings that have to match exactly across the file that " +
	"writes the cache and every file that invalidates it, and nothing checks that they do. A typo " +
	"produces no error and no finding: the invalidation runs, matches nothing, and the stale value " +
	"stays. Export the key from where the cache is created and import it, so a rename is a " +
	"compiler error rather than a silent miss."

var messageNoStringLiteralInvalidateCache = rule.Message{
	Id:          "noStringLiteralInvalidateCache",
	Description: "cache.invalidate takes a string literal here rather than an imported key. " + cacheKeyReasoning,
}

var messageNoTemplateLiteralInvalidateCache = rule.Message{
	Id: "noTemplateLiteralInvalidateCache",
	Description: "cache.invalidate takes a template literal here rather than an imported key. " +
		cacheKeyReasoning + " A template is worse than a plain string, since the interpolated " +
		"parts make the final key invisible at the call site.",
}

var messageNoArrayWithStringLiteralInvalidateCache = rule.Message{
	Id: "noArrayWithStringLiteralInvalidateCache",
	Description: "This element of the cache.invalidate array is a literal rather than an imported " +
		"key. " + cacheKeyReasoning,
}

// NetworkNoInvalidateCacheLiteralKey flags a literal cache key passed to cache.invalidate.
//
//	valid:   networkService.cache.invalidate(UserCacheKey)
//	valid:   networkService.cache.invalidate([UserCacheKey, PostCacheKey])
//	invalid: networkService.cache.invalidate('users')
//	invalid: networkService.cache.invalidate(`users-${id}`)
//	invalid: networkService.cache.invalidate(['users', PostCacheKey])
//
// Three message ids rather than one, because the three shapes want different repairs and the array
// case reports per element. A caller passing an array of five keys with one literal among them
// should see the finding on that element rather than on the array.
//
// Deliberately narrower than its sibling network-no-invalidate-cache-in-on-success: that rule tests
// the receiver name to decide whether the cache belongs to a service, while this one does not. The
// difference is what each is protecting. That rule is about where an invalidation lives, so it has
// to know it is looking at the network's cache; this one is about how any cache key is written, and
// a typo'd key is a defect whatever the cache belongs to.
//
// No fix. The repair is to export a key from wherever the cache is created and import it, which
// means creating a binding in another file.
var NetworkNoInvalidateCacheLiteralKey = rule.Rule{
	Name: "structure/network-no-invalidate-cache-literal-key",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				if !isCacheInvalidateCall(callExpressionCallee(call)) {
					return
				}
				if call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
					return
				}

				firstArgument := ast.SkipParentheses(call.Arguments.Nodes[0])
				if firstArgument == nil {
					return
				}

				switch firstArgument.Kind {
				case ast.KindStringLiteral:
					ctx.ReportNode(firstArgument, messageNoStringLiteralInvalidateCache)

				case ast.KindTemplateExpression, ast.KindNoSubstitutionTemplateLiteral:
					ctx.ReportNode(firstArgument, messageNoTemplateLiteralInvalidateCache)

				case ast.KindArrayLiteralExpression:
					// Reported per element, so an array of five keys with one literal among them
					// points at the literal rather than at the array.
					for _, element := range firstArgument.AsArrayLiteralExpression().Elements.Nodes {
						switch ast.SkipParentheses(element).Kind {
						case ast.KindStringLiteral:
							ctx.ReportNode(element, messageNoArrayWithStringLiteralInvalidateCache)
						case ast.KindTemplateExpression, ast.KindNoSubstitutionTemplateLiteral:
							ctx.ReportNode(element, messageNoTemplateLiteralInvalidateCache)
						}
					}
				}
			},
		}
	},
}

// callExpressionCallee returns a call's callee with grouping removed.
func callExpressionCallee(call *ast.CallExpression) *ast.Node {
	if call == nil {
		return nil
	}
	return ast.SkipParentheses(call.Expression)
}

// isCacheInvalidateCall reports a `<something>.cache.invalidate` callee, without judging the
// receiver. The receiver test lives in network-no-invalidate-cache-in-on-success, which needs to
// know whose cache it is; this shape check is the part both rules share.
func isCacheInvalidateCall(callee *ast.Node) bool {
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return false
	}

	invalidate := callee.AsPropertyAccessExpression()
	if name := invalidate.Name(); name == nil || name.Text() != "invalidate" {
		return false
	}

	cacheAccess := ast.SkipParentheses(invalidate.Expression)
	if cacheAccess == nil || cacheAccess.Kind != ast.KindPropertyAccessExpression {
		return false
	}

	name := cacheAccess.AsPropertyAccessExpression().Name()
	return name != nil && name.Text() == "cache"
}
