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
	"go/token"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Options says how a run schedules its work.
type Options struct {
	// SingleThreaded runs one action at a time, for `--single-threaded`; the findings are the same either way.
	SingleThreaded bool

	// Cache is the per-package findings cache the run reads and writes, or nil for a run that does neither, as
	// under `--no-cache`. The findings are the same either way. The load is given it first (see
	// load.PackagesCached), so the packages it holds were never parsed; a session no load was given opens here,
	// over packages the load checked whole.
	Cache *CacheSession

	// Withheld is the load's Loaded.Withheld, which the cache keeps with each package it saves.
	Withheld map[*packages.Package]load.PackageWithheld
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
// nodesVisited, and len(Findings) is findings before suppression. rulesWatchedAndQuiet counts the rules with 0 in
// FindingsByRule that are not in CrashedRules: only such a rule ran on every package it was given and reported
// nothing. A crashed rule also has 0, from the packages it never finished, and counting it quiet would read a
// rule that saw nothing as a rule that found nothing. rulesSilent, the rules
// that listened to no file at all, is the engine's to name, since only it knows which rules the config kept from
// every file before they reached this run; a run over no files leaves every rule it was given silent.
type Result struct {
	// Findings are every diagnostic of every rule that finished, but a house rule's in a generated or copied file
	// (see WithheldAsGenerated and WithheldAsCopied), each once by rule, position and message, sorted by file, line,
	// column and rule. A rule that crashed on a package contributes no findings for it, since what it reported
	// before it stopped is not what it would have reported.
	Findings []Finding

	// Crashes are each rule that did not finish on a package, once each, sorted by file.
	Crashes []Crash

	RulesRun int

	// FilesWalked is the distinct Go files of the analyzed packages, the files the rules were given.
	FilesWalked int

	// NodesVisited is the nodes of one ast.Inspect over those files.
	NodesVisited int

	// FindingsByRule is each rule's number of findings kept, 0 included, by rule name.
	FindingsByRule map[string]int

	// CrashedRules names, once each and sorted, every rule with a crash on at least one package. Whatever its count
	// in FindingsByRule, such a rule is never quiet.
	CrashedRules []string

	// WithheldAsGenerated is, by rule name, how many findings of a house rule were dropped because their file is
	// generated, only for rules that had any. A house rule is about how we write code, which a generator's output
	// does not answer to, while a vet or tools finding there is still a bug in what we ship, so those stay. The
	// engine writes these counts into the lint record, so a run over generated files never reads as clean when it
	// withheld what it found there.
	WithheldAsGenerated map[string]int

	// WithheldAsCopied is the same count for files that declare themselves copies of upstream source (see package
	// copied). A copy is foreign code we carry until upstream ships the fix, and restyling it to the house would bury
	// its diff against upstream, the one thing about it a reader needs; vet, tools and staticcheck still judge it.
	// A file both generated and copied is counted as generated.
	WithheldAsCopied map[string]int

	// Cache is what the per-package findings cache did, all zero on a run without it.
	Cache CacheCounts

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
// what a rule gets wrong on a package is a Crash. The packages below analyzed come back without their syntax and
// type information, which the run let go once their facts were learned.
//
// With options.Cache, a package the cache holds is restored rather than analyzed: Analyze is given only the packages
// that missed, a restored package's facts reach the importers that missed as a run's would, and a restored package
// to analyze gives its findings and its files' walk from its entry, into the same dedupe and withholding as live
// ones. Every package that missed, with no load error and on which every action finished without one, is written
// once the graph has finished.
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
	checkerOptions := &checker.Options{Sequential: options.SingleThreaded}
	session := options.Cache
	roots := analyzed
	if session != nil {
		if !session.opened {
			var reachable []*packages.Package
			packages.Visit(analyzed, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
			session.Restore(load.LoaderSource, reachable, analyzed)
		}
		checkerOptions.Restored, checkerOptions.Restore = session.restored, session.restore
		roots = slices.DeleteFunc(slices.Clone(analyzed), session.restored)
	}
	graph, analyzeError := checker.Analyze(analyzers, roots, checkerOptions)
	if analyzeError != nil {
		return nil, fmt.Errorf("analyzing %d packages: %w", len(roots), analyzeError)
	}
	type rootKey struct {
		analyzer        *analysis.Analyzer
		analyzedPackage *packages.Package
	}
	actionOf := make(map[rootKey]*checker.Action, len(graph.Roots))
	for _, action := range graph.Roots {
		actionOf[rootKey{action.Analyzer, action.Package}] = action
	}

	result := &Result{
		RulesRun:            len(rules),
		FindingsByRule:      make(map[string]int, len(rules)),
		WithheldAsGenerated: map[string]int{},
		WithheldAsCopied:    map[string]int{},
	}
	for _, enabled := range rules {
		result.FindingsByRule[enabled.Name] = 0
	}
	files := analyzedFiles(analyzed, session)
	generated, copies := withheldFiles(files)
	seenFindings := map[findingKey]bool{}
	seenCrashes := map[Crash]bool{}
	// In the order of graph.Roots, each analyzer over each package, so of two findings alike the one kept is the
	// one a run without the cache keeps.
	for _, analyzer := range analyzers {
		analyzerRule := ruleOf[analyzer]
		for _, analyzedPackage := range analyzed {
			found, restored := session.findingsOf(analyzer, analyzedPackage)
			if !restored {
				action, ran := actionOf[rootKey{analyzer, analyzedPackage}]
				if !ran {
					return nil, fmt.Errorf("reading the analysis of %s: analyzer %s has no action on it",
						analyzedPackage.ID, analyzer.Name)
				}
				if action.Err != nil {
					crash := Crash{File: firstFile(action.Package), Error: crashSentence(analyzerRule, action)}
					if !seenCrashes[crash] {
						seenCrashes[crash] = true
						result.Crashes = append(result.Crashes, crash)
					}
					if !slices.Contains(result.CrashedRules, analyzerRule.Name) {
						result.CrashedRules = append(result.CrashedRules, analyzerRule.Name)
					}
					continue
				}
				found = actionFindings(analyzerRule, action)
			}
			for _, finding := range found {
				key := findingKey{finding.Rule, finding.File, finding.Line, finding.Column, finding.Message}
				if seenFindings[key] {
					continue
				}
				seenFindings[key] = true
				if analyzerRule.Origin == rule.OriginHouse && generated[finding.File] {
					result.WithheldAsGenerated[finding.Rule]++
					continue
				}
				if analyzerRule.Origin == rule.OriginHouse && copies[finding.File] {
					result.WithheldAsCopied[finding.Rule]++
					continue
				}
				result.Findings = append(result.Findings, finding)
				result.FindingsByRule[finding.Rule]++
			}
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
	slices.Sort(result.CrashedRules)
	result.FilesWalked, result.NodesVisited = walk(files)
	if session != nil {
		session.save(graph, options.Withheld, files)
		result.Cache = session.counts
	}
	result.Elapsed = time.Since(started)
	return result, nil
}

// actionFindings is what action found, as newFinding makes it.
func actionFindings(actionRule rule.Rule, action *checker.Action) []Finding {
	found := make([]Finding, 0, len(action.Diagnostics))
	for _, diagnostic := range action.Diagnostics {
		found = append(found, newFinding(actionRule, action.Package.Fset, diagnostic))
	}
	return found
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

// analyzedFiles is each analyzed package's files as the rules were given them, with each one's walk and whether it
// is generated or copied, read from the syntax, or from the entry of a package the cache restored, which has none.
func analyzedFiles(analyzed []*packages.Package, session *CacheSession) map[*packages.Package][]storedFile {
	files := make(map[*packages.Package][]storedFile, len(analyzed))
	for _, analyzedPackage := range analyzed {
		if restored, hit := session.filesOf(analyzedPackage); hit {
			files[analyzedPackage] = restored
			continue
		}
		files[analyzedPackage] = storedFiles(analyzedPackage)
	}
	return files
}

// withheldFiles is every file of the analyzed packages that go/ast.IsGenerated reads as generated, a comment line
// `// Code generated ... DO NOT EDIT.` before the package clause, and every one whose header declares it a copy of
// upstream source (see package copied), judged on the syntax the rules were given rather than on a second read of
// the disk, which could disagree with it.
func withheldFiles(files map[*packages.Package][]storedFile) (generated map[string]bool, copies map[string]bool) {
	generated, copies = map[string]bool{}, map[string]bool{}
	for _, packageFiles := range files {
		for _, file := range packageFiles {
			generated[file.Name] = generated[file.Name] || file.Generated
			copies[file.Name] = copies[file.Name] || file.Copied
		}
	}
	return generated, copies
}

// walk counts the distinct files the rules were given, the syntax trees go/packages parsed from each package's
// CompiledGoFiles, and the nodes of one ast.Inspect over them.
func walk(files map[*packages.Package][]storedFile) (filesWalked int, nodes int) {
	seen := map[string]bool{}
	for _, packageFiles := range files {
		for _, file := range packageFiles {
			if seen[file.Name] {
				continue
			}
			seen[file.Name] = true
			nodes += file.Nodes
		}
	}
	return len(seen), nodes
}
