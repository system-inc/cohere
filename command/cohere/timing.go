package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// printTimings renders per-rule cost, most expensive first.
//
// A rule budget you cannot see is a rule budget you cannot hold. Every performance win in the
// migration this tool replaces came from a measurement nobody could have inferred: the worst
// offender cost 2,580ms of a 3,700ms run because it resolved a scope lookup on every identifier
// before testing whether the name even looked abbreviated, and a cheap structural gate took it to
// 152ms with byte-identical findings. Nobody predicted that. A timing table found it.
//
// Setup and listeners are separate columns because they are different defects. Time in setup means
// a rule is doing expensive work to decide it has nothing to do. Time in listeners means it is
// doing expensive work per node. The fixes have nothing in common.
func printTimings(out io.Writer, timings *program.Timings, lintDuration time.Duration) {
	sorted := timings.Sorted()
	if len(sorted) == 0 {
		// A timing table with no rows is the instrument measuring nothing, which must not print as
		// though it measured everything and found it fast.
		fmt.Fprintf(out, "\ntiming: no rules ran, so there is nothing to measure\n")
		return
	}

	// Attributed time is summed across workers, so it exceeds wall clock by roughly the worker
	// count. Reporting it against wall clock would print shares over 100 percent and read as a bug
	// in the measurement rather than as parallelism. Shares are taken against the attributed total,
	// which is the number they are actually a share of.
	attributed := timings.TotalDuration()

	fmt.Fprintf(out, "\ntiming: %d rules, %s of rule time across all workers, %s wall clock\n",
		len(sorted), formatMilliseconds(attributed), formatMilliseconds(lintDuration))
	// Said out loud rather than left for someone to discover: a --timing run is slower than a real
	// one. Each listener call is wrapped in a time.Now pair costing about 42ns against a listener
	// body well under a nanosecond, which is why these numbers are for comparing rules to each
	// other and never for quoting as the tool's speed.
	fmt.Fprintf(out, "  (a --timing run is slower than a real one: every listener call is timed. "+
		"Compare rules to each other, not these totals to a normal run.)\n")
	printCoverage(out, attributed, lintDuration)
	fmt.Fprintf(out, "  %-42s %9s %9s %9s %8s %8s %8s\n",
		"rule", "total", "setup", "listen", "files", "nodes", "found")

	for _, timing := range sorted {
		share := ""
		if attributed > 0 {
			share = fmt.Sprintf(" %5.1f%%", 100*float64(timing.TotalDuration())/float64(attributed))
		}

		fmt.Fprintf(out, "  %-42s %9s %9s %9s %8d %8d %8d%s\n",
			timing.Name,
			formatMilliseconds(timing.TotalDuration()),
			formatMilliseconds(timing.SetupDuration),
			formatMilliseconds(timing.ListenerDuration),
			timing.FilesListened,
			timing.NodesOffered,
			timing.Findings,
			share,
		)
	}

	printSharedFills(out, timings)
	printTimingNotes(out, sorted)
}

// printSharedFills reports work that several rules share, which belongs to none of them.
//
// Without this the first rule to ask for a cached derivation carries its whole cost, and since
// files are walked in parallel that rule is arbitrary. Measured before this existed: three comment
// rules doing identical work reported 171ms, 132ms, and 1.0ms, and the cheap one had simply asked
// last. A reader would have concluded the 171ms rule was expensive and optimized the wrong thing.
func printSharedFills(out io.Writer, timings *program.Timings) {
	fills := timings.SharedFills()
	if len(fills) == 0 {
		return
	}

	// Keys are collapsed to their family before reporting, because a key identifies a cache entry
	// and a cache entry is not a line worth reading. The HIR cache keys per function node, by kind
	// and source offset, which is correct for a cache and produced 11,150 rows here against three
	// real ones: `--timing` was 17,000 lines, two thirds of them a single derivation reported one
	// function at a time, each costing 0.00ms and none of them actionable.
	//
	// The family is everything before the first colon, which is how these keys are already built
	// (`hir.Function:175:10055`, `comments.All`). A key with no colon is its own family, so the
	// three genuinely distinct entries are unchanged.
	type sharedFamily struct {
		duration time.Duration
		entries  int
	}
	families := make(map[string]*sharedFamily, len(fills))
	for key, duration := range fills {
		name := key
		if colon := strings.IndexByte(key, ':'); colon >= 0 {
			name = key[:colon]
		}
		family, seen := families[name]
		if !seen {
			family = &sharedFamily{}
			families[name] = family
		}
		family.duration += duration
		family.entries++
	}

	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		family := families[name]
		if family.entries == 1 {
			fmt.Fprintf(out, "  shared: %s cost %s, paid once per file and used by several rules\n",
				name, formatMilliseconds(family.duration))
			continue
		}
		fmt.Fprintf(out, "  shared: %s cost %s across %d entries, paid once per file and used by several rules\n",
			name, formatMilliseconds(family.duration), family.entries)
	}
}

// printTimingNotes names the two shapes a per-rule table does not make obvious on its own.
//
// The coverage line already reports a rule that listened to nothing. These are the other two the
// brief asked for: a rule that listens to everything, and work duplicated across rules.
func printTimingNotes(out io.Writer, sorted []program.RuleTiming) {
	mostOffered := 0
	for _, timing := range sorted {
		if timing.NodesOffered > mostOffered {
			mostOffered = timing.NodesOffered
		}
	}
	if mostOffered == 0 {
		return
	}

	// The rule offered the most nodes is the reference point for every other row, not a warning
	// about that rule. Registering for a common kind and returning early is cheap, so this row shows
	// what a well-gated rule costs at maximum node volume. A rule with comparable nodes and much
	// higher time is doing expensive work per node, and that comparison is what makes the case.
	//
	// This started as a warning and was reworded, because the framing was backwards on the first
	// real run: consistency-no-ambiguous-identifier had the most nodes and was among the cheapest,
	// while a rule with slightly fewer nodes cost sixty times more.
	for _, timing := range sorted {
		if timing.NodesOffered < mostOffered || timing.FilesListened == 0 {
			continue
		}

		perNode := ""
		if timing.NodesOffered > 0 {
			perNode = fmt.Sprintf(", %.0fns per node", float64(timing.TotalDuration().Nanoseconds())/float64(timing.NodesOffered))
		}
		fmt.Fprintf(out,
			"  note: %s saw the most nodes of any rule (%d%s) — compare a rule's cost against this one, not against its own node count\n",
			timing.Name, timing.NodesOffered, perNode)
	}

	// Duplicated work is invisible per rule and obvious in aggregate: three rules independently
	// scanning the same comment trivia each look reasonable and together cost three scans. Rules
	// offered the same node count are the candidates, since they registered for the same kinds.
	byOffered := map[int][]string{}
	for _, timing := range sorted {
		if timing.NodesOffered > 0 {
			byOffered[timing.NodesOffered] = append(byOffered[timing.NodesOffered], timing.Name)
		}
	}
	// A pair counts, not just three or more. The defect that motivated this note was found on a
	// pair: two rules offered exactly 3,407 nodes and differing 83x in cost, where one had filled a
	// shared cache the other read for free. Requiring three would have missed it.
	//
	// The cost spread is what makes it worth printing. Rules registered for the same kind and
	// costing about the same are unremarkable; the same rules an order of magnitude apart mean one
	// of them is carrying work the others are not.
	offeredCounts := make([]int, 0, len(byOffered))
	for offered := range byOffered {
		offeredCounts = append(offeredCounts, offered)
	}
	sort.Ints(offeredCounts)

	for _, offered := range offeredCounts {
		names := byOffered[offered]
		if len(names) < 2 {
			continue
		}

		extremes := costRange(sorted, names)
		if extremes.cheapestCost <= 0 || float64(extremes.dearestCost)/float64(extremes.cheapestCost) < 10 {
			continue
		}

		fmt.Fprintf(out,
			"  note: %s and %s were each offered %d nodes but differ %.0fx in cost (%s against %s) — if they share work, this table may be billing it to whichever ran first\n",
			extremes.dearestName, extremes.cheapestName, offered,
			float64(extremes.dearestCost)/float64(extremes.cheapestCost),
			formatMilliseconds(extremes.dearestCost), formatMilliseconds(extremes.cheapestCost))
	}
}

// costExtremes is the cheapest and dearest rule in a group, each with the name it belongs to.
//
// Name and cost travel together rather than being looked up separately, because separating them is
// exactly the bug this replaced: the caller printed names[0] and names[len-1] while the costs came
// from a scan for the minimum and maximum. With two rules those coincide; with three they come
// apart, and the note then quotes one rule's cost beside another rule's name.
//
// Reproduced before the fix on three rules at 3,407 nodes each: the line named a 50ms rule and
// quoted 400ms, and the rule that actually cost 400ms did not appear at all. A reader chases the
// wrong rule, which is the failure this note exists to prevent.
type costExtremes struct {
	cheapestName string
	cheapestCost time.Duration
	dearestName  string
	dearestCost  time.Duration
}

// costRange finds the cheapest and dearest among the named rules.
//
// Reported as a spread rather than a total because the spread is the signal: equal node counts with
// equal cost is a coincidence, and equal node counts an order of magnitude apart is a rule carrying
// somebody else's work.
func costRange(sorted []program.RuleTiming, names []string) costExtremes {
	named := map[string]bool{}
	for _, name := range names {
		named[name] = true
	}

	extremes := costExtremes{}
	for _, timing := range sorted {
		if !named[timing.Name] {
			continue
		}
		total := timing.TotalDuration()
		if extremes.cheapestName == "" || total < extremes.cheapestCost {
			extremes.cheapestName, extremes.cheapestCost = timing.Name, total
		}
		if extremes.dearestName == "" || total > extremes.dearestCost {
			extremes.dearestName, extremes.dearestCost = timing.Name, total
		}
	}
	return extremes
}

// formatMilliseconds reads at a glance, which is the resolution any of these numbers means anything
// at. Sub-millisecond costs print as a fraction rather than as a bare 0, because a rule at 0.4ms
// and a rule that never ran are different facts.
func formatMilliseconds(duration time.Duration) string {
	milliseconds := float64(duration) / float64(time.Millisecond)
	if milliseconds >= 100 {
		return fmt.Sprintf("%.0fms", milliseconds)
	}
	if milliseconds >= 1 {
		return fmt.Sprintf("%.1fms", milliseconds)
	}
	return fmt.Sprintf("%.2fms", milliseconds)
}

// printCoverage says what fraction of the lint phase this table actually accounts for.
//
// Every number in the table is true and the table implies something false about what it covers.
// The rows are rule listeners and per-file setup; the walk that offers nodes to those listeners is
// not timed by anything. A reader sums the share column, gets 100 percent, and concludes the phase
// is explained. It is not: the shares are of rule time, and rule time is a minority of the phase.
//
// Measured on the ahra tree at 02:31, load 4.92:
//
//	single-threaded    285ms rule time    1,024ms wall    the table covers ~28%
//	parallel           314ms rule time      301ms wall    the table appears to cover ~104%
//
// The parallel reading is the dangerous one, and it is the default mode. Rule time slightly
// exceeding wall clock reads as "rules are the entire phase and traversal is free." Traversal is
// not free; it parallelizes almost perfectly across files, so roughly 700ms of walking collapses
// into a few tens of milliseconds of wall clock and disappears underneath the rule time rather
// than being cheap. Only single-threaded shows the shape of the real work.
//
// So this prints the ratio rather than a traversal row. A traversal row would be wall clock minus
// attributed time, which is a number produced by subtraction rather than by measurement, and it
// would absorb scheduling, contention, and anything else unaccounted for under a label claiming to
// name one thing. Quoting an unmeasured residual as if it were measured is the failure this whole
// instrument exists to prevent. A ratio makes the gap visible and stays honest about its size
// without inventing an attribution for it.
func printCoverage(out io.Writer, attributed time.Duration, lintDuration time.Duration) {
	if lintDuration <= 0 || attributed <= 0 {
		// No phase duration means no ratio to state. Saying nothing is correct here; printing a
		// coverage claim computed from a zero would be worse than the silence it replaces.
		return
	}

	coverage := 100 * float64(attributed) / float64(lintDuration)

	if coverage > 95 {
		// Attributed time at or above wall clock means the run was parallel: listener time summed
		// across workers, against wall clock that overlapped them. The ratio is not a coverage
		// figure at all here, and printing it as one would be the exact misreading this line was
		// added to prevent.
		fmt.Fprintf(out,
			"  coverage: rule time is summed across workers, so it exceeds wall clock and is not a share of this phase. "+
				"Tree traversal is not timed by any row here. Run --single-threaded to see what the phase actually costs.\n")
		return
	}

	fmt.Fprintf(out,
		"  coverage: these rows account for %.0f%% of the %s lint phase. "+
			"The remaining %.0f%% is tree traversal, which no row here measures.\n",
		coverage,
		formatMilliseconds(lintDuration),
		100-coverage,
	)
}
