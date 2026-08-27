package no_shadow_probe

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rule_testing"
)

// Probe: does the TypeScript parser give us a populated per-scope symbol table
// (Locals) on the containers no-shadow needs to walk?
func TestLocalsArePopulated(t *testing.T) {
	source := `
const outer = 1;
function f() {
  const outer = 2;
  return outer;
}
function g(outer: string) { return outer; }
`
	var report []string
	probe := rule.Rule{
		Name:             "no-shadow-probe",
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
							locals := ast.GetLocals(current)
							names := []string{}
							for name := range locals {
								names = append(names, string(name))
							}
							report = append(report, current.Kind.String()+":"+joinSorted(names))
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
	if len(report) == 0 {
		t.Fatalf("no locals containers seen at all; substrate absent")
	}
	for _, line := range report {
		t.Logf("container %s", line)
	}
	// Control + assertion: the source file must carry `outer`, and some nested
	// container must ALSO carry `outer`. That pair is exactly what shadowing is.
	sawOuterTwice := 0
	for _, line := range report {
		if containsName(line, "outer") {
			sawOuterTwice++
		}
	}
	if sawOuterTwice < 2 {
		t.Errorf("expected `outer` in at least 2 scopes, saw it in %d; scope graph cannot express shadowing", sawOuterTwice)
	}
}

func joinSorted(names []string) string {
	out := ""
	for index, name := range names {
		if index > 0 {
			out += ","
		}
		out += name
	}
	return out
}

func containsName(line string, name string) bool {
	for index := 0; index+len(name) <= len(line); index++ {
		if line[index:index+len(name)] == name {
			return true
		}
	}
	return false
}
