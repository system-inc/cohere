package rules

import (
	"fmt"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/unused"
)

// unusedReport is staticcheck's U1000 as an analyzer that reports. unused.Analyzer reports nothing itself:
// it returns an unused.Result, and staticcheck's own runner turns that into findings, so this does the
// same, with the runner's message and U1000 as the finding's message id.
//
// staticcheck's runner also merges the result across a package's variants, so a function only its tests
// call is not reported from the build without them. This reports one variant at a time, so that merge is
// the engine's to make if it loads test variants.
var unusedReport = &analysis.Analyzer{
	Name:     "unusedreport",
	Doc:      "reports the unused code staticcheck's U1000 finds",
	Requires: []*analysis.Analyzer{unused.Analyzer.Analyzer},
	Run:      runUnusedReport,
}

func runUnusedReport(pass *analysis.Pass) (any, error) {
	result, isResult := pass.ResultOf[unused.Analyzer.Analyzer].(unused.Result)
	if !isResult {
		return nil, fmt.Errorf("reading U1000's result: it is %T", pass.ResultOf[unused.Analyzer.Analyzer])
	}

	// An unused object names its file by path, so each path maps back to the token file a position needs.
	tokenFiles := map[string]*token.File{}
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.Pos())
		if tokenFile == nil {
			continue
		}
		tokenFiles[tokenFile.Name()] = tokenFile
	}
	for _, object := range result.Unused {
		tokenFile, found := tokenFiles[object.Position.Filename]
		if !found {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:      tokenFile.Pos(object.Position.Offset),
			Category: unused.Analyzer.Analyzer.Name,
			Message:  fmt.Sprintf("%s %s is unused", object.Kind, object.Name),
		})
	}
	return nil, nil
}
