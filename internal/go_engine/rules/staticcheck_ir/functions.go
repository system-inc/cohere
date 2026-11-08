// Package staticcheck_ir hands a rule the functions of staticcheck's intermediate representation, the very ones
// staticcheck's own checks read, built once per package by staticcheck's buildir pass. A rule written as a
// rule.FunctionCheck reads them here, and until the engine's dispatcher calls such checks itself, FunctionCheckAnalyzer
// runs one as a plain analyzer.
package staticcheck_ir

import (
	"fmt"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/staticcheck"
)

// buildirName is the name staticcheck gives the pass that builds its intermediate representation.
const buildirName = "buildir"

// Analyzer is staticcheck's buildir pass, the analyzer a check requires to read Functions. Its package,
// honnef.co/go/tools/internal/passes/buildir, is internal to staticcheck's module, so cohere cannot import it, and
// finds the analyzer instead among what staticcheck's registered checks require. Requiring that same pointer is what
// makes the build shared: the checker runs one action per analyzer per package, so the port and staticcheck's
// checks read one result.
var Analyzer = findBuildir()

// findBuildir is the analyzer named buildir that staticcheck's checks require, walked through their requirements.
// Should staticcheck rename the pass, or require two of that name, it is a stand-in whose run fails naming why, so
// every check reading Functions fails loudly rather than finding nothing.
func findBuildir() *analysis.Analyzer {
	found := []*analysis.Analyzer{}
	visited := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if visited[analyzer] {
			return
		}
		visited[analyzer] = true
		if analyzer.Name == buildirName {
			found = append(found, analyzer)
		}
		for _, required := range analyzer.Requires {
			visit(required)
		}
	}
	for _, wrapped := range staticcheck.Analyzers {
		visit(wrapped.Analyzer)
	}
	if len(found) == 1 {
		return found[0]
	}
	return &analysis.Analyzer{
		Name: buildirName,
		Doc:  "stands in for staticcheck's buildir pass, which staticcheck's checks no longer require once",
		Run: func(pass *analysis.Pass) (any, error) {
			return nil, fmt.Errorf("staticcheck's checks require %d analyzers named %q, where cohere reads "+
				"exactly one as staticcheck's intermediate representation", len(found), buildirName)
		},
	}
}

// Functions is every source function of the pass's package in staticcheck's intermediate representation, its
// anonymous functions included, read from buildir's result. The pass's analyzer must require Analyzer.
func Functions(pass *analysis.Pass) ([]*ir.Function, error) {
	return functionsOf(pass.ResultOf[Analyzer])
}

// functionsOf reads the SrcFuncs field of buildir's result, a *buildir.IR from
// honnef.co/go/tools/internal/passes/buildir, a type cohere cannot name, so the read is by reflection, and this is the
// one place it is. It fails unless the field is there and holds []*ir.Function, so a renamed or retyped field is an
// error and never an empty slice that reads as a package with nothing to report.
// TestFunctionsRefusesAResultOfAnotherShape holds that, and
// TestFunctionsReadsTheIntermediateRepresentationStaticcheckReads holds that a real result reads.
func functionsOf(result any) ([]*ir.Function, error) {
	value := reflect.ValueOf(result)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("reading %s's result: it is %T, where a pointer to a struct was expected", buildirName,
			result)
	}
	field := value.Elem().FieldByName("SrcFuncs")
	if !field.IsValid() {
		return nil, fmt.Errorf("reading %s's result: %T has no SrcFuncs field", buildirName, result)
	}
	if !field.CanInterface() {
		return nil, fmt.Errorf("reading %s's result: the SrcFuncs field of %T is unexported", buildirName, result)
	}
	functions, isFunctions := field.Interface().([]*ir.Function)
	if !isFunctions {
		return nil, fmt.Errorf("reading %s's result: the SrcFuncs field of %T is %s, where []*ir.Function was "+
			"expected", buildirName, result, field.Type())
	}
	return functions, nil
}
