package program

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/suppression"
)

// fileDispatcher is one worker's dispatch machinery, made once and reused for every file the worker walks.
//
// Everything a rule was handed for a file used to be made for that file: a Report closure, a RecordNote
// closure and a containment per rule, a wrapping closure per listener, and the merged table's slices. Over
// 484 rules and 3,978 files that was about 370 MB of a cold ahra run's allocation, for answers that do not
// change between files (#9jpmqm9). Now each rule has one slot per worker, made the first time the worker
// meets the rule, and the slot's closures read the file being dispatched from the dispatcher.
//
// That makes a rule's Report good only while its file is being dispatched, which is what rule.Context
// says. A rule that kept it and called it later would file a finding against whichever file the worker had
// moved on to, so between files the dispatcher holds no file and a late Report panics, naming the rule.
// Checked when this was written: no rule keeps a Context, a Report or a RecordNote past Run and its
// listeners, none starts a goroutine, and the run-scoped caches (the design system, the theme map, the
// import indexes) keep only what they built.
//
// Not safe for concurrent use, and it need not be: a worker dispatches one file at a time.
type fileDispatcher struct {
	graph   *Graph
	timings *Timings
	catalog *ruleNameCatalog

	// slots is this worker's slot for each rule it has met, by name.
	slots map[string]*ruleSlot

	// unrunRuleReferences counts, by name, the rules this worker's directives named that cohere doesn't
	// run. See reportUnknownRuleReferences.
	unrunRuleReferences map[string]int

	// listeners is the merged dispatch table of the file being dispatched, indexed by kind. A kind may have
	// listeners from several rules, so each holds a slice, and a slice index rather than a map key, since the
	// walk reads it once per node: about 100ms of CPU across a cold ahra walk (#zqsdzbq). Kept across files:
	// after each walk the kinds in used are emptied, and their backing arrays serve the next file.
	listeners [][]listenerCall
	used      []ast.Kind

	// The file being dispatched, all nil between files.
	sourceFile *ast.SourceFile
	directives *suppression.Index
	notes      RuleNotes
	report     func(rule.Diagnostic)

	// readiness is the file's readiness measurement, nil when the run measures none. See Readiness.
	readiness *fileReadiness

	// withheld is the findings the file's directives silenced, by rule and directive, for the findings cache.
	withheld []LintCacheWithheld

	// replayed is what the findings cache replayed on the file, nil when it replayed nothing. Set by the walk
	// before the dispatch. See directiveReplay.
	replayed *directiveReplay
}

// directiveReplay is what a dispatch needs from a replayed file's entry to count its directives as a walk of every
// rule would (#kdee854): the replayed rules' withheld findings, each marked applied on its directive before the
// tally, and every rule applicable to the file, replayed ones included, as the rules that ran.
type directiveReplay struct {
	withheld []LintCacheWithheld
	rules    []rule.Rule
}

// ruleSlot is one rule on one worker: what dispatching the rule needs that does not change between files.
type ruleSlot struct {
	name   string
	timing *RuleTiming

	// containment is the rule's boundary on the file being dispatched, made afresh each time the rule is
	// offered a file.
	containment ruleContainment

	report     func(rule.Diagnostic)
	recordNote func(key string)

	// programView is the rule's view of the program, pointed at each file this worker dispatches and
	// released when the file ends.
	programView rule.ProgramViewSlot
}

// listenerCall is one rule's listener for one kind in the merged table. It is called through its rule's
// slot rather than wrapped, so the table holds no closure of its own.
type listenerCall struct {
	listener func(node *ast.Node)
	slot     *ruleSlot
}

func newFileDispatcher(graph *Graph, timings *Timings, catalog *ruleNameCatalog) *fileDispatcher {
	return &fileDispatcher{
		graph:     graph,
		timings:   timings,
		catalog:   catalog,
		slots:     map[string]*ruleSlot{},
		listeners: make([][]listenerCall, ast.KindCount),

		unrunRuleReferences: map[string]int{},
	}
}

// slotsFor is the slots of a selection's applicable rules, in the same order, found once per selection.
//
// Mutates: selection keeps the slots found here, so every later file of the selection reuses them
func (dispatcher *fileDispatcher) slotsFor(selection *ruleSelection) []*ruleSlot {
	if selection.slots == nil {
		selection.slots = make([]*ruleSlot, len(selection.applicable))
		for index, subject := range selection.applicable {
			selection.slots[index] = dispatcher.slotFor(subject.Name)
		}
	}
	return selection.slots
}

// slotFor is the worker's slot for a rule, made the first time it is asked for.
//
// The slot's Report carries the rule's name, so a rule cannot report under another rule's name even by
// accident.
func (dispatcher *fileDispatcher) slotFor(ruleName string) *ruleSlot {
	if slot, made := dispatcher.slots[ruleName]; made {
		return slot
	}
	slot := &ruleSlot{name: ruleName, timing: dispatcher.timings.forRule(ruleName)}
	slot.report = func(diagnostic rule.Diagnostic) {
		dispatcher.requireFile(ruleName, "Report")
		diagnostic.RuleName = ruleName
		if diagnostic.SourceFile == nil {
			diagnostic.SourceFile = dispatcher.sourceFile
		}

		// Filtering here rather than after the walk is what keeps a suppressed finding from
		// ever existing as a finding. The alternative (collect everything, drop some later)
		// leaves a window where a caller can read the unfiltered slice and report a number the
		// user will never see explained.
		if slot.timing != nil {
			slot.timing.Findings++
		}

		// Readiness counts before suppression, since Adamic reads no disable comment, and a rule running
		// measure-only reports nothing at all: its findings never print, fail the run or reach the edit
		// engine with a fix.
		dispatcher.readiness.count(ruleName)
		if dispatcher.readiness.hidden(ruleName) {
			return
		}

		if directive := dispatcher.directives.SuppressedBy(
			diagnostic.RuleName,
			diagnostic.Range.Pos(),
		); directive >= 0 {
			dispatcher.withheld = append(
				dispatcher.withheld,
				LintCacheWithheld{RuleName: diagnostic.RuleName, Directive: int32(directive)},
			)
			return
		}

		dispatcher.report(diagnostic)
	}
	slot.recordNote = func(key string) {
		dispatcher.requireFile(ruleName, "RecordNote")
		dispatcher.readiness.note(ruleName, key)
		if dispatcher.readiness.hidden(ruleName) {
			return
		}
		// Made for the file on its first note, and never reused: the file's notes outlive its dispatch, in
		// the walk's result and the findings cache.
		if dispatcher.notes == nil {
			dispatcher.notes = RuleNotes{}
		}
		if dispatcher.notes[ruleName] == nil {
			dispatcher.notes[ruleName] = map[string]int{}
		}
		dispatcher.notes[ruleName][key]++
	}
	dispatcher.slots[ruleName] = slot
	return slot
}

// requireFile panics unless a file is being dispatched: a rule called one of its Context's functions after
// the file it was handed for was over. See fileDispatcher.
func (dispatcher *fileDispatcher) requireFile(ruleName string, function string) {
	if dispatcher.sourceFile == nil {
		// Invariant: the walk calls a rule's Context only while that rule's file is dispatched, so only a rule
		// that kept its Context past Run and its listeners gets here, a bug in that rule this names.
		panic(fmt.Sprintf("rule %s called its Context's %s after its file's dispatch ended; a Context is good only "+
			"during Run and the listeners Run returns", ruleName, function))
	}
}

// hear calls one of the rule's listeners inside its containment. Once the rule has crashed on this file its
// listeners are skipped, because a rule past its panic is in a state nobody tested.
func (slot *ruleSlot) hear(meter *workerMeter, listener func(node *ast.Node), node *ast.Node) {
	if slot.containment.crash != nil {
		return
	}
	defer slot.containment.recoverPanic()
	meter.call(slot.timing, listener, node)
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
//
// rules and slots are aligned: slots[index] is the worker's slot for rules[index].
func (dispatcher *fileDispatcher) dispatchFileSafely(
	sourceFile *ast.SourceFile,
	report func(rule.Diagnostic),
	rules []rule.Rule,
	slots []*ruleSlot,
	fileChecker *checker.Checker,
	listeningCounts map[string]int,
	offeredCounts map[string]int,
	ruleOptions map[string]any,
	measureOnly map[string]bool,
	resolution configuration.Resolved,
) (
	visited int,
	silenced suppressionTally,
	notes RuleNotes,
	ruleCrashes []RuleCrash,
	readiness *fileReadiness,
	crashed error,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// The visited count is discarded along with the file. A file that crashed halfway
			// contributed nodes to a total that would then describe a walk nobody completed.
			visited = 0
			silenced = suppressionTally{}
			notes = nil
			ruleCrashes = nil
			readiness = nil
			crashed = fmt.Errorf("%v", recovered)
		}
	}()

	visited, silenced, notes, ruleCrashes, readiness = dispatcher.dispatchFile(
		sourceFile,
		report,
		rules,
		slots,
		fileChecker,
		listeningCounts,
		offeredCounts,
		ruleOptions,
		measureOnly,
		resolution,
	)
	return visited, silenced, notes, ruleCrashes, readiness, nil
}

// dispatchFile asks every rule what it wants to hear about in this file, merges those answers into
// one dispatch table, and walks the tree once against it. It returns how many nodes it visited.
//
// Merging before walking is what makes the cost per node independent of the rule count: the walk
// does one table read per node regardless of whether one rule or five hundred registered for that
// kind.
//
// Mutates: listeningCounts counts each rule that listened on the file, for the caller's totals
// Mutates: offeredCounts counts each rule the file was offered to, for the caller's totals
func (dispatcher *fileDispatcher) dispatchFile(
	sourceFile *ast.SourceFile,
	report func(rule.Diagnostic),
	rules []rule.Rule,
	slots []*ruleSlot,
	fileChecker *checker.Checker,
	listeningCounts map[string]int,
	offeredCounts map[string]int,
	ruleOptions map[string]any,
	measureOnly map[string]bool,
	resolution configuration.Resolved,
) (visitedNodes int, silenced suppressionTally, notes RuleNotes, ruleCrashes []RuleCrash, readiness *fileReadiness) {
	listened := false

	// Directives are read once per file, before any rule runs, because every rule's findings filter
	// through the same index. Scanning is proportional to the file rather than to the rule count, so
	// a file with no directives costs one pass and then answers every query with an empty slice.
	directives := suppression.Build(sourceFile.Text())
	reportUnknownRuleReferences(sourceFile, directives, dispatcher.catalog, report, dispatcher.unrunRuleReferences)
	// The replayed rules' withheld findings, which this walk does not produce. FindingsReuse.lookup proved each
	// names a directive that can silence its rule.
	ranRules := rules
	if dispatcher.replayed != nil {
		for _, withheld := range dispatcher.replayed.withheld {
			directives.MarkApplied(int(withheld.Directive), withheld.RuleName)
		}
		ranRules = dispatcher.replayed.rules
	}

	dispatcher.sourceFile, dispatcher.directives, dispatcher.report = sourceFile, directives, report
	dispatcher.readiness = newFileReadiness(dispatcher.graph.Readiness, rules, measureOnly)
	readiness = dispatcher.readiness
	// However the dispatch ends, a panic included, so the next file starts from an empty table and no slot
	// can report into a file that is over, or read the program through it.
	defer dispatcher.endFile(slots)

	// One cache per file, shared by every rule that runs on it. Work a rule derives from the file
	// outside the walk is paid for by that rule alone, so three rules deriving the same thing pay
	// three times. Measured: three comment rules at 1,777ms combined, each visiting one node per
	// file, because each rescanned the same trivia. The walk is shared; this makes the derivations
	// shared too.
	fileCache := rule.NewFileCache()

	// Under --timing, the worker's meter, which bills each call in this file to its rule and each cache
	// fill to its derivation. Nil otherwise.
	meter := dispatcher.timings.workerMeter()
	meter.watchFills(fileCache)

	for ruleIndex, subject := range rules {
		slot := slots[ruleIndex]

		// The rule reads the program through a view of what it declared (rule.ProgramReads), kept in its
		// slot rather than built for each file.
		context := rule.Context{
			SourceFile:  sourceFile,
			Program:     slot.programView.Point(dispatcher.graph.Program, sourceFile, subject),
			TypeChecker: fileChecker,
			FileCache:   fileCache,
			Report:      slot.report,
			RecordNote:  slot.recordNote,
		}

		// Options are decoded to the type the rule declares rather than handed through as JSON. A
		// rule that receives the wrong shape fails its type assertion and declines every file, which
		// looks exactly like a rule with nothing to report.
		// Counted before Run rather than after, because the question this answers is whether anything
		// ever handed this rule a file. A rule that panics or declines has still been offered one; a
		// rule nobody wired never reaches this line at all.
		offeredCounts[subject.Name]++

		slot.containment = ruleContainment{ruleName: subject.Name, fileName: sourceFile.FileName().AsString()}

		// Branched rather than always passing a closure, which would allocate once per rule per file on
		// every run to serve a flag most runs don't set.
		var listeners rule.Listeners
		if meter == nil {
			listeners = slot.containment.run(subject, context, ruleOptions[subject.Name])
		} else {
			meter.setup(slot.timing, func() {
				listeners = slot.containment.run(subject, context, ruleOptions[subject.Name])
			})
		}
		if slot.containment.crash != nil {
			// Offered and crashed, so it neither declined nor listened.
			continue
		}

		if len(listeners) == 0 {
			if slot.timing != nil {
				// A rule that declined. Counted so that a rule doing expensive setup and then
				// declining every file is visible as exactly that, rather than as a cheap rule.
				slot.timing.FilesDeclined++
			}
			// The rule looked at the file and declined it. That is the cheapest and most valuable thing
			// a rule can do, and it is counted rather than ignored so a rule that declines *everything*
			// is visible as a rule that never ran.
			continue
		}

		listeningCounts[subject.Name]++
		if slot.timing != nil {
			slot.timing.FilesListened++
		}
		for kind, listener := range listeners {
			if len(dispatcher.listeners[kind]) == 0 {
				dispatcher.used = append(dispatcher.used, kind)
			}
			dispatcher.listeners[kind] = append(
				dispatcher.listeners[kind],
				listenerCall{listener: listener, slot: slot},
			)
			listened = true
		}
	}

	if listened {
		visitedNodes = dispatcher.walk(sourceFile.AsNode(), meter)
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
	//
	// Only directives read it, and most files have none, so it is built only for a file that has one:
	// built for every file it was 42 MB of a cold ahra run's allocation, nearly all of it read by nothing
	// (#9jpmqm9).
	//
	// For a replayed file, every rule applicable to it, since the replayed ones ran too, only earlier.
	var ranRule map[string]bool
	if len(directives.Directives()) > 0 {
		ranRule = make(map[string]bool, len(ranRules))
		for _, subject := range ranRules {
			ranRule[bareRuleName(subject.Name)] = true
		}
	}

	for _, slot := range slots {
		if slot.containment.crash != nil {
			ruleCrashes = append(ruleCrashes, *slot.containment.crash)
		}
	}

	if resolution.UnusedDisableSeverity() != configuration.SeverityOff {
		reportDeadDirectives(sourceFile, directives, ranRules, measureOnly, ruleCrashes, report)
	}
	silenced = tally(sourceFile.FileName().AsString(), directives, ranRule, resolution)
	silenced.directives = len(directives.Directives()) > 0 || len(directives.RuleReferences()) > 0
	silenced.withheld = dispatcher.withheld
	return visitedNodes, silenced, dispatcher.notes, ruleCrashes, readiness
}

// endFile empties the table the file filled and lets go of the file. Each emptied kind is cleared before it
// is shortened, so the table keeps no listener, and through it no rule's state, from a file that is over.
// Each slot's program view is released, so a rule that kept its Program past the file fails on its next read.
func (dispatcher *fileDispatcher) endFile(slots []*ruleSlot) {
	for _, slot := range slots {
		slot.programView.Release()
	}
	for _, kind := range dispatcher.used {
		clear(dispatcher.listeners[kind])
		dispatcher.listeners[kind] = dispatcher.listeners[kind][:0]
	}
	dispatcher.used = dispatcher.used[:0]
	dispatcher.sourceFile, dispatcher.directives, dispatcher.notes = nil, nil, nil
	dispatcher.report, dispatcher.readiness = nil, nil
	dispatcher.withheld, dispatcher.replayed = nil, nil
}

// walk is the package's walk over the dispatcher's table: every node once, each listener called through its
// rule's slot.
func (dispatcher *fileDispatcher) walk(node *ast.Node, meter *workerMeter) int {
	if node == nil {
		return 0
	}

	visited := 1
	for _, call := range dispatcher.listeners[node.Kind] {
		call.slot.hear(meter, call.listener, node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		visited += dispatcher.walk(child, meter)
		return false
	})

	return visited
}
