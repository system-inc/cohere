package program

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/configuration"
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

	// RulesOffered counts, per rule name, how many files the rule was actually handed. A rule that
	// was never wired is offered zero; a rule that was offered files and declined all of them is
	// offered many and listens to none. Those are opposite defects — nobody configured it, against
	// it is configured and satisfied — and before this existed the coverage note described both with
	// one sentence, so a satisfied rule read exactly like a dead one.
	RulesOffered map[string]int

	// RulesListening counts, per rule name, how many files that rule chose to listen to. A rule that
	// declined every file reads as zero here, which is the difference between "ran and found nothing"
	// and "never actually looked" — the distinction a bare finding count erases.
	RulesListening map[string]int

	// RulesReporting counts, per rule name, how many findings that rule produced.
	//
	// Listening and reporting are different questions and only together do they say what a zero
	// means. A rule that listened to no files never looked. A rule that listened to thousands and
	// reported nothing looked at real code and had nothing to say, which is either a clean tree or a
	// rule that cannot see. Two false positives shipped past a full fixture pair and were caught
	// only by running against the tree, so the distinction is worth a counter rather than a habit.
	RulesReporting map[string]int

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

	// UnusedSuppressionsForUnrunRules is the subset of UnusedSuppressions naming only rules this run
	// did not run.
	//
	// During the migration this is most of the number, and the two mean opposite things. A directive
	// that silenced nothing while its rule ran is dead scaffolding worth deleting. One naming a rule
	// verify has not ported yet silenced nothing because nothing looked, and deleting it would strip
	// a suppression the gate still needs. Collapsing them tells a reader to delete comments that are
	// load-bearing today.
	UnusedSuppressionsForUnrunRules int

	// UnusedSuppressions is how many directives never withheld anything.
	//
	// An unused suppression is a rule scoped off a file that no longer needs it, and it is how a
	// codebase accumulates permanent exemptions nobody chose. It only means what it says after a
	// full run with every rule, so a caller running a filtered subset should not report it.
	UnusedSuppressions int

	// FilesCrashed names the files a rule panicked on, with the panic that ended each.
	//
	// Named rather than counted, because the panic message is the defect and a count of crashes is
	// not actionable. A file the linter could not process is not a file with nothing to report, and
	// keeping those apart is the whole job of this struct.
	FilesCrashed []FileCrash

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
	reportingCounts := make(map[string]int, len(rules))
	offeredCounts := make(map[string]int, len(rules))
	nodesVisited := 0
	suppressed := suppressionTally{}
	filesIgnored := 0
	scopedOff := map[string]int{}
	unconfigured := map[string]int{}
	configFailures := []error{}
	fileCrashes := []FileCrash{}

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
			localReporting := make(map[string]int, len(rules))
			localOffered := make(map[string]int, len(rules))
			localNodes := 0
			localSuppressed := suppressionTally{}
			localIgnored := 0
			localScopedOff := map[string]int{}
			localUnconfigured := map[string]int{}
			localFailures := []error{}
			localCrashes := []FileCrash{}

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

				// The checker is acquired only when an applicable rule declares it reads one.
				//
				// CheckerForFile hands out an exclusive lock held until release, and it is held across
				// the whole dispatch below rather than around a single query, so acquiring it serializes
				// this file's entire walk against every other file's. Measured: about 50% of the lint
				// phase, 366-417ms against 557-575ms on the same tree at controlled load.
				//
				// This is paid now. Measured against the live registry on 2026-08-25: 212 rules
				// registered, 44 of them declaring NeedsTypeChecker, so the guard acquires on any file
				// one of those 44 applies to rather than skipping every file. The comment here
				// previously read "93 rules run and none reads the checker", which was true when the
				// tsgolint adapter had not registered and became false the moment it did.
				//
				// Left as a measurement with its date rather than a standing claim: a count of rules
				// is exactly the kind of number that decays silently, and the previous version of this
				// sentence is the proof.
				//
				// Declared rather than lazy on purpose. A getter that acquired on first use would work
				// until two rules on one file both asked, and the second acquisition would happen inside
				// the first's window — a deadlock whose shape is invisible at both call sites. The
				// declaration is legible at both ends and cannot deadlock by being used twice.
				var fileChecker *checker.Checker
				release := func() {}
				if anyRuleNeedsTypeChecker(applicable) {
					fileChecker, release = g.CheckerForFile(ctx, sourceFile)
				}

				// A rule's Report closure captures the rule it belongs to, so a rule cannot report under
				// another rule's name even by accident.
				visited, silenced, crashed := dispatchFileSafely(sourceFile, func(diagnostic rule.Diagnostic) {
					localDiagnostics = append(localDiagnostics, diagnostic)
					localReporting[diagnostic.RuleName]++
				}, applicable, g, fileChecker, localListening, localOffered, ruleOptions, localTimings)

				release()

				if crashed != nil {
					// One file is lost rather than the run. Without this, a panic in any rule on any
					// node kills the process: files are walked in goroutines, a panic in a goroutine
					// cannot be recovered by its parent, and nothing else in this codebase recovers.
					// Measured before this existed: one panic on the branch producing the tree's 128
					// findings ended the run at exit 2 with no lint line and no phases line, losing a
					// completed types phase along with it.
					//
					// Named rather than counted, and never swallowed. A file the linter could not
					// process is not a file with nothing to report, and the whole coverage line exists
					// to keep those two apart.
					localCrashes = append(localCrashes, FileCrash{FileName: sourceFile.FileName(), Cause: crashed})
					continue
				}

				localNodes += visited
				localSuppressed.add(silenced)
			}

			// Findings are appended in whichever order the workers finish, so `diagnostics` carries
			// scheduling order rather than any property of the tree. The SET is stable — every finding
			// is appended exactly once — and only the order moves. Measured on the shipped binary:
			// three runs over ~/Projects/ahra gave three distinct raw hashes and one identical hash
			// after sorting.
			//
			// Deliberately not sorted. Nobody depends on the order today and sorting costs something
			// on a path that runs over 3,481 files, so this stays incidental rather than becoming a
			// guarantee.
			//
			// Written down because the cost falls on the next reader, not on this code: anything
			// comparing two runs MUST sort first, and a walk-order change will produce a diff that
			// looks like a regression it did not cause. That already happened once, and it took three
			// runs of the previous binary to establish the reordering was pre-existing.
			//
			// If anything ever depends on the order — a cache keyed on it, a golden file, a
			// differential that reads position — this decision flips, because at that point the
			// output stops being incidentally unsorted and becomes a silent dependency on goroutine
			// scheduling.
			mutex.Lock()
			diagnostics = append(diagnostics, localDiagnostics...)
			for name, count := range localOffered {
				offeredCounts[name] += count
			}
			for name, count := range localListening {
				listeningCounts[name] += count
			}
			for name, count := range localReporting {
				reportingCounts[name] += count
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
			fileCrashes = append(fileCrashes, localCrashes...)
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
			RulesOffered:   offeredCounts,
			RulesListening: listeningCounts,
			RulesReporting: reportingCounts,

			Suppressed:                      suppressed.applied,
			SuppressedWithoutReason:         suppressed.appliedNoReason,
			UnusedSuppressions:              suppressed.unusedDirectives,
			UnusedSuppressionsForUnrunRules: suppressed.unusedForUnrunRule,

			FilesCrashed:      fileCrashes,
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
// FileCrash is a file a rule panicked on, and the panic it raised.
//
// The cause is carried rather than summarized because the panic message is the only evidence of what
// went wrong: `Unhandled case in Node.Text: *ast.Token` names the defect precisely, and a count of
// crashes names nothing.
type FileCrash struct {
	FileName string
	Cause    error
}

// dispatchFileSafely is dispatchFile with a boundary around it.
//
// A rule is ordinary Go code walking a tree it did not build, and the compiler's own accessors panic
// rather than error on shapes they do not handle. `Node.Text()` panics on any kind outside its
// switch, and 220 call sites across the rules reach it. Kind-checking every one is the real fix and
// this is not a substitute for it: this is what keeps the other 3,406 files reportable while that
// work happens.
//
// The recovery is here rather than deeper because the file is the unit the coverage line already
// speaks in. Recovering per node would leave a half-walked file reported as fully walked, which is
// worse than losing it: a partial result that claims to be whole is the failure this package exists
// to prevent.
// anyRuleNeedsTypeChecker reports whether any of these rules declared that it reads the checker.
//
// Asked per file against the applicable set rather than once against the whole catalog, because
// applicability is per file: a type-aware rule that declines this file should not make it pay for a
// checker nobody will read.
func anyRuleNeedsTypeChecker(rules []rule.Rule) bool {
	for _, subject := range rules {
		if subject.NeedsTypeChecker {
			return true
		}
	}
	return false
}

func dispatchFileSafely(
	sourceFile *ast.SourceFile,
	report func(rule.Diagnostic),
	applicable []rule.Rule,
	g *Graph,
	fileChecker *checker.Checker,
	listeningCounts map[string]int,
	offeredCounts map[string]int,
	ruleOptions map[string]any,
	timings *Timings,
) (visited int, silenced suppressionTally, crashed error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// The visited count is discarded along with the file. A file that crashed halfway
			// contributed nodes to a total that would then describe a walk nobody completed.
			visited = 0
			silenced = suppressionTally{}
			crashed = fmt.Errorf("%v", recovered)
		}
	}()

	visited, silenced = dispatchFile(sourceFile, report, applicable, g, fileChecker,
		listeningCounts, offeredCounts, ruleOptions, timings)
	return visited, silenced, nil
}

func dispatchFile(
	sourceFile *ast.SourceFile,
	report func(rule.Diagnostic),
	rules []rule.Rule,
	graph *Graph,
	fileChecker *checker.Checker,
	listeningCounts map[string]int,
	offeredCounts map[string]int,
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

	// Which rule triggered each cache fill, so its cost can be moved off that rule's total.
	fillPayer := map[string]string{}
	seenFills := map[string]bool{}

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
		// Counted before Run rather than after, because the question this answers is whether anything
		// ever handed this rule a file. A rule that panics or declines has still been offered one; a
		// rule nobody wired never reaches this line at all.
		offeredCounts[ruleName]++

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
			merged[kind] = append(merged[kind], attributingListener(timing, listener, ruleName, fileCache, seenFills, fillPayer))
		}
	}

	if len(merged) > 0 {
		visitedNodes = walk(sourceFile.AsNode(), merged)
	}

	// A cached derivation is paid for by whichever rule asked first, and files are walked in
	// parallel, so that identity is arbitrary. Move the cost off that rule and onto the derivation,
	// or the table names a victim rather than a cause.
	if timings != nil {
		for key, cost := range fileCache.FillDurations() {
			timings.RecordSharedFill(fillPayer[key], key, cost)
		}
	}

	// The rules this run actually ran, so a directive naming only rules verify has not ported can be
	// told apart from one whose rule ran and found nothing to silence.
	//
	// Built from `rules` rather than from listeningCounts: a rule that declined every file in this
	// one still ran, and counting it as absent would call its directives unportable when they are
	// simply satisfied.
	ranRule := make(map[string]bool, len(rules))
	for _, subject := range rules {
		ranRule[subject.Name] = true
	}

	return visitedNodes, tally(directives, ranRule)
}

// suppressionTally is what one file's directives did, summed across the run.
type suppressionTally struct {
	applied          int
	appliedNoReason  int
	unusedDirectives int

	// unusedForUnrunRule is the subset of unusedDirectives naming only rules this run did not run.
	//
	// Separated because the two mean opposite things to a reader. A directive that silenced nothing
	// while its rule ran is dead scaffolding worth deleting. A directive naming a rule verify has
	// not ported yet silenced nothing because nothing looked, and deleting it would remove a
	// suppression the gate still needs. Reporting them as one number tells a reader to go delete
	// comments that are load-bearing today.
	unusedForUnrunRule int
}

func (t *suppressionTally) add(other suppressionTally) {
	t.applied += other.applied
	t.appliedNoReason += other.appliedNoReason
	t.unusedDirectives += other.unusedDirectives
	t.unusedForUnrunRule += other.unusedForUnrunRule
}

// namesOnlyUnrunRules reports whether every rule a directive named is one this run did not run.
//
// A blanket directive names no rules and silences everything in its scope, so it is never in this
// category: something ran, and it still withheld nothing. A directive naming a mix is not either,
// because at least one rule looked and declined to fire, which is the shape worth deleting.
func namesOnlyUnrunRules(directive *suppression.Directive, ranRule map[string]bool) bool {
	if len(directive.Rules) == 0 {
		return false
	}
	for _, named := range directive.Rules {
		if ranRule[bareRuleName(named)] {
			return false
		}
	}
	return true
}

// bareRuleName drops a plugin prefix, so `structure/no-x` and `no-x` compare equal.
//
// Directives are written against the gate's names, which carry the plugin that owns the rule, while
// the registry holds the rule's own name. Comparing them raw would report every prefixed directive
// as naming an unrun rule.
func bareRuleName(name string) string {
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		return name[slash+1:]
	}
	return name
}

// tally reads what a file's directives actually did, after the walk.
//
// The reasonless count is per withheld finding rather than per directive, because that is the
// number that answers the question being asked: how much of what verify chose not to tell you was
// silenced by someone who did not say why.
func tally(directives *suppression.Index, ranRule map[string]bool) suppressionTally {
	counted := suppressionTally{}
	for index, directive := range directives.Directives() {
		applied := directives.AppliedCount(index)
		if applied == 0 {
			counted.unusedDirectives++
			if namesOnlyUnrunRules(directive, ranRule) {
				counted.unusedForUnrunRule++
			}
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
) ([]rule.Rule, map[string]any, configuration.Resolved, error) {
	if g.LintConfig == nil {
		return rules, nil, configuration.Resolved{}, nil
	}

	resolution := g.LintConfig.Resolve(sourceFile.FileName())
	if resolution.Ignored {
		return nil, nil, resolution, nil
	}

	applicable := make([]rule.Rule, 0, len(rules))
	options := map[string]any{}
	for _, subject := range rules {
		switch status, _ := resolution.StatusOf(subject.Name); status {
		case configuration.StatusScopedOff:
			// Someone configured this rule off, here or tree-wide. Counted rather than dropped
			// silently, because a rule absent across a directory is otherwise indistinguishable
			// from a rule with nothing to report.
			scopedOff[subject.Name]++
			continue
		case configuration.StatusUnconfigured:
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
