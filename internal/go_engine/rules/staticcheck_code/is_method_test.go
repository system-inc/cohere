package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// methodComparison is what one package's side by side run of IsMethod found.
type methodComparison struct {
	// questions counts the questions asked of each side.
	questions int
	// mismatches describes each question whose two answers differed.
	mismatches []string
	// met counts each label of requiredMethodLabels the run met.
	met map[string]int
}

// methodNames are the names IsMethod is asked with: none, which any name passes, the name the fixture's seekers
// share, and the name of a method of the same signature under another name.
var methodNames = []string{"", "Seek", "Find"}

// methodSignatures are the signatures IsMethod is asked with, by a name for the mismatches: io.Seeker's Seek, as
// SA1013 asks it, and io.Writer's Write, which no fixture method has.
var methodSignatures = map[string]*types.Signature{
	"(io.Seeker).Seek":  knowledge.Signatures["(io.Seeker).Seek"],
	"(io.Writer).Write": knowledge.Signatures["(io.Writer).Write"],
}

// methodAnalyzer asks both sides of every selector of a package, its result a methodComparison. It requires
// staticcheck's generated pass, which PassContext reads.
var methodAnalyzer = &analysis.Analyzer{
	Name:       "comparemethods",
	Doc:        "asks staticcheck's code.IsMethod and the port of every selector, and compares them",
	Requires:   []*analysis.Analyzer{generated.Analyzer},
	Run:        compareMethods,
	ResultType: reflect.TypeFor[*methodComparison](),
}

// Both sides answer alike for every selector of the methods fixture, under every name and signature asked: method
// values of concrete, pointer, interface, embedded and generic types, method expressions, fields holding functions,
// qualified identifiers and a method value taken without a call. The test fails unless each way IsMethod can answer
// was met, true, and false for a name, a selection that is no method value, a selector that is no selection and a
// signature, so a fixture that stops holding one cannot pass by asking nothing.
func TestIsMethodMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"methods"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading methods: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{methodAnalyzer}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing methods: %v", analyzeError)
	}
	questions := 0
	met := map[string]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		compared, isComparison := action.Result.(*methodComparison)
		if !isComparison {
			test.Fatalf("running %s: its result is %T, where a comparison was expected", action, action.Result)
		}
		questions += compared.questions
		for _, mismatch := range compared.mismatches {
			test.Error(mismatch)
		}
		for label, count := range compared.met {
			met[label] += count
		}
	}
	test.Logf("%d questions asked of each side", questions)
	for _, label := range requiredMethodLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// compareMethods asks both sides of every selector of the pass's package under every name and signature, recording
// each question they answer differently and the labels it met.
func compareMethods(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	compared := &methodComparison{met: map[string]int{}}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, isSelector := node.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			selection, isSelection := pass.TypesInfo.Selections[selector]
			for _, name := range methodNames {
				for signatureName, signature := range methodSignatures {
					upstream := code.IsMethod(pass, selector, name, signature)
					port := staticcheck_code.IsMethod(context, selector, name, signature)
					compared.questions++
					if upstream != port {
						compared.mismatches = append(compared.mismatches, fmt.Sprintf(
							"IsMethod(%q, %s) at %s: staticcheck answered %t, the port %t", name, signatureName,
							pass.Fset.Position(selector.Pos()), upstream, port))
					}
					switch {
					case upstream:
						compared.met["true"]++
					case name != "" && selector.Sel.Name != name:
						compared.met["false for a name"]++
					case !isSelection:
						compared.met["false for a selector that is no selection"]++
					case selection.Kind() != types.MethodVal:
						compared.met["false for a selection that is no method value"]++
					default:
						compared.met["false for a signature"]++
					}
				}
			}
			return true
		})
	}
	return compared, nil
}

// requiredMethodLabels are what the fixture must meet for the comparison to prove each way IsMethod answers.
func requiredMethodLabels() []string {
	return []string{
		"true",
		"false for a name",
		"false for a selector that is no selection",
		"false for a selection that is no method value",
		"false for a signature",
	}
}
