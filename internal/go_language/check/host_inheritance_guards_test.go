package check

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// guardedRule is a syntax-only walk the guards' tests inherit for, whose own analyzer reports each file it is handed
// at its package clause, as run, which a test may swap, does.
func guardedRule(run func(pass *analysis.Pass) (any, error)) rule.Rule {
	analyzer := &analysis.Analyzer{Name: "guarded", Doc: "reports each file it walks", Run: run}
	guarded := houseRule(analyzer)
	// As the registry marks a syntax-only walk.
	analyzer.Run = rule.SyntaxOnlyRun(guarded.Name, analyzer.Run)
	return guarded
}

// reportEachFile reports each file the pass holds at its package clause.
func reportEachFile(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		pass.Reportf(file.Package, "walked")
	}
	return nil, nil
}

// handBuiltHost is a host's run holding, by ID, an entry for each of entries, each compiling its files and decoding
// to its analyses, keyed by the analyzer whose slot holds them.
func handBuiltHost(entries map[string]handBuiltEntry) *HostRun {
	host := &HostRun{entries: map[string]*hostEntry{}}
	for identifier, entry := range entries {
		decoded := &restoredPackage{analyses: entry.analyses}
		host.entries[identifier] = &hostEntry{compiledFiles: entry.files,
			decoded: func() (*restoredPackage, error) { return decoded, nil }}
	}
	return host
}

// handBuiltEntry is one entry of handBuiltHost: the files the host compiled into it, and its analyses.
type handBuiltEntry struct {
	files    []string
	analyses map[*analysis.Analyzer]restoredAnalysis
}

// Of a file's holders, the lowest ID whose entry holds the rule's slot is the one inherited from (#y6q1x2j): a lower
// holder without the slot, as a package where the rule crashed leaves, is passed over, never taken as holding no
// findings. A file no entry compiled, and a file one of whose findings edits another file, are not inherited at all.
func TestTheHostsGuardsFallToRunningTheRule(test *testing.T) {
	test.Parallel()
	guarded := guardedRule(reportEachFile)
	held := Finding{Rule: guarded.Name, File: "/project/held.go", Line: 1, Column: 1, MessageId: "walked",
		Message: "walked"}
	editsAnother := Finding{Rule: guarded.Name, File: "/project/edits.go", Line: 1, Column: 1, MessageId: "walked",
		Message: "walked", Suggestions: []Suggestion{{Message: "move it",
			Edits: []Edit{{File: "/project/held.go", Start: 0, End: 1, Text: "p"}}}}}
	host := handBuiltHost(map[string]handBuiltEntry{
		// The lower ID, with no slot for the rule.
		"a": {files: []string{"/project/held.go"}, analyses: map[*analysis.Analyzer]restoredAnalysis{}},
		"b": {files: []string{"/project/held.go", "/project/edits.go"},
			analyses: map[*analysis.Analyzer]restoredAnalysis{
				guarded.Analyzer: {findings: []Finding{held, editsAnother}},
			}},
	})
	inheritance := host.Inheritance([]rule.Rule{guarded})
	if !inheritance.Inherits(guarded.Name, held.File) {
		test.Error("held.go is not inherited, though its second holder holds the rule's slot")
	}
	if findings := inheritance.Findings(guarded.Name, held.File); len(findings) != 1 ||
		findings[0].Line != 1 || findings[0].Message != "walked" {
		test.Errorf("held.go's inherited findings are %+v, want the second holder's one", findings)
	}
	if inheritance.Inherits(guarded.Name, "/project/edits.go") {
		test.Error("edits.go is inherited, though a finding on it edits held.go")
	}
	if inheritance.Inherits(guarded.Name, "/project/unheld.go") {
		test.Error("unheld.go is inherited, though no entry of the host compiled it")
	}
}

// A build counts what it showed of the host's, never what it asked for (#y6q1x2j): over first.go, whose inherited
// finding places, and second.go, whose finding names a line past the file's end, the walk is handed second.go, which
// it judges itself, and the build counts one file and one finding inherited. A walk that returns an error while
// inheriting first.go reports none of the host's findings, its error stands, and the build counts nothing.
func TestABuildCountsWhatItShowedOfTheHosts(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	files := []*ast.File{}
	for _, name := range []string{"/project/first.go", "/project/second.go"} {
		file, parseError := parser.ParseFile(fileSet, name, "package guarded\n", 0)
		if parseError != nil {
			test.Fatal(parseError)
		}
		files = append(files, file)
	}
	walkError := errors.New("the walk failed")
	for _, failing := range []bool{false, true} {
		walked := []string{}
		guarded := guardedRule(func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				walked = append(walked, filepath.Base(fileSet.File(file.FileStart).Name()))
			}
			if failing {
				return nil, walkError
			}
			return reportEachFile(pass)
		})
		host := handBuiltHost(map[string]handBuiltEntry{
			"a": {files: []string{"/project/first.go", "/project/second.go"},
				analyses: map[*analysis.Analyzer]restoredAnalysis{guarded.Analyzer: {findings: []Finding{
					{File: "/project/first.go", Line: 1, Column: 1, MessageId: "walked", Message: "the host's"},
					{File: "/project/second.go", Line: 99, Column: 1, MessageId: "walked", Message: "unplaceable"},
				}}},
			},
		})
		inheritance := host.Inheritance([]rule.Rule{guarded})
		reported := []string{}
		pass := &analysis.Pass{Analyzer: guarded.Analyzer, Fset: fileSet, Files: files,
			ResultOf: map[*analysis.Analyzer]any{rule.Inherited: inheritance},
			Report: func(diagnostic analysis.Diagnostic) {
				position := fileSet.PositionFor(diagnostic.Pos, false)
				reported = append(reported, filepath.Base(position.Filename)+" "+diagnostic.Message)
			}}
		_, runError := guarded.Analyzer.Run(pass)
		countedFiles, countedFindings := inheritance.Counts()
		if !slices.Equal(walked, []string{"second.go"}) {
			test.Errorf("failing %t, the walk was handed %q, want second.go alone", failing, walked)
		}
		if failing {
			if !errors.Is(runError, walkError) || len(reported) != 0 || countedFiles != 0 || countedFindings != 0 {
				test.Errorf("a failing walk ended with %v, reported %q and counted %d files and %d findings, want "+
					"its error, nothing reported and nothing counted", runError, reported, countedFiles,
					countedFindings)
			}
			continue
		}
		expected := []string{"second.go walked", "first.go the host's"}
		if runError != nil || !slices.Equal(reported, expected) || countedFiles != 1 || countedFindings != 1 {
			test.Errorf("the walk ended with %v, reported %q and counted %d files and %d findings, want %q and one "+
				"of each", runError, reported, countedFiles, countedFindings, expected)
		}
	}
}
