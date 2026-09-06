package no_shadow_probe

import (
	"fmt"
	"sort"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/rule_testing"
)

// Probe: for the shapes the corpus reports on, does a Locals walk find the
// inner binding AND an outer binding of the same name?
func TestShadowShapesAreVisible(t *testing.T) {
	cases := map[string]string{
		"typeInBlock":     "type T = 1;\n{\n  type T = 2;\n}\n",
		"typeParamShadow": "type T = 1;\nfunction foo<T>(arg: T) {}\n",
		"nestedTypeParam": "function foo<T>() {\n  return function <T>() {};\n}\n",
		"varInFunction":   "var a = 3;\nfunction b() {\n  var a = 10;\n}\n",
		"paramShadow":     "var a = 3;\nfunction b(a) {}\n",
		"blockLet":        "let x = 1;\n{\n  let x = 2;\n}\n",
		"catchParam":      "var err = 1;\ntry {} catch (err) {}\n",
		"enumMember":      "const A = 1;\nenum E {\n  A = 2,\n}\n",
		"classGeneric":    "class C<T> {\n  static m<T>() {}\n}\n",
		"namespaceMerge":  "import type { Foo } from 'foo';\ndeclare module 'foo' {\n  interface Foo {}\n}\n",
		"arrowParam":      "var a = 1;\nconst f = (a) => a;\n",
		"funcExprName":    "var a = 1;\nvar b = function a() {};\n",
	}
	names := []string{}
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		source := cases[name]
		scopes := []string{}
		probe := rule.Rule{
			Name:             "no-shadow-probe-coverage",
			NeedsTypeChecker: true,
			Run: func(ctx rule.Context, options any) rule.Listeners {
				return rule.Listeners{
					ast.KindSourceFile: func(node *ast.Node) {
						var visit func(*ast.Node, int)
						visit = func(current *ast.Node, depth int) {
							if current == nil {
								return
							}
							if ast.IsLocalsContainer(current) {
								locals := ast.GetLocals(current)
								if len(locals) > 0 {
									keys := []string{}
									for key := range locals {
										keys = append(keys, string(key))
									}
									sort.Strings(keys)
									scopes = append(scopes, fmt.Sprintf("d%d %s %v", depth, current.Kind.String(), keys))
								}
							}
							current.ForEachChild(func(child *ast.Node) bool {
								visit(child, depth+1)
								return false
							})
						}
						visit(node, 0)
					},
				}
			},
		}
		rule_testing.RunTyped(t, probe, "probe.ts", source)
		t.Logf("=== %s", name)
		for _, line := range scopes {
			t.Logf("      %s", line)
		}
		if len(scopes) == 0 {
			t.Errorf("%s: no populated scopes at all", name)
		}
	}
}
