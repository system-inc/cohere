package check

import (
	"go/ast"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// fullKindUnits are the full-kind fixture's units that hold Holder by name or by pointer: Holder's unexported slots is
// two ints on linux and four on darwin, and its name, its exported field and its methods are the same in both. They
// are the kinds of use build_dependence's layout fixture judges independent of linux (TestLayoutVerdictsOnLinux's
// Forward, PassesHolder, Shell and Keep), each with findings of rules without FullKind, which the layout fixture's
// units have none of.
var fullKindUnits = []string{"Shell", "Forward", "Keep", "Wrap", "Same"}

// fullKindBuild loads the full-kind fixture as the build goos makes it, cgo off and no workspace file.
func fullKindBuild(test *testing.T, goos string) *packages.Package {
	test.Helper()
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "full_kind"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	loaded, loadError := load.Packages(directory, []string{"."}, []string{"GOOS=" + goos, "CGO_ENABLED=0",
		"GOWORK=off", "GOFLAGS="}, nil)
	if loadError != nil || len(loaded.Roots) != 1 {
		test.Fatalf("loading the full-kind fixture for %s: %v", goos, loadError)
	}
	return loaded.Roots[0]
}

// declarationUnits are the package's top-level functions and type specifications, by name, each spanning its
// declaration.
func declarationUnits(analyzed *packages.Package) map[string]functionUnit {
	units := map[string]functionUnit{}
	add := func(name string, node ast.Node) {
		start, _ := positionAt(analyzed.Fset, node.Pos())
		end, _ := positionAt(analyzed.Fset, node.End())
		units[name] = functionUnit{name: name, start: start, end: end.offset}
	}
	for _, file := range analyzed.Syntax {
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				add(declaration.Name.Name, declaration)
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					if typeSpecification, isType := specification.(*ast.TypeSpec); isType {
						add(typeSpecification.Name.Name, declaration)
					}
				}
			}
		}
	}
	return units
}

// fullKindFindings runs registered over the full-kind fixture as goos builds it and is what it found in each of
// fullKindUnits, as one line each.
func fullKindFindings(test *testing.T, registered []rule.Rule, goos string) map[string][]string {
	test.Helper()
	fixture := fullKindBuild(test, goos)
	run, runError := runIsolated(registered, []*packages.Package{fixture}, nil)
	if runError != nil {
		test.Fatal(runError)
	}
	for ruleName, crash := range run.crashed[fixture] {
		test.Errorf("%s crashed on the full-kind fixture for %s: %s", ruleName, goos, crash)
	}
	units := declarationUnits(fixture)
	found := map[string][]string{}
	for _, finding := range run.findings[fixture] {
		for _, name := range fullKindUnits {
			if unitHolds(units[name], finding.at) {
				found[name] = append(found[name], findingLine(finding))
			}
		}
	}
	for _, name := range fullKindUnits {
		slices.Sort(found[name])
	}
	return found
}

// Every rule without FullKind finds the same in the full-kind fixture's units that hold Holder by name or by pointer
// on darwin and on linux, which differ only in Holder's unexported slots (design section 4): such a rule reads no more
// of a named type than its name, so a unit the dependence judgment calls independent has the host's findings in the
// other build. The rules with FullKind are left to block 4, which reads every named type they see in full.
func TestEveryRuleWithoutFullKindFindsTheSameWhereOnlyAnUnexportedFieldDiffers(test *testing.T) {
	test.Parallel()
	declared, declaredError := rules.DeclaredInputs()
	if declaredError != nil {
		test.Fatal(declaredError)
	}
	notFullKind := slices.DeleteFunc(rules.All(), func(registered rule.Rule) bool {
		return declared[registered.Name].FullKind
	})
	host, linux := fullKindFindings(test, notFullKind, "darwin"), fullKindFindings(test, notFullKind, "linux")
	compared := 0
	for _, name := range fullKindUnits {
		compared += len(host[name])
		if !slices.Equal(host[name], linux[name]) {
			test.Errorf("%s: darwin finds %q, linux %q", name, host[name], linux[name])
		}
	}
	test.Logf("%d rules without full kind, %d findings compared in %q", len(notFullKind), compared, fullKindUnits)
	for _, name := range fullKindUnits {
		if len(host[name]) == 0 {
			test.Errorf("%s: no rule without full kind finds anything there, so it compares nothing", name)
		}
	}
}
