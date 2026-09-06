package no_shadow_probe

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/rule_testing"
)

// Probe: does a Locals symbol carry enough flags to answer the value/type split
// (isValueVariable / isTypeVariable) that no-shadow's DEFAULT options depend on?
func TestSymbolFlagsExpressValueTypeSplit(t *testing.T) {
	source := `
type OnlyType = 1;
const onlyValue = 2;
class Both {}
interface Iface {}
enum Enu { A }
function fn<TP>(param: TP) { return param; }
namespace NS { export const x = 1; }
import type { Thing } from './other';
`
	seen := map[string]string{}
	probe := rule.Rule{
		Name:             "no-shadow-probe-flags",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					var visit func(*ast.Node)
					visit = func(current *ast.Node) {
						if current == nil {
							return
						}
						if ast.IsLocalsContainer(current) {
							for name, symbol := range ast.GetLocals(current) {
								if symbol == nil {
									continue
								}
								kinds := ""
								if len(symbol.Declarations) > 0 {
									kinds = symbol.Declarations[0].Kind.String()
								}
								seen[string(name)] = fmt.Sprintf("flags=0x%x decls=%d firstKind=%s",
									uint32(symbol.Flags), len(symbol.Declarations), kinds)
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
	if len(seen) == 0 {
		t.Fatal("no symbols at all")
	}
	for _, name := range []string{"OnlyType", "onlyValue", "Both", "Iface", "Enu", "TP", "param", "NS", "Thing", "fn"} {
		if info, ok := seen[name]; ok {
			t.Logf("%-12s %s", name, info)
		} else {
			t.Logf("%-12s MISSING", name)
		}
	}
	// Assertion with teeth: a pure type and a pure value must be distinguishable.
	if seen["OnlyType"] == seen["onlyValue"] {
		t.Errorf("type and value symbols are indistinguishable: %q", seen["OnlyType"])
	}
}
