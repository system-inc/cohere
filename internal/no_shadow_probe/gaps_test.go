package no_shadow_probe

import (
	"fmt"
	"sort"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/rule_testing"
)

// Probe: are enum member scope and function-expression-name scope reachable by
// ANY means (Locals, symbol Members, symbol Exports), or genuinely absent?
func TestEnumAndFunctionExpressionNameScopes(t *testing.T) {
	source := "const A = 1;\nenum E {\n  A = 2,\n}\nvar b = function a() {};\n"
	lines := []string{}
	probe := rule.Rule{
		Name:             "no-shadow-probe-gaps",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					var visit func(*ast.Node)
					visit = func(current *ast.Node) {
						if current == nil {
							return
						}
						switch current.Kind {
						case ast.KindEnumDeclaration, ast.KindFunctionExpression:
							localsLength := -1
							if ast.IsLocalsContainer(current) {
								localsLength = len(ast.GetLocals(current))
							}
							lines = append(lines, fmt.Sprintf("%s isLocalsContainer=%v localsLen=%d",
								current.Kind.String(), ast.IsLocalsContainer(current), localsLength))
							if symbol := ctx.TypeChecker.GetSymbolAtLocation(current.Name()); symbol != nil {
								members := []string{}
								for name := range symbol.Members {
									members = append(members, string(name))
								}
								sort.Strings(members)
								exports := []string{}
								for name := range symbol.Exports {
									exports = append(exports, string(name))
								}
								sort.Strings(exports)
								lines = append(lines, fmt.Sprintf("   symbol members=%v exports=%v", members, exports))
							} else {
								lines = append(lines, "   symbol=nil")
							}
						}
						current.ForEachChild(func(child *ast.Node) bool {
							visit(child)
							return false
						})
					}
					visit(node)
				},
			}
		},
	}
	rule_testing.RunTyped(t, probe, "probe.ts", source)
	for _, line := range lines {
		t.Logf("%s", line)
	}
	if len(lines) == 0 {
		t.Fatal("probe saw neither an enum nor a function expression")
	}
}
