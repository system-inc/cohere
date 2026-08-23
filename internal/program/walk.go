package program

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/system-inc/verify/internal/config"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/suppression"
)

// Coverage is what a run actually looked at.
//
// This exists because "what did you find" is the less important half of a verdict and the only half
// most tools report. A run that found nothing and a run that checked nothing print the same green,
// and this project's history is a list of times that ambiguity cost days: a gate that linted zero
// files and exited 0, three configs that loaded no plugins, a filter that stopped reporting. Every
// result this package returns carries what it covered, so a caller that prints a verdict without
// also printing the population had to work at it.
type Coverage struct {
	// FilesInProgram is the whole type graph — our code plus every declaration it pulled in.
	FilesInProgram int

	// FilesWalked is how many files the rules actually visited.
	FilesWalked int

	// NodesVisited is how many AST nodes the traversal touched, across every file.
	NodesVisited int

	// RulesRun is how many rules were dispatched.
	RulesRun int

	// RulesListening counts, per rule name, how many files that rule chose to listen to. A rule that
	// declined every file reads as zero here, which is the difference between "ran and found nothing"
	// and "never actually looked" — the distinction a bare finding count erases.
	RulesListening map[string]int

	// Suppressed is how many findings a disable comment withheld.
	//
	// This is the coverage discipline pointed the other way. Coverage stops a run that checked
	// nothing from printing the same green as a run that found nothing; this stops a run that hid
	// forty findings from printing the same green as a run that had none to hide. A suppression is
	// a decision, and a decision that leaves no trace in the output is indistinguishable from the
	// tool being blind.
	Suppressed int

	// SuppressedWithoutReason is how many of those withheld findings were silenced by a directive
	// that never said why.
	//
	// Measured rather than enforced, deliberately. Requiring a reason today would turn 281 working
	// suppressions red for no defect, so the number is printed every run instead — which is what
	// lets the convention be tightened later from evidence rather than from a guess.
	SuppressedWithoutReason int

	// UnusedSuppressions is how many directives never withheld anything.
	//
	// An unused suppression is a rule scoped off a file that no longer needs it, and it is how a
	// codebase accumulates permanent exemptions nobody chose. It only means what it says after a
	// full run with every rule, so a caller running a filtered subset should not report it.
	UnusedSuppressions int

	// FilesIgnored is how many files an ignorePattern excluded from linting entirely.
	//
	// A file skipped by configuration and a file with no findings produce identical output
	// otherwise, which is the same ambiguity the coverage line exists to destroy. Exclusion is a
	// decision someone made, and a decision that leaves no trace is indistinguishable from the tool
	// never having looked.
	FilesIgnored int

	// RulesScopedOff counts, per rule name, how many files the config turned that rule off for.
	//
	// This is the narrower half of the same accounting. A rule can be enabled in the config, run on
	// the tree, and still be silent across a whole directory because an override scoped it off
	// there. Without this number that looks identical to a rule with nothing to report.
	RulesScopedOff map[string]int

	// RulesUnconfigured counts, per rule name, how many files a rule skipped because the config
	// never mentions it.
	//
	// Deliberately separate from RulesScopedOff. "Someone turned this off" and "nobody has said
	// whether this should run" are different facts, and a registered rule missing from the config
	// is a decision waiting to be made rather than one already made. Reporting them as one number
	// would describe a new rule as though someone had excluded it.
	RulesUnconfigured map[string]int
}

// Result is the findings of one walk, and the coverage that produced them.
type Result struct {
	Diagnostics []rule.Diagnostic
	Coverage    Coverage

	// Timings is per-rule cost, populated only when the caller asked for it by setting
	// Graph.CollectTimings. Nil otherwise, so an ordinary run pays nothing for the instrument.
	Timings *Timings
}

// Walk visits every file in the given set once, dispatching every rule's listeners as it goes.
//
// One traversal serves every rule. This is the arrangement the whole tool exists to make possible:
// a rule that wanted its own pass would multiply the only unavoidable cost in the system by the
// number of rules, which is exactly how 53 rules of ordinary shape came to cost 1.81s. Here the
// five-hundredth rule costs a map lookup per node it asked about, and nothing at all for the nodes
// it did not.
//
// Files are walked in parallel, one goroutine per checker, because a file must be visited by the
// checker that owns it and there is no use in more workers than there are checkers.
func (g *Graph) Walk(ctx context.Context, files []*ast.SourceFile, rules []rule.Rule) (Result, error) {
	if len(files) == 0 {
		return Result{}, fmt.Errorf("nothing to walk: the file set is empty (the program holds %d files)", len(g.Program.GetSourceFiles()))
	}
	if len(rules) == 0 {
		return Result{}, fmt.Errorf("nothing to run: no rules were given for %d files", len(files))
	}

	workers := min(g.Workers(), len(files))

	var mutex sync.Mutex
	diagnostics := []rule.Diagnostic{}
	listeningCounts := make(map[string]int, len(rules))
	nodesVisited := 0
	suppressed := suppressionTally{}
	filesIgnored := 0
	scopedOff := map[string]int{}
	unconfigured := map[string]int{}
	configFailures := []error{}

	// Nil unless asked for, and every timing call below is guarded on it, so a run without --timing
	// does not pay for the instrument at all.
	var timings *Timings
	if g.CollectTimings {
		ruleNames := make([]string, 0, len(rules))
		for _, subject := range rules {
			ruleNames = append(ruleNames, subject.Name)
		}
		timings = NewTimings(ruleNames)
	}

	// Files are handed out by index stride rather than by a queue: the compiler assigns a file to a
	// checker by its position in the program's file list, so striding keeps each worker mostly on one
	// checker instead of contending across all of them.
	var waitGroup sync.WaitGroup
	for worker := range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			localDiagnostics := []rule.Diagnostic{}
			localListening := make(map[string]int, len(rules))
			localNodes := 0
			localSuppressed := suppressionTally{}
			localIgnored := 0
			localScopedOff := map[string]int{}
			localUnconfigured := map[string]int{}
			localFailures := []error{}

			// Each worker accumulates locally and merges once under the mutex. Timing through a
			// shared lock would measure contention rather than rule cost.
			var localTimings *Timings
			if timings != nil {
				localTimings = NewTimings(nil)
			}

			for index := worker; index < len(files); index += workers {
				sourceFile := files[index]

				// Configuration is consulted before a checker is acquired, because an ignored file
				// should cost nothing at all rather than cost a checker and then be discarded.
				applicable, ruleOptions, resolution, err := g.rulesFor(sourceFile, rules, localScopedOff, localUnconfigured)
				if err != nil {
					// A rule that requires an option and did not get one is a hard failure, never a
					// quiet decline. Declining is indistinguishable from finding nothing, and that
					// ambiguity kept a dead rule alive for months.
					localFailures = append(localFailures, err)
					continue
				}
				if resolution.Ignored {
					localIgnored++
					continue
				}
				if len(applicable) == 0 {
					continue
				}

				fileChecker, release := g.CheckerForFile(ctx, sourceFile)

				// A rule's Report closure captures the rule it belongs to, so a rule cannot report under
				// another rule's name even by accident.
				visited, silenced := dispatchFile(sourceFile, func(diagnostic rule.Diagnostic) {
					localDiagnostics = append(localDiagnostics, diagnostic)
				}, applicable, g, fileChecker, localListening, ruleOptions, localTimings)

				release()

				localNodes += visited
				localSuppressed.add(silenced)
			}

			mutex.Lock()
			diagnostics = append(diagnostics, localDiagnostics...)
			for name, count := range localListening {
				listeningCounts[name] += count
			}
			nodesVisited += localNodes
			suppressed.add(localSuppressed)
			filesIgnored += localIgnored
			for name, count := range localScopedOff {
				scopedOff[name] += count
			}
			for name, count := range localUnconfigured {
				unconfigured[name] += count
			}
			configFailures = append(configFailures, localFailures...)
			timings.merge(localTimings)
			mutex.Unlock()
		}()
	}
	waitGroup.Wait()

	if len(configFailures) > 0 {
		// One error is enough to stop the run: they are all the same misconfiguration seen once per
		// file, so reporting the first names the problem without printing it 3,408 times.
		return Result{}, fmt.Errorf("rule configuration: %w", configFailures[0])
	}

	return Result{
		Diagnostics: diagnostics,
		Timings:     timings,
		Coverage: Coverage{
			FilesInProgram: len(g.Program.GetSourceFiles()),
			FilesWalked:    len(files),
			NodesVisited:   nodesVisited,
			RulesRun:       len(rules),
			RulesListening: listeningCounts,

			Suppressed:              suppressed.applied,
			SuppressedWithoutReason: suppressed.appliedNoReason,
			UnusedSuppressions:      suppressed.unusedDirectives,

			FilesIgnored:      filesIgnored,
			RulesScopedOff:    scopedOff,
			RulesUnconfigured: unconfigured,
		},
	}, nil
}

// dispatchFile asks every rule what it wants to hear about in this file, merges those answers into
// one dispatch table, and walks the tree once against it. It returns how many nodes it visited.
//
// Merging before walking is what makes the cost per node independent of the rule count: the walk
// does one map lookup per node regardless of whether one rule or five hundred registered for that
// kind.
func dispatchFile(
	sourceFile *ast.SourceFile,
	report func(rule.Diagnostic),
	rules []rule.Rule,
	graph *Graph,
	fileChecker *checker.Checker,
	listeningCounts map[string]int,
	ruleOptions map[string]any,
	timings *Timings,
) (visitedNodes int, silenced suppressionTally) {
	// A kind may have listeners from several rules, so the merged table maps a kind to a slice rather
	// than to one function.
	merged := map[ast.Kind][]func(node *ast.Node){}

	// Directives are read once per file, before any rule runs, because every rule's findings filter
	// through the same index. Scanning is proportional to the file rather than to the rule count, so
	// a file with no directives costs one pass and then answers every query with an empty slice.
	directives := suppression.Build(sourceFile.Text())

	// One cache per file, shared by every rule that runs on it. Work a rule derives from the file
	// outside the walk is paid for by that rule alone, so three rules deriving the same thing pay
	// three times. Measured: three comment rules at 1,777ms combined, each visiting one node per
	// file, because each rescanned the same trivia. The walk is shared; this makes the derivations
	// shared too.
	fileCache := rule.NewFileCache()

	for _, subject := range rules {
		ruleName := subject.Name

		var timing *RuleTiming
		if timings != nil {
			timing = timings.forRule(ruleName)
		}

		context := rule.Context{
			SourceFile:  sourceFile,
			Program:     graph.Program,
			TypeChecker: fileChecker,
			FileCache:   fileCache,
			Report: func(diagnostic rule.Diagnostic) {
				diagnostic.RuleName = ruleName
				if diagnostic.SourceFile == nil {
					diagnostic.SourceFile = sourceFile
				}

				// Filtering here rather than after the walk is what keeps a suppressed finding from
				// ever existing as a finding. The alternative — collect everything, drop some later —
				// leaves a window where a caller can read the unfiltered slice and report a number the
				// user will never see explained.
				if timing != nil {
					timing.Findings++
				}

				if directives.Suppresses(diagnostic.RuleName, diagnostic.Range.Pos()) {
					return
				}

				report(diagnostic)
			},
		}

		// Options are decoded to the type the rule declares rather than handed through as JSON. A
		// rule that receives the wrong shape fails its type assertion and declines every file, which
		// looks exactly like a rule with nothing to report.
		setupStart := timingNow(timing)
		listeners := subject.Run(context, ruleOptions[ruleName])
		if timing != nil {
			timing.SetupDuration += time.Since(setupStart)
		}

		if len(listeners) == 0 {
			if timing != nil {
				// A rule that declined. Counted so that a rule doing expensive setup and then
				// declining every file is visible as exactly that, rather than as a cheap rule.
				timing.FilesDeclined++
			}
			// The rule looked at the file and declined it. That is the cheapest and most valuable thing
			// a rule can do, and it is counted rather than ignored so a rule that declines *everything*
			// is visible as a rule that never ran.
			continue
		}

		listeningCounts[ruleName]++
		if timing != nil {
			timing.FilesListened++
		}
		for kind, listener := range listeners {
			merged[kind] = append(merged[kind], measuringListener(timing, listener))
		}
	}

	if len(merged) > 0 {
		visitedNodes = walk(sourceFile.AsNode(), merged)
	}

	return visitedNodes, tally(directives)
}

// suppressionTally is what one file's directives did, summed across the run.
type suppressionTally struct {
	applied          int
	appliedNoReason  int
	unusedDirectives int
}

func (t *suppressionTally) add(other suppressionTally) {
	t.applied += other.applied
	t.appliedNoReason += other.appliedNoReason
	t.unusedDirectives += other.unusedDirectives
}

// tally reads what a file's directives actually did, after the walk.
//
// The reasonless count is per withheld finding rather than per directive, because that is the
// number that answers the question being asked: how much of what verify chose not to tell you was
// silenced by someone who did not say why.
func tally(directives *suppression.Index) suppressionTally {
	counted := suppressionTally{}
	for index, directive := range directives.Directives() {
		applied := directives.AppliedCount(index)
		if applied == 0 {
			counted.unusedDirectives++
			continue
		}
		counted.applied += applied
		if !directive.HasReason() {
			counted.appliedNoReason += applied
		}
	}
	return counted
}

// walk visits every node once, calling whatever listeners registered for its kind.
func walk(node *ast.Node, listeners map[ast.Kind][]func(node *ast.Node)) int {
	if node == nil {
		return 0
	}

	visited := 1
	for _, listener := range listeners[node.Kind] {
		listener(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		visited += walk(child, listeners)
		return false
	})

	return visited
}

// rulesFor narrows the rule set to those the configuration enables for one file, and decodes each
// one's options into the type that rule declares.
//
// The error return is the point. A rule that requires an option and does not receive one is a hard
// failure here rather than a rule that quietly declines every file. Declining is correct behavior
// for a misconfigured guard and completely indistinguishable from a rule with nothing to report,
// which is how boundary-no-project-import stayed enabled and inert for months under the previous
// gate.
//
// When no configuration is loaded every rule applies with nil options, which keeps existing callers
// and tests working unchanged. The command always loads a real config and fails loudly if it
// cannot, so that permissive path is reachable only from a caller that chose it.
func (g *Graph) rulesFor(
	sourceFile *ast.SourceFile,
	rules []rule.Rule,
	scopedOff map[string]int,
	unconfigured map[string]int,
) ([]rule.Rule, map[string]any, config.Resolved, error) {
	if g.LintConfig == nil {
		return rules, nil, config.Resolved{}, nil
	}

	resolution := g.LintConfig.Resolve(sourceFile.FileName())
	if resolution.Ignored {
		return nil, nil, resolution, nil
	}

	applicable := make([]rule.Rule, 0, len(rules))
	options := map[string]any{}
	for _, subject := range rules {
		switch status, _ := resolution.StatusOf(subject.Name); status {
		case config.StatusScopedOff:
			// Someone configured this rule off, here or tree-wide. Counted rather than dropped
			// silently, because a rule absent across a directory is otherwise indistinguishable
			// from a rule with nothing to report.
			scopedOff[subject.Name]++
			continue
		case config.StatusUnconfigured:
			// Nobody has said whether this rule should run. That is a different fact from a
			// deliberate exclusion and it is counted separately, or a rule waiting on a decision
			// reads as one somebody already made.
			unconfigured[subject.Name]++
			continue
		}

		decoded, err := g.RuleOptions.Decode(subject.Name, resolution.RawOptionsFor(subject.Name))
		if err != nil {
			return nil, nil, resolution, err
		}
		if decoded != nil {
			options[subject.Name] = decoded
		}
		applicable = append(applicable, subject)
	}
	return applicable, options, resolution, nil
}
