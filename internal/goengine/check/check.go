// Package check is the Go engine's lint phase: every enabled rule over the loaded packages once, through
// checker.Analyze, with facts flowing across packages. One broken analyzer never takes the run down and is never
// silent: a panic, an error or a failed prerequisite becomes a crash, which makes the run incomplete, and every
// other rule's findings still come back.
//
// It takes and returns plain values; the engine turns them into the lint record and finding records.
package check

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/goengine/rule"
)

// Options says how a run schedules its work.
type Options struct {
	// SingleThreaded runs one action at a time, for `--single-threaded`; the findings are the same either way.
	SingleThreaded bool
}

// Finding is one diagnostic of one rule. Positions are of the file as the engine read it, not as a `//line`
// directive renames it, so they agree with the edits' offsets and with the suppression comments in that file.
type Finding struct {
	Rule string
	File string

	// Line and Column are 1-based, the column counted in UTF-8 bytes, as go/token counts it.
	Line   int
	Column int

	// EndLine and EndColumn are where the diagnostic's range ends, exclusive, or 0 when the analyzer gave no end.
	EndLine   int
	EndColumn int

	// MessageId is the diagnostic's Category when it has one, which is where a house rule puts it, else the
	// ported rule's upstream name, else the analyzer's name.
	MessageId string
	Message   string

	Suggestions []Suggestion
}

// Suggestion is one repair an analyzer offered, which the engine offers and never applies.
type Suggestion struct {
	Message string
	Edits   []Edit
}

// Edit replaces the bytes from Start to End of File with Text. The offsets are 0-based UTF-8 bytes. File is
// kept because an analyzer may edit a file of the package other than the one its finding is in.
type Edit struct {
	File  string
	Start int
	End   int
	Text  string
}

// Crash is a rule that did not finish on one package. File is the package's first Go file, since a rule runs
// per package and the record names a file.
type Crash struct {
	File  string
	Error string
}

// Result is one run.
//
// The lint record's counts come from here. RulesRun is rulesRun, FilesWalked is filesWalked, NodesVisited is
// nodesVisited, and len(Findings) is findings before suppression. FindingsByRule is what rulesWatchedAndQuiet
// counts: a rule with 0 here ran on every package it was given and reported nothing. rulesSilent, the rules
// that listened to no file at all, is the engine's to name, since only it knows which rules the config kept from
// every file before they reached this run; a run over no files leaves every rule it was given silent.
type Result struct {
	// Findings are every diagnostic of every rule that finished, each once by rule, position and message, sorted
	// by file, line, column and rule. A rule that crashed on a package contributes no findings for it, since what
	// it reported before it stopped is not what it would have reported.
	Findings []Finding

	// Crashes are each rule that did not finish on a package, once each, sorted by file.
	Crashes []Crash

	RulesRun int

	// FilesWalked is the distinct Go files of the analyzed packages, the files the rules were given.
	FilesWalked int

	// NodesVisited is the nodes of one ast.Inspect over those files.
	NodesVisited int

	// FindingsByRule is each rule's number of findings, 0 included, by rule name.
	FindingsByRule map[string]int

	Elapsed time.Duration
}

// findingKey is what makes two findings the same finding.
type findingKey struct {
	rule    string
	file    string
	line    int
	column  int
	message string
}

// Run runs every rule over the analyzed packages, which are load.Loaded.Packages, loaded with every dependency
// from source. The rules are the enabled ones. It returns an error only when the rules cannot be run at all;
// what a rule gets wrong on a package is a Crash.
func Run(analyzed []*packages.Package, rules []rule.Rule, options Options) (*Result, error) {
	started := time.Now()
	ruleOf := make(map[*analysis.Analyzer]rule.Rule, len(rules))
	analyzers := make([]*analysis.Analyzer, 0, len(rules))
	for _, enabled := range rules {
		if enabled.Analyzer == nil {
			return nil, fmt.Errorf("running rule %s: it has no analyzer", enabled.Name)
		}
		if earlier, taken := ruleOf[enabled.Analyzer]; taken {
			return nil, fmt.Errorf("running rules %s and %s: they share analyzer %s, so no finding could say "+
				"which rule it is", earlier.Name, enabled.Name, enabled.Analyzer.Name)
		}
		ruleOf[enabled.Analyzer] = enabled
		analyzers = append(analyzers, enabled.Analyzer)
	}
	if wrapError := wrapRuns(analyzers); wrapError != nil {
		return nil, wrapError
	}
	graph, analyzeError := checker.Analyze(analyzers, analyzed, &checker.Options{Sequential: options.SingleThreaded})
	if analyzeError != nil {
		return nil, fmt.Errorf("analyzing %d packages: %w", len(analyzed), analyzeError)
	}

	result := &Result{RulesRun: len(rules), FindingsByRule: make(map[string]int, len(rules))}
	for _, enabled := range rules {
		result.FindingsByRule[enabled.Name] = 0
	}
	seenFindings := map[findingKey]bool{}
	seenCrashes := map[Crash]bool{}
	for _, action := range graph.Roots {
		actionRule, known := ruleOf[action.Analyzer]
		if !known {
			return nil, fmt.Errorf("reading the analysis of %s: analyzer %s is no rule's", action.Package.ID,
				action.Analyzer.Name)
		}
		if action.Err != nil {
			crash := Crash{File: firstFile(action.Package), Error: crashSentence(actionRule, action)}
			if !seenCrashes[crash] {
				seenCrashes[crash] = true
				result.Crashes = append(result.Crashes, crash)
			}
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			found := newFinding(actionRule, action.Package.Fset, diagnostic)
			key := findingKey{found.Rule, found.File, found.Line, found.Column, found.Message}
			if seenFindings[key] {
				continue
			}
			seenFindings[key] = true
			result.Findings = append(result.Findings, found)
			result.FindingsByRule[found.Rule]++
		}
	}
	slices.SortFunc(result.Findings, func(left, right Finding) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			cmp.Compare(left.Rule, right.Rule),
			// The rest only breaks ties, so the same input always reads the same.
			cmp.Compare(left.Message, right.Message),
		)
	})
	slices.SortFunc(result.Crashes, func(left, right Crash) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Error, right.Error))
	})
	result.FilesWalked, result.NodesVisited = walk(analyzed)
	result.Elapsed = time.Since(started)
	return result, nil
}

// newFinding is diagnostic as the rule that reported it.
func newFinding(enabled rule.Rule, fileSet *token.FileSet, diagnostic analysis.Diagnostic) Finding {
	start := fileSet.PositionFor(diagnostic.Pos, false)
	found := Finding{
		Rule:      enabled.Name,
		File:      start.Filename,
		Line:      start.Line,
		Column:    start.Column,
		MessageId: cmp.Or(diagnostic.Category, enabled.UpstreamName, enabled.Analyzer.Name),
		Message:   diagnostic.Message,
	}
	if diagnostic.End.IsValid() {
		end := fileSet.PositionFor(diagnostic.End, false)
		found.EndLine, found.EndColumn = end.Line, end.Column
	}
	for _, fix := range diagnostic.SuggestedFixes {
		suggestion := Suggestion{Message: fix.Message}
		for _, textEdit := range fix.TextEdits {
			editStart := fileSet.PositionFor(textEdit.Pos, false)
			edit := Edit{File: editStart.Filename, Start: editStart.Offset, End: editStart.Offset,
				Text: string(textEdit.NewText)}
			// An insertion may leave End unset.
			if textEdit.End.IsValid() {
				edit.End = fileSet.PositionFor(textEdit.End, false).Offset
			}
			suggestion.Edits = append(suggestion.Edits, edit)
		}
		found.Suggestions = append(found.Suggestions, suggestion)
	}
	return found
}

// crashSentence says why action did not finish. When it failed because something it required failed, checker
// names only those prerequisites, so the sentence adds what each of them actually said, which is the cause.
func crashSentence(enabled rule.Rule, action *checker.Action) string {
	var causes []string
	for _, cause := range originatingFailures(action) {
		causes = append(causes, fmt.Sprintf("%s: %v", cause, cause.Err))
	}
	if len(causes) == 0 {
		return fmt.Sprintf("%s: %v", enabled.Name, action.Err)
	}
	slices.Sort(causes)
	return fmt.Sprintf("%s: %v, because %s", enabled.Name, action.Err, strings.Join(causes, "; "))
}

// originatingFailures is every action below action that failed on its own, with everything it required
// finished, rather than because a prerequisite failed.
func originatingFailures(action *checker.Action) []*checker.Action {
	seen := map[*checker.Action]bool{}
	var found []*checker.Action
	var visit func(below *checker.Action)
	visit = func(below *checker.Action) {
		if seen[below] || below.Err == nil {
			return
		}
		seen[below] = true
		prerequisiteFailed := false
		for _, dependency := range below.Deps {
			if dependency.Err != nil {
				prerequisiteFailed = true
				visit(dependency)
			}
		}
		if !prerequisiteFailed {
			found = append(found, below)
		}
	}
	for _, dependency := range action.Deps {
		visit(dependency)
	}
	return found
}

// firstFile is the file a crash on analyzedPackage names: its first Go file as written, else the first file the
// rules were given, else the package's ID, so a crash never names nothing.
func firstFile(analyzedPackage *packages.Package) string {
	if len(analyzedPackage.GoFiles) > 0 {
		return analyzedPackage.GoFiles[0]
	}
	if len(analyzedPackage.CompiledGoFiles) > 0 {
		return analyzedPackage.CompiledGoFiles[0]
	}
	return analyzedPackage.ID
}

// walk counts the distinct files the rules were given, the syntax trees go/packages parsed from each package's
// CompiledGoFiles, and the nodes of one ast.Inspect over them.
func walk(analyzed []*packages.Package) (files int, nodes int) {
	seen := map[string]bool{}
	for _, analyzedPackage := range analyzed {
		for _, file := range analyzedPackage.Syntax {
			name := analyzedPackage.Fset.PositionFor(file.FileStart, false).Filename
			if seen[name] {
				continue
			}
			seen[name] = true
			ast.Inspect(file, func(node ast.Node) bool {
				if node != nil {
					nodes++
				}
				return true
			})
		}
	}
	return len(seen), nodes
}
