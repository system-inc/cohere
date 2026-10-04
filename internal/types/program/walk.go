package program

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/suppression"
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
	// cohere has not ported yet silenced nothing because nothing looked, and deleting it would strip
	// a suppression the gate still needs. Collapsing them tells a reader to delete comments that are
	// load-bearing today.
	UnusedSuppressionsForUnrunRules int

	// UnusedSuppressions is how many directives never withheld anything.
	//
	// An unused suppression is a rule scoped off a file that no longer needs it, and it is how a
	// codebase accumulates permanent exemptions nobody chose. It only means what it says after a
	// full run with every rule, so a caller running a filtered subset should not report it.
	UnusedSuppressions int

	// DeadSuppressions names each directive counted in UnusedSuppressions and not in
	// UnusedSuppressionsForUnrunRules: one that silenced nothing while a rule it names ran. A count
	// alone sends a reader linting files one at a time to find them, and a single file is the wrong
	// tool, because without the whole program the type-aware rules go quiet and every directive for
	// one of them reads as dead.
	DeadSuppressions []DeadSuppression

	// FilesCrashed names the files a rule panicked on, with the panic that ended each.
	//
	// Named rather than counted, because the panic message is the defect and a count of crashes is
	// not actionable. A file the linter could not process is not a file with nothing to report, and
	// keeping those apart is the whole job of this struct.
	FilesCrashed []FileCrash

	// RulesCrashed names each rule that panicked on a file, with the file and the panic.
	//
	// A rule's panic is contained to that rule: it stops hearing about the file, and every other rule
	// on the file runs to the end. Before this, one rule's panic cost every rule its verdict on the
	// file. prefer-arrow-callback's shared arm crashed 71 files that way, and all 446 rules lost those
	// files under an ordinary-looking summary (#qa9nttp). The crashed rule's verdict on the file is
	// still missing, which is why it is named rather than counted.
	RulesCrashed []RuleCrash

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

	// FilesReplayed is how many files this walk served from the findings cache rather than walking
	// with every rule. Zero without a cache. The lint line reports it, so a reader can tell a walked
	// verdict from a remembered one.
	FilesReplayed int

	// TypeAwareRerun and ShapeKeyedRerun are how many of the replayed files ran their content-keyed and
	// their shape-keyed type-aware rules again, because something they import changed. The difference
	// between the two is what shapes saved.
	TypeAwareRerun  int
	ShapeKeyedRerun int

	// DesignSystemRerun is how many of the replayed files ran their design-system rules again, because a
	// stylesheet the design system read changed or the last run's could not be keyed.
	DesignSystemRerun int

	// FilesOnForeignCheckers is how many files were walked on a checker other than the one that owns them:
	// taken by a worker whose own group was done, or every file under WalkOnForeignCheckers. See walkQueue.
	FilesOnForeignCheckers int

	// Notes is what each file's rules noted through rule.Context.Note, by file name. Kept per file and
	// per rule rather than summed, because the findings cache stores a file's notes beside its findings
	// and a refresh re-walks only some of a file's rules: a total could not be split back apart.
	Notes map[string]RuleNotes
}

// RuleNotes is one file's notes: a count per key, per rule.
type RuleNotes map[string]map[string]int

// Walk visits every file in the given set once, dispatching every rule's listeners as it goes.
//
// One traversal serves every rule. This is the arrangement the whole tool exists to make possible:
// a rule that wanted its own pass would multiply the only unavoidable cost in the system by the
// number of rules, which is exactly how 53 rules of ordinary shape came to cost 1.81s. Here the
// five-hundredth rule costs a function call per node it asked about, and nothing at all for the nodes
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

	// Built once per walk, read by every worker. Nil when no registry was supplied.
	catalog := newRuleNameCatalog(g.RegisteredRuleNames, g.LintConfig.RuleKeys())

	var mutex sync.Mutex
	diagnostics := []rule.Diagnostic{}
	filesReplayed, typeAwareRerun, shapeKeyedRerun, designSystemRerun := 0, 0, 0, 0
	filesOnForeignCheckers := 0

	// Computed once per walk, before any worker runs, and only when the findings cache is in use: about
	// 47ms on ahra, the one cost type-aware caching adds over the walk. Shape fingerprints need this run's
	// shapes, which the caller computes; without them the shape-keyed rules are keyed on the type
	// fingerprint instead, which can only re-run them more often.
	var fingerprints, shapeFingerprints map[tspath.Path][sha256.Size]byte
	if g.FindingsReuse != nil && !g.CollectTimings {
		fingerprints = g.TypeFingerprints()
		shapeFingerprints = fingerprints
		if g.Shapes != nil {
			shapeFingerprints = g.SignatureFingerprints(g.Shapes)
		}
	}
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
	ruleCrashesAll := []RuleCrash{}
	notes := map[string]RuleNotes{}

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

	// Each worker walks the files of one checker, and then helps finish the others. See assignFilesToWorkers
	// and walkQueue.
	needsCheckers := anyRuleNeedsTypeChecker(rules)
	assignments := g.assignFilesToWorkers(ctx, files, workers, needsCheckers)
	queues := make([]*walkQueue, workers)
	homeFiles := make([]*ast.SourceFile, workers)
	for worker, indices := range assignments {
		queues[worker] = &walkQueue{indices: indices, back: len(indices)}
		if len(indices) > 0 {
			homeFiles[worker] = files[indices[0]]
		}
	}
	var waitGroup sync.WaitGroup
	for worker := range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			localDiagnostics := []rule.Diagnostic{}
			localReplayed, localTypeAwareRerun, localShapeKeyedRerun, localDesignSystemRerun := 0, 0, 0, 0
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
			localRuleCrashes := []RuleCrash{}
			localNotes := map[string]RuleNotes{}
			localForeign := 0

			// Each worker accumulates locally and merges once under the mutex. Timing through a
			// shared lock would measure contention rather than rule cost.
			var localTimings *Timings
			if timings != nil {
				localTimings = NewTimings(nil)
			}

			// walkFile walks one file on the checker that owns checkerFile.
			walkFile := func(index int, checkerFile *ast.SourceFile) {
				if checkerFile != files[index] {
					localForeign++
				}
				sourceFile := files[index]

				// Configuration is consulted before a checker is acquired, because an ignored file
				// should cost nothing at all rather than cost a checker and then be discarded.
				applicable, ruleOptions, resolution, err := g.rulesFor(sourceFile, rules, localScopedOff, localUnconfigured)
				if err != nil {
					// A rule that requires an option and did not get one is a hard failure, never a
					// quiet decline. Declining is indistinguishable from finding nothing, and that
					// ambiguity kept a dead rule alive for months.
					localFailures = append(localFailures, err)
					return
				}
				if resolution.Ignored {
					localIgnored++
					return
				}
				if len(applicable) == 0 {
					return
				}

				// The findings cache. A file whose bytes, key and applied cacheable rules all match the
				// last run's entry is walked with only its uncacheable rules, and the cacheable rules'
				// findings and coverage are replayed. Every other file is walked in full and, if it is
				// eligible, recorded. Off under --timing, since per-rule cost comes from the walk and a
				// replayed rule would time as free. See FindingsReuse.
				reuse := g.FindingsReuse
				if g.CollectTimings {
					reuse = nil
				}
				//
				// Type-aware rules replay only while their fingerprint is unchanged too, so an importer of an
				// edited file replays its pure rules and runs its type-aware ones again. Shape-keyed rules are
				// keyed on the shape fingerprint, which an edit inside an imported body does not move.
				//
				// Design-system rules replay while every path the design system read last time still holds and the
				// entry was produced under that design system. See DesignSystemKey.
				walkRules := applicable
				var replayed *LintCacheEntry
				var hits classHits
				var keys cacheKeys
				if reuse != nil {
					pureHere, typeAwareHere, designHere, _ := CacheClasses(applicable)
					shapedHere, typedHere := ShapeClasses(typeAwareHere)
					keys = cacheKeys{
						contentHash:      HashContent(sourceFile.Text()),
						pure:             ruleNames(pureHere),
						typed:            ruleNames(typedHere),
						typeFingerprint:  fingerprints[sourceFile.Path()],
						shaped:           ruleNames(shapedHere),
						shapeFingerprint: shapeFingerprints[sourceFile.Path()],
						design:           ruleNames(designHere),
					}
					if entry, found := reuse.lookup(sourceFile.FileName(), keys); found.pure {
						replayed = &entry
						hits = found
						replays := make(map[string]bool, len(applicable))
						for _, names := range [][]string{keys.pure, classIf(hits.typed, keys.typed), classIf(hits.shaped, keys.shaped),
							classIf(hits.design, keys.design)} {
							for _, name := range names {
								replays[name] = true
							}
						}
						// The rules still to walk, in the order they were configured: the order rules run in
						// can decide which findings exist (HashRuleSet).
						walkRules = nil
						for _, subject := range applicable {
							if !replays[subject.Name] {
								walkRules = append(walkRules, subject)
							}
						}
						localReplayed++
						if !hits.typed && len(keys.typed) > 0 {
							localTypeAwareRerun++
						}
						if !hits.shaped && len(keys.shaped) > 0 {
							localShapeKeyedRerun++
						}
						if !hits.design && len(keys.design) > 0 {
							localDesignSystemRerun++
						}
						replayEntry(entry, hits, sourceFile, &localDiagnostics, localReporting, localOffered, localListening)
						if len(walkRules) == 0 {
							localNodes += entry.VisitedNodes
							return
						}
					}
				}

				// The checker is acquired only when an applicable rule declares it reads one. It is the checker that
				// owns checkerFile: the file itself, or for a file taken from another worker's group, a file of this
				// worker's own, so a stolen file is walked on this worker's checker. See walkQueue.
				//
				// CheckerForFile hands out an exclusive lock held until release, and it is held across
				// the whole dispatch below rather than around a single query.
				//
				// This comment used to say that acquiring it serializes this file's entire walk against
				// every other file's, at about 50% of the lint phase. That was measured and is wrong,
				// and the correction matters because the wrong version sends the next reader hunting a
				// bottleneck that is not there. Instrumented on 2026-08-24, per worker against a 2.45s
				// phase: 1,306ms waiting to acquire and 834ms holding and working, so 53% wait, which
				// reads like confirmation of the old number and is not.
				//
				// High wait does not mean lost throughput, because while one worker waits the other
				// fifteen work. The comparison that settles it is `--single-threaded`: 12.68-13.52s
				// against 2.32-2.52s parallel, so parallel is 5.35x faster. If the lock serialized each
				// file's walk against every other's, those would be nearly equal.
				//
				// The honest statement of the cost is that the lock is why this gets 5.35x rather than
				// something closer to 16x. That lost headroom is real; it is not elapsed time.
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
				if anyRuleNeedsTypeChecker(walkRules) {
					fileChecker, release = g.CheckerForFile(ctx, checkerFile)
				}

				// A rule's Report closure captures the rule it belongs to, so a rule cannot report under
				// another rule's name even by accident.
				//
				// A file being recorded counts its listening and offered rules into maps of its own, so
				// the entry can say which cacheable rules listened on this file; they are merged into the
				// worker's totals at once, before the crash check, exactly as passing the totals in did.
				recording := reuse != nil && (replayed == nil || !hits.typed || !hits.shaped || !hits.design)
				listeningTarget, offeredTarget := localListening, localOffered
				var fileListening, fileOffered map[string]int
				if recording {
					fileListening = make(map[string]int, len(walkRules))
					fileOffered = make(map[string]int, len(walkRules))
					listeningTarget, offeredTarget = fileListening, fileOffered
				}
				diagnosticsBefore := len(localDiagnostics)
				visited, silenced, fileNotes, ruleCrashes, crashed := dispatchFileSafely(sourceFile, func(diagnostic rule.Diagnostic) {
					localDiagnostics = append(localDiagnostics, diagnostic)
					localReporting[diagnostic.RuleName]++
				}, walkRules, g, fileChecker, listeningTarget, offeredTarget, ruleOptions, localTimings, catalog, resolution)

				release()

				if recording {
					for name, count := range fileListening {
						localListening[name] += count
					}
					for name, count := range fileOffered {
						localOffered[name] += count
					}
				}

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
					return
				}

				// A rule that crashed lost its verdict on this file and the rest of the file stands. The file
				// is not recorded in the findings cache, so the next run walks it again and names the crash
				// again rather than replaying a verdict with a hole in it.
				localRuleCrashes = append(localRuleCrashes, ruleCrashes...)
				if len(ruleCrashes) > 0 {
					recording = false
				}

				// A replayed file counts the nodes its full walk visited. The walk counts nodes only when
				// some rule listens, so the uncacheable rules alone could count none and change the
				// coverage line.
				if replayed != nil {
					localNodes += replayed.VisitedNodes
				} else {
					localNodes += visited
				}
				localSuppressed.add(silenced)
				if len(fileNotes) > 0 {
					localNotes[sourceFile.FileName()] = fileNotes
				}

				if recording && replayed == nil {
					if entry, eligible := recordableEntry(sourceFile, keys,
						localDiagnostics[diagnosticsBefore:], fileListening, visited, silenced); eligible {
						reuse.keep(entry)
					}
				}
				if recording && replayed != nil {
					if entry, eligible := refreshClasses(*replayed, keys, hits,
						localDiagnostics[diagnosticsBefore:], fileListening); eligible {
						reuse.keep(entry)
					}
				}
			}

			// This worker's own group first, then files from the end of whichever group has the most left, walked
			// on this worker's own checker. Without the second loop the walk ends when its heaviest group does:
			// measured quiet on ahra (#zqsdzbq), workers ended between 0.89s and 1.59s, the last waiting 0.79s on
			// a checker lock its own group's type check also wanted.
			own := queues[worker]
			if g.WalkOnForeignCheckers {
				own = foreignQueue(queues, homeFiles, worker)
			}
			for index, ok := own.next(); ok; index, ok = own.next() {
				if g.WalkOnForeignCheckers {
					walkFile(index, homeFiles[worker])
				} else {
					walkFile(index, files[index])
				}
			}
			// Off when every file is already on a foreign checker: stealing could hand a worker its own group.
			if homeFiles[worker] != nil && !g.WalkOnForeignCheckers {
				for victim := busiestQueue(queues); victim != nil; victim = busiestQueue(queues) {
					if index, ok := victim.steal(); ok {
						walkFile(index, homeFiles[worker])
					}
				}
			}

			// Appended in whichever order the workers finish. The order is fixed once, after the wait
			// below, rather than here, where it would still depend on who took the lock first.
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
			ruleCrashesAll = append(ruleCrashesAll, localRuleCrashes...)
			for fileName, fileNotes := range localNotes {
				notes[fileName] = fileNotes
			}
			timings.merge(localTimings)
			filesReplayed += localReplayed
			typeAwareRerun += localTypeAwareRerun
			shapeKeyedRerun += localShapeKeyedRerun
			designSystemRerun += localDesignSystemRerun
			filesOnForeignCheckers += localForeign
			mutex.Unlock()
		}()
	}
	waitGroup.Wait()

	// What the design system read, if any rule loaded it, so the cache can key this walk's design-system
	// findings on it. Asked of the program after the walk rather than of the rules during it: the design system
	// is built once per program, whichever file asked first.
	if g.FindingsReuse != nil && !g.CollectTimings {
		g.FindingsReuse.NoteDesignSystem(rule.DesignSystemReads(g.Program))
	}

	sortDiagnostics(diagnostics)

	if len(configFailures) > 0 {
		// One error is enough to stop the run: they are all the same misconfiguration seen once per
		// file, so reporting the first names the problem without printing it 3,408 times.
		return Result{}, fmt.Errorf("rule configuration: %w", configFailures[0])
	}

	return Result{
		Diagnostics:       diagnostics,
		Timings:           timings,
		FilesReplayed:     filesReplayed,
		TypeAwareRerun:    typeAwareRerun,
		ShapeKeyedRerun:   shapeKeyedRerun,
		DesignSystemRerun: designSystemRerun,

		FilesOnForeignCheckers: filesOnForeignCheckers,
		Notes:                  notes,
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
			DeadSuppressions:                suppressed.dead,

			FilesCrashed:      fileCrashes,
			RulesCrashed:      ruleCrashesAll,
			FilesIgnored:      filesIgnored,
			RulesScopedOff:    scopedOff,
			RulesUnconfigured: unconfigured,
		},
	}, nil
}

// ruleNameCatalog answers whether a rule name a suppression comment wrote exists anywhere cohere can
// see: a registered rule, or a key in the config at any severity (a decision recorded about a rule
// cohere has not ported yet still proves the rule exists).
//
// "Exists" is deliberately the same test `suppression.Directive.Covers` applies: the name equals a
// known one, or qualifies it with a plugin prefix on a `/` boundary. A looser test would stay silent
// on a directive that silences nothing, which is the case this exists to report.
type ruleNameCatalog struct {
	names map[string]bool

	// registeredByBareName maps a registered rule's name without its plugin prefix to the full name,
	// so a directive written under a stale prefix (`structure/x` for `nexus/x`) can be told what it
	// probably meant.
	registeredByBareName map[string]string
}

// newRuleNameCatalog returns nil when no registry was supplied, which turns the check off: without
// one, every name would read as unknown.
func newRuleNameCatalog(registered []string, configKeys []string) *ruleNameCatalog {
	if len(registered) == 0 {
		return nil
	}
	catalog := &ruleNameCatalog{
		names:                make(map[string]bool, len(registered)+len(configKeys)),
		registeredByBareName: make(map[string]string, len(registered)),
	}
	for _, name := range registered {
		catalog.names[name] = true
		catalog.registeredByBareName[bareRuleName(name)] = name
	}
	for _, key := range configKeys {
		catalog.names[key] = true
	}
	return catalog
}

func (c *ruleNameCatalog) exists(name string) bool {
	if c.names[name] {
		return true
	}
	for known := range c.names {
		if strings.HasSuffix(name, "/"+known) {
			return true
		}
	}
	return false
}

// reportUnknownRuleReferences reports every rule name a disable or enable comment wrote that exists
// nowhere, the way ESLint does: rule id the unknown name, anchored on the whole comment, message
// beginning with ESLint's own sentence so the two engines read alike (measured on 10.8.1).
//
// Reported straight to the walk rather than through a rule's Report, so no directive can silence a
// finding about a directive that silences nothing.
func reportUnknownRuleReferences(sourceFile *ast.SourceFile, directives *suppression.Index,
	catalog *ruleNameCatalog, report func(rule.Diagnostic)) {
	if catalog == nil {
		return
	}
	for _, reference := range directives.RuleReferences() {
		if catalog.exists(reference.Name) {
			continue
		}
		description := fmt.Sprintf("Definition for rule '%s' was not found. A suppression naming it "+
			"silences nothing, so whatever it was written to allow is reported anyway or was never "+
			"reported at all.", reference.Name)
		if registered, found := catalog.registeredByBareName[bareRuleName(reference.Name)]; found {
			description += fmt.Sprintf(" cohere registers this rule as '%s'.", registered)
		}
		report(rule.Diagnostic{
			RuleName:   reference.Name,
			Range:      core.NewTextRange(reference.Pos, reference.End),
			Message:    rule.Message{Id: "ruleNotFound", Description: description},
			SourceFile: sourceFile,
		})
	}
}

// sortDiagnostics puts a walk's findings in file order, then position, then rule and message.
//
// Without it the order was goroutine scheduling: three runs over one unchanged tree printed three
// orderings of one identical set, so two runs could not be diffed and a walk-order change read as a
// regression nobody caused (#zevtfkq). It was left unsorted once on the grounds that sorting costs
// something on a path over 3,481 files. That cost was never measured and is not what this pays:
// this sorts findings, not files, and the whole ahra tree returned 52 on 2026-10-01.
//
// File then position is the order ESLint and tsc print, so a reader comparing the engines reads the
// same sequence in both. Rule name, message id and description only break ties at one range, and
// they make the order total, so a rule that reports in map order cannot leak that order out.
//
// It is also the boundary the lint cache's note asks for (lint_cache.go, HashRuleSet): a warm run
// replaying stored findings and a cold run walking fresh return one sequence, and cache state cannot
// decide it. The fix phase is unaffected either way, because the edit engine orders its proposals
// itself before resolving overlaps.
func sortDiagnostics(diagnostics []rule.Diagnostic) {
	fileName := func(diagnostic rule.Diagnostic) string {
		if diagnostic.SourceFile == nil {
			return ""
		}
		return diagnostic.SourceFile.FileName()
	}
	slices.SortStableFunc(diagnostics, func(first, second rule.Diagnostic) int {
		return cmp.Or(
			cmp.Compare(fileName(first), fileName(second)),
			cmp.Compare(first.Range.Pos(), second.Range.Pos()),
			cmp.Compare(first.Range.End(), second.Range.End()),
			cmp.Compare(first.RuleName, second.RuleName),
			cmp.Compare(first.Message.Id, second.Message.Id),
			cmp.Compare(first.Message.Description, second.Message.Description),
		)
	})
}

// FileCrash is a file a rule panicked on, and the panic it raised.
//
// The cause is carried rather than summarized because the panic message is the only evidence of what
// went wrong: `Unhandled case in Node.Text: *ast.Token` names the defect precisely, and a count of
// crashes names nothing.
type FileCrash struct {
	FileName string
	Cause    error
}

// RuleCrash is one rule that panicked on one file, and the panic it raised.
type RuleCrash struct {
	RuleName string
	FileName string
	Cause    error
}

// ruleContainment is the boundary around one rule on one file. The first panic is recorded and the
// rule hears nothing more about the file; the other rules never notice.
type ruleContainment struct {
	ruleName string
	fileName string
	crash    *RuleCrash
}

// recoverPanic records the panic the deferring call raised, if any. It must be deferred directly, since
// recover answers only in a function called by the deferral itself.
func (c *ruleContainment) recoverPanic() {
	if recovered := recover(); recovered != nil && c.crash == nil {
		c.crash = &RuleCrash{RuleName: c.ruleName, FileName: c.fileName, Cause: fmt.Errorf("%v", recovered)}
	}
}

// run calls the rule's Run inside the boundary.
func (c *ruleContainment) run(subject rule.Rule, context rule.Context, options any) (listeners rule.Listeners) {
	defer c.recoverPanic()
	return subject.Run(context, options)
}

// listener wraps one of the rule's listeners in the boundary. Once the rule has crashed on this file
// its listeners are skipped, because a rule past its panic is in a state nobody tested.
func (c *ruleContainment) listener(listener func(node *ast.Node)) func(node *ast.Node) {
	return func(node *ast.Node) {
		if c.crash != nil {
			return
		}
		defer c.recoverPanic()
		listener(node)
	}
}

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

// dispatchFileSafely is dispatchFile with a boundary around it.
//
// A rule is ordinary Go code walking a tree it did not build, and the compiler's own accessors panic
// rather than error on shapes they do not handle. `Node.Text()` panics on any kind outside its
// switch, and 220 call sites across the rules reach it. Kind-checking every one is the real fix and
// this is not a substitute for it.
//
// Two boundaries, at two units. A rule's own panic, in its Run or in a listener, is contained to that
// rule on that file by ruleContainment, and the file's other verdicts stand. This one catches what is
// left, a panic outside any rule (reading the file's directives, say), and loses the file, because
// then nothing in it can be trusted. A rule's findings from before its panic are kept: they are real,
// and the crash is named beside them, so nothing claims the rule finished the file.
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
	catalog *ruleNameCatalog,
	resolution configuration.Resolved,
) (visited int, silenced suppressionTally, notes RuleNotes, ruleCrashes []RuleCrash, crashed error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// The visited count is discarded along with the file. A file that crashed halfway
			// contributed nodes to a total that would then describe a walk nobody completed.
			visited = 0
			silenced = suppressionTally{}
			notes = nil
			ruleCrashes = nil
			crashed = fmt.Errorf("%v", recovered)
		}
	}()

	visited, silenced, notes, ruleCrashes = dispatchFile(sourceFile, report, applicable, g, fileChecker,
		listeningCounts, offeredCounts, ruleOptions, timings, catalog, resolution)
	return visited, silenced, notes, ruleCrashes, nil
}

// dispatchFile asks every rule what it wants to hear about in this file, merges those answers into
// one dispatch table, and walks the tree once against it. It returns how many nodes it visited.
//
// Merging before walking is what makes the cost per node independent of the rule count: the walk
// does one table read per node regardless of whether one rule or five hundred registered for that
// kind.

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
	catalog *ruleNameCatalog,
	resolution configuration.Resolved,
) (visitedNodes int, silenced suppressionTally, notes RuleNotes, ruleCrashes []RuleCrash) {
	// A kind may have listeners from several rules, so the merged table holds a slice per kind rather
	// than one function. Indexed by kind rather than keyed by it: the walk reads it once per node, and a
	// slice index is cheaper than a map lookup, about 100ms of CPU across a cold ahra walk (#zqsdzbq).
	merged := make([][]func(node *ast.Node), ast.KindCount)
	listened := false

	// Directives are read once per file, before any rule runs, because every rule's findings filter
	// through the same index. Scanning is proportional to the file rather than to the rule count, so
	// a file with no directives costs one pass and then answers every query with an empty slice.
	directives := suppression.Build(sourceFile.Text())
	reportUnknownRuleReferences(sourceFile, directives, catalog, report)

	// One cache per file, shared by every rule that runs on it. Work a rule derives from the file
	// outside the walk is paid for by that rule alone, so three rules deriving the same thing pay
	// three times. Measured: three comment rules at 1,777ms combined, each visiting one node per
	// file, because each rescanned the same trivia. The walk is shared; this makes the derivations
	// shared too.
	fileCache := rule.NewFileCache()

	// Which rule triggered each cache fill, so its cost can be moved off that rule's total.
	fillPayer := map[string]string{}
	seenFills := map[string]bool{}

	containments := make([]*ruleContainment, 0, len(rules))
	notes = RuleNotes{}

	// Each rule reads the program through a view of what it declared (rule.ProgramReads), built for
	// the whole file in one allocation.
	programViews := rule.ViewProgramForEach(graph.Program, sourceFile, rules)

	for ruleIndex, subject := range rules {
		ruleName := subject.Name

		var timing *RuleTiming
		if timings != nil {
			timing = timings.forRule(ruleName)
		}

		context := rule.Context{
			SourceFile:  sourceFile,
			Program:     programViews[ruleIndex],
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
			RecordNote: func(key string) {
				if notes[ruleName] == nil {
					notes[ruleName] = map[string]int{}
				}
				notes[ruleName][key]++
			},
		}

		// Options are decoded to the type the rule declares rather than handed through as JSON. A
		// rule that receives the wrong shape fails its type assertion and declines every file, which
		// looks exactly like a rule with nothing to report.
		// Counted before Run rather than after, because the question this answers is whether anything
		// ever handed this rule a file. A rule that panics or declines has still been offered one; a
		// rule nobody wired never reaches this line at all.
		offeredCounts[ruleName]++

		containment := &ruleContainment{ruleName: ruleName, fileName: sourceFile.FileName()}
		containments = append(containments, containment)

		setupStart := timingNow(timing)
		listeners := containment.run(subject, context, ruleOptions[ruleName])
		if timing != nil {
			timing.SetupDuration += time.Since(setupStart)
		}
		if containment.crash != nil {
			// Offered and crashed, so it neither declined nor listened.
			continue
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
			merged[kind] = append(merged[kind], containment.listener(attributingListener(timing, listener, ruleName, fileCache, seenFills, fillPayer)))
			listened = true
		}
	}

	if listened {
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

	// The rules this run actually ran, so a directive naming only rules cohere has not ported can be
	// told apart from one whose rule ran and found nothing to silence.
	//
	// Built from `rules` rather than from listeningCounts: a rule that declined every file in this
	// one still ran, and counting it as absent would call its directives unportable when they are
	// simply satisfied.
	//
	// Keyed by the bare name, because that is what a directive is looked up by (see bareRuleName). A
	// registered name carries its plugin (`@typescript-eslint/no-misused-spread`), so keying by it
	// filed every dead directive for a prefixed rule as unported (#6dc4f7k).
	ranRule := make(map[string]bool, len(rules))
	for _, subject := range rules {
		ranRule[bareRuleName(subject.Name)] = true
	}

	for _, containment := range containments {
		if containment.crash != nil {
			ruleCrashes = append(ruleCrashes, *containment.crash)
		}
	}

	return visitedNodes, tally(sourceFile.FileName(), directives, ranRule, resolution), notes, ruleCrashes
}

// suppressionTally is what one file's directives did, summed across the run.
type suppressionTally struct {
	applied          int
	appliedNoReason  int
	unusedDirectives int

	// unusedForUnrunRule is the subset of unusedDirectives naming only rules this run did not run.
	//
	// Separated because the two mean opposite things to a reader. A directive that silenced nothing
	// while its rule ran is dead scaffolding worth deleting. A directive naming a rule cohere has
	// not ported yet silenced nothing because nothing looked, and deleting it would remove a
	// suppression the gate still needs. Reporting them as one number tells a reader to go delete
	// comments that are load-bearing today.
	unusedForUnrunRule int

	// dead is where each unused directive outside unusedForUnrunRule sits.
	dead []DeadSuppression
}

// DeadSuppression is one directive that silenced nothing while a rule it names ran: where it is, and
// the rules it names (none for a blanket directive).
type DeadSuppression struct {
	File  string
	Line  int
	Rules []string
}

func (t *suppressionTally) add(other suppressionTally) {
	t.applied += other.applied
	t.appliedNoReason += other.appliedNoReason
	t.unusedDirectives += other.unusedDirectives
	t.unusedForUnrunRule += other.unusedForUnrunRule
	t.dead = append(t.dead, other.dead...)
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

// namesOnlyOffRules reports whether the config turns off, for this file, every rule a directive named.
//
// Such a directive silences nothing whether or not cohere ports the rules, because a rule that is off
// cannot report, so it is dead rather than waiting on a port. The excuse is for a rule the config
// would run if this binary had it. ESLint flagged 13 `no-await-in-loop` directives in ahra as unused
// and was right, while cohere excused them as unported (#ezhwsbc). One named rule the config would run
// keeps the excuse, because the directive may be load-bearing for that one.
//
// Looked up by the name the directive wrote, which is the name the config is keyed by.
func namesOnlyOffRules(directive *suppression.Directive, resolution configuration.Resolved) bool {
	if len(directive.Rules) == 0 {
		return false
	}
	for _, named := range directive.Rules {
		if status, _ := resolution.StatusOf(named); status != configuration.StatusScopedOff {
			return false
		}
	}
	return true
}

// bareRuleName drops a plugin prefix, so `structure/no-x` and `no-x` compare equal.
//
// Directives and registered rules both may carry the plugin that owns the rule, and not always the same
// way (`core/no-x` against a bare `no-x`), so both sides are compared bare. Comparing either raw files a
// dead directive as naming an unrun rule.
func bareRuleName(name string) string {
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		return name[slash+1:]
	}
	return name
}

// tally reads what a file's directives actually did, after the walk.
//
// The reasonless count is per withheld finding rather than per directive, because that is the
// number that answers the question being asked: how much of what cohere chose not to tell you was
// silenced by someone who did not say why.
func tally(fileName string, directives *suppression.Index, ranRule map[string]bool, resolution configuration.Resolved) suppressionTally {
	counted := suppressionTally{}
	for index, directive := range directives.Directives() {
		applied := directives.AppliedCount(index)
		if applied == 0 {
			counted.unusedDirectives++
			if namesOnlyUnrunRules(directive, ranRule) && !namesOnlyOffRules(directive, resolution) {
				counted.unusedForUnrunRule++
				continue
			}
			counted.dead = append(counted.dead, DeadSuppression{File: fileName, Line: directive.Line, Rules: directive.Rules})
			continue
		}
		counted.applied += applied
		if !directive.HasReason() {
			counted.appliedNoReason += applied
		}
	}
	return counted
}

// walk visits every node once, calling whatever listeners registered for its kind. listeners is indexed
// by kind and holds ast.KindCount entries.
func walk(node *ast.Node, listeners [][]func(node *ast.Node)) int {
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

// assignFilesToWorkers gives each worker the files of one checker, so no two workers ever want the same
// checker and the exclusive lock each file is walked under is never contended.
//
// Files used to be handed out by index stride, on the belief that the compiler assigns a file to a checker
// by its position in the file list, which would keep each worker mostly on one checker. It does not: the
// checker pool partitions files by import affinity, a balanced graph partition, so a stride sent every
// worker across every checker. Traced on a cold ahra run, 2026-10-03 (#zqsdzbq): workers spent 34.4s,
// summed, blocked acquiring a checker, about half of all their time in a walk of about 4.4s. Interleaved
// against the stride on the same tree, three rounds, the walk ran about 25% faster in wall time on the same
// CPU, with identical findings.
//
// Groups are as balanced as the compiler made them for type checking, by files and imports rather than by
// what the rules cost, so the slowest group sets the walk's end.
//
// A walk with no rule that reads a checker strides instead: asking which checker owns a file creates every
// checker, which such a walk never needs.
func (g *Graph) assignFilesToWorkers(ctx context.Context, files []*ast.SourceFile, workers int, needsCheckers bool) [][]int {
	assignments := make([][]int, workers)
	if !needsCheckers {
		for index := range files {
			assignments[index%workers] = append(assignments[index%workers], index)
		}
		return assignments
	}
	slots := map[*checker.Checker]int{}
	for index, sourceFile := range files {
		owner, release := g.Program.GetTypeCheckerForFile(ctx, sourceFile)
		release()
		slot, seen := slots[owner]
		if !seen {
			slot = len(slots) % workers
			slots[owner] = slot
		}
		assignments[slot] = append(assignments[slot], index)
	}
	return assignments
}

// walkQueue is one worker's group of files. The owner takes from the front, and a worker whose own group
// is done takes from the back of the busiest group, walking what it takes on its own checker.
//
// Any checker can type any file of the program; ownership decides which checker reports a file's type
// diagnostics, not which may answer questions about it. What must not happen is one rule mixing answers
// from two checkers, and that cannot: every query for a file comes from the one checker that file is
// walked on. The cost is that the taking checker resolves the stolen file's imports for itself, on a core
// that would otherwise be idle. TestEveryFileWalkedOnAForeignCheckerFindsTheSame holds the findings and the
// type diagnostics identical whichever checker walks a file.
type walkQueue struct {
	mutex       sync.Mutex
	indices     []int
	front, back int
}

// next is the owner's next file.
func (q *walkQueue) next() (int, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.front >= q.back {
		return 0, false
	}
	q.front++
	return q.indices[q.front-1], true
}

// steal is a file from the far end, for another worker.
func (q *walkQueue) steal() (int, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.front >= q.back {
		return 0, false
	}
	q.back--
	return q.indices[q.back], true
}

func (q *walkQueue) remaining() int {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.back - q.front
}

// busiestQueue is the group with the most files left, nil when every group is done. Files left stand in for
// work left: a group's files are of a kind, so its count tracks its cost closely enough to pick a victim.
func busiestQueue(queues []*walkQueue) *walkQueue {
	var busiest *walkQueue
	most := 0
	for _, queue := range queues {
		if left := queue.remaining(); left > most {
			busiest, most = queue, left
		}
	}
	return busiest
}

// foreignQueue is the group a worker walks under WalkOnForeignCheckers: the next worker's that has a group,
// so every file is walked on a checker that does not own it. A worker with no group of its own has no
// checker to lend and walks nothing.
func foreignQueue(queues []*walkQueue, homeFiles []*ast.SourceFile, worker int) *walkQueue {
	if homeFiles[worker] == nil {
		return &walkQueue{}
	}
	for step := 1; step < len(queues); step++ {
		if next := (worker + step) % len(queues); homeFiles[next] != nil {
			return queues[next]
		}
	}
	return queues[worker]
}
