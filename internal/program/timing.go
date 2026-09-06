package program

import (
	"sort"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
)

// RuleTiming is what one rule cost over a run.
//
// Setup and listener time are kept apart because they are different defects. A rule burning time in
// Run and then declining the file is doing expensive work to decide it has nothing to do, which is
// the shape that made consistency-no-abbreviated-identifier 2,580ms of a 3,700ms run: it resolved a
// scope lookup on every identifier before testing whether the name even looked abbreviated. A cheap
// structural gate took it to 152ms, 17x, findings byte-identical. A rule burning time in its
// listeners is doing expensive work per node, which is a different fix.
type RuleTiming struct {
	Name string

	// SetupDuration is time inside the rule's Run function, deciding what to listen to.
	SetupDuration time.Duration

	// ListenerDuration is time inside the rule's listeners, walking nodes.
	ListenerDuration time.Duration

	// FilesListened is how many files the rule accepted, and FilesDeclined how many it refused.
	//
	// Declining is the cheapest and most valuable thing a rule can do, so a rule that declines
	// nothing is worth seeing: registering for a very common node kind and returning early is
	// legitimate, and it is also how a cheap-looking rule becomes expensive at scale.
	FilesListened int
	FilesDeclined int

	// NodesOffered is how many nodes the walk called this rule's listeners for.
	NodesOffered int

	// Findings is how many diagnostics the rule reported, before suppression.
	Findings int
}

// TotalDuration is what the rule cost altogether.
func (r RuleTiming) TotalDuration() time.Duration {
	return r.SetupDuration + r.ListenerDuration
}

// Timings collects per-rule cost across a run.
//
// Timing is per file per rule rather than per listener call, and that is a measured decision rather
// than a convenience. A time.Now pair costs about 42ns against a listener body of well under a
// nanosecond, so timing every call across two million nodes adds at least 14 percent to a 634ms run
// and the instrument starts dominating what it measures. Per file per rule is about 2.9ms across
// 3,407 files and 20 rules, which is 0.46 percent.
//
// The tradeoff is real and worth stating rather than hiding: this cannot say which node kind inside
// a rule is slow, only which rule is. That is the question --timing was asked to answer, and the
// finer one belongs to --explain on a single file where the overhead no longer matters.
type Timings struct {
	byRule map[string]*RuleTiming

	// sharedFills is what each cached derivation cost, summed across files, keyed by the cache key.
	//
	// Kept apart from any rule's total because it belongs to no rule. Whichever rule asks for a
	// derivation first pays for it, and files are walked in parallel, so that identity varies. Three
	// comment rules sharing one scan measured 171ms, 132ms, and 1.0ms for identical work; the cheap
	// one had simply asked last. Attributing shared cost to a rule is the instrument lying, and
	// reporting it separately is the only reading that survives a change in order.
	sharedFills map[string]time.Duration
}

// NewTimings returns a collector ready for a run.
func NewTimings(ruleNames []string) *Timings {
	byRule := make(map[string]*RuleTiming, len(ruleNames))
	for _, name := range ruleNames {
		byRule[name] = &RuleTiming{Name: name}
	}
	return &Timings{byRule: byRule, sharedFills: map[string]time.Duration{}}
}

// forRule returns the accumulator for a rule, creating it if the run added one late.
func (t *Timings) forRule(name string) *RuleTiming {
	if t == nil {
		return nil
	}
	timing, known := t.byRule[name]
	if !known {
		timing = &RuleTiming{Name: name}
		t.byRule[name] = timing
	}
	return timing
}

// merge folds one worker's local timings into the shared set.
//
// Workers accumulate locally and merge once under the mutex, the same way diagnostics and coverage
// already do. Timing every rule through a shared lock would measure lock contention rather than
// rule cost.
func (t *Timings) merge(other *Timings) {
	if t == nil || other == nil {
		return
	}
	for key, cost := range other.sharedFills {
		t.sharedFills[key] += cost
	}
	for name, source := range other.byRule {
		target := t.forRule(name)
		target.SetupDuration += source.SetupDuration
		target.ListenerDuration += source.ListenerDuration
		target.FilesListened += source.FilesListened
		target.FilesDeclined += source.FilesDeclined
		target.NodesOffered += source.NodesOffered
		target.Findings += source.Findings
	}
}

// Sorted returns every rule's timing, most expensive first.
//
// Sorted by cost so an outlier is obvious without arithmetic, with the name as a tiebreak so two
// runs over the same tree produce the same order and a diff between them means something.
func (t *Timings) Sorted() []RuleTiming {
	if t == nil {
		return nil
	}
	sorted := make([]RuleTiming, 0, len(t.byRule))
	for _, timing := range t.byRule {
		sorted = append(sorted, *timing)
	}
	sort.SliceStable(sorted, func(first, second int) bool {
		if sorted[first].TotalDuration() != sorted[second].TotalDuration() {
			return sorted[first].TotalDuration() > sorted[second].TotalDuration()
		}
		return sorted[first].Name < sorted[second].Name
	})
	return sorted
}

// RecordSharedFill notes what a cached derivation cost on one file, and subtracts it from the rule
// that happened to trigger it.
//
// The subtraction is the point. Without it the first rule to ask carries a cost that belongs to
// every rule sharing the derivation, and the table names an arbitrary victim.
func (t *Timings) RecordSharedFill(ruleName string, key string, cost time.Duration) {
	if t == nil || cost <= 0 {
		return
	}
	t.sharedFills[key] += cost
	if timing := t.forRule(ruleName); timing != nil {
		timing.ListenerDuration -= cost
		if timing.ListenerDuration < 0 {
			timing.ListenerDuration = 0
		}
	}
}

// SharedFills is what each cached derivation cost across the run, keyed by cache key.
func (t *Timings) SharedFills() map[string]time.Duration {
	if t == nil {
		return nil
	}
	return t.sharedFills
}

// TotalDuration is the summed cost of every rule.
func (t *Timings) TotalDuration() time.Duration {
	if t == nil {
		return 0
	}
	var total time.Duration
	for _, timing := range t.byRule {
		total += timing.TotalDuration()
	}
	return total
}

// timingNow returns a start time only when something is actually being timed.
//
// time.Now costs about 42ns, which is nothing once but real across millions of calls, so the
// instrument declines to read the clock when nobody asked for the number.
func timingNow(timing *RuleTiming) time.Time {
	if timing == nil {
		return time.Time{}
	}
	return time.Now()
}

// measuringListener wraps a listener to count nodes and accumulate its own time.
//
// This times each call, which the earlier design deliberately avoided: a time.Now pair costs about
// 42ns against a listener body well under a nanosecond, so this is genuinely expensive across two
// million nodes. It is paid only under --timing, and it is paid because the cheap alternative was
// measured and found to be wrong.
//
// The cheap version attributed each file's walk time across rules in proportion to nodes offered.
// That is right for rules whose per-call cost is similar and badly wrong otherwise, and the planted
// control proved it: a rule sleeping 200 microseconds per file, about 680ms in total, reported as
// 7.8ms and sat near the bottom of the table. It registered for SourceFile, so it was offered 3,407
// nodes against another rule's 681,426, and the proportion buried it.
//
// A timing instrument that cannot find a deliberately planted slow rule has not been shown to work.
// So the honest measurement costs what it costs, and the overhead is reported in the header rather
// than hidden: a --timing run is slower than a real one, and the table says so.
func measuringListener(timing *RuleTiming, listener func(node *ast.Node)) func(node *ast.Node) {
	if timing == nil {
		return listener
	}
	return func(node *ast.Node) {
		timing.NodesOffered++
		start := time.Now()
		listener(node)
		timing.ListenerDuration += time.Since(start)
	}
}

// attributingListener is measuringListener plus a note of which rule triggered a cache fill.
//
// The attribution has to happen at the listener boundary rather than after Run, because a rule
// derives shared work inside its listener, not while deciding what to listen to. Checking after Run
// finds an empty cache and attributes nothing, which is a silent no-op: the table looks the same as
// before and the correction never fires.
func attributingListener(
	timing *RuleTiming,
	listener func(node *ast.Node),
	ruleName string,
	fileCache *rule.FileCache,
	seenFills map[string]bool,
	fillPayer map[string]string,
) func(node *ast.Node) {
	if timing == nil {
		return listener
	}
	return func(node *ast.Node) {
		timing.NodesOffered++
		start := time.Now()
		listener(node)
		timing.ListenerDuration += time.Since(start)

		for key := range fileCache.FillDurations() {
			if !seenFills[key] {
				seenFills[key] = true
				fillPayer[key] = ruleName
			}
		}
	}
}

// SetForTest populates one rule's measured cost, for tests that need a known table without running
// a program. Not used outside tests.
func (t *Timings) SetForTest(name string, cost time.Duration, nodesOffered int) {
	timing := t.forRule(name)
	if timing == nil {
		return
	}
	timing.ListenerDuration = cost
	timing.NodesOffered = nodesOffered
	timing.FilesListened = 1
}
