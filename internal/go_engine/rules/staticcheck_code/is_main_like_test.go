package staticcheck_code_test

import (
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// mainLikeAnswers is what one package's side by side run of IsMainLike answered.
type mainLikeAnswers struct {
	upstream bool
	port     bool
}

// mainLikeAnalyzer asks both sides of each package, its result the two answers. It requires staticcheck's generated
// pass, which PassContext does.
var mainLikeAnalyzer = &analysis.Analyzer{
	Name:     "comparemainlike",
	Doc:      "asks staticcheck's code.IsMainLike and the port of every package, and compares them",
	Requires: []*analysis.Analyzer{generated.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		context, contextError := staticcheck_report.PassContext(pass)
		if contextError != nil {
			return nil, contextError
		}
		return &mainLikeAnswers{upstream: code.IsMainLike(pass), port: staticcheck_code.IsMainLike(context)}, nil
	},
	ResultType: reflect.TypeFor[*mainLikeAnswers](),
}

// Both sides answer alike for every package of the main_like fixture and the stand-ins for cobra it imports: a
// package main, a package importing cobra, one importing it under another name, one importing only cobra's doc
// subpackage, which imports cobra, a plain library, and the stand-ins themselves. The test fails unless both answers
// were met, so a fixture that stops holding one cannot pass by asking nothing.
func TestIsMainLikeMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	patterns := []string{"main_like/...", "github.com/spf13/cobra/..."}
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		// A fixture that does not type-check is one the checker skips, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{mainLikeAnalyzer}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	met := map[bool]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		answers, isAnswers := action.Result.(*mainLikeAnswers)
		if !isAnswers {
			test.Fatalf("running %s: its result is %T, where answers were expected", action, action.Result)
		}
		test.Logf("%s: staticcheck %t, the port %t", action.Package.PkgPath, answers.upstream, answers.port)
		met[answers.upstream]++
		if answers.upstream != answers.port {
			test.Errorf("of %s staticcheck answered %t and the port %t", action.Package.PkgPath, answers.upstream,
				answers.port)
		}
	}
	for _, answer := range []bool{true, false} {
		if met[answer] == 0 {
			test.Errorf("no package of the fixture is main-like %t, so the comparison proves nothing of it", answer)
		}
	}
}
