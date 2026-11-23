package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/variational_types/merged"
)

// spikeReport is the whole run, written to --json.
type spikeReport struct {
	Root      string         `json:"root"`
	Patterns  []string       `json:"patterns"`
	Workspace string         `json:"workspace"`
	Builds    []buildListing `json:"builds"`
	// Ours is every package the patterns matched, in the order they were checked.
	Ours   []ourReport  `json:"ours"`
	Totals oursTotals   `json:"totals"`
	Timing timingReport `json:"timing"`
	Proof  []buildProof `json:"proof"`
	Errors checkErrors  `json:"errors"`
	// Profile is the profiled repeats of each check, when --cpu-profile asked for them.
	Profile *profileReport `json:"profile,omitempty"`
	// Schedule is the importer replayed on the engine's schedule, when --schedule-workers asked for it.
	Schedule *scheduleReport `json:"schedule,omitempty"`
	// Parallel is ours checked in parallel on the engine's schedule, when --check-workers asked for it.
	Parallel *parallelReport `json:"parallel,omitempty"`
	// Shortfalls are what the merging importer could not keep exactly.
	Shortfalls merged.Shortfalls `json:"shortfalls"`
	Planted    *differenceAtKey  `json:"planted,omitempty"`
	// PeakResidentBytes is the most memory the process held resident over the whole run, the oracle's loads included.
	PeakResidentBytes int64    `json:"peakResidentBytes"`
	Notes             []string `json:"notes"`
}

// buildListing is one build as go list gave it.
type buildListing struct {
	Bit        int    `json:"bit"`
	Target     string `json:"target"`
	CgoEnabled bool   `json:"cgoEnabled"`
	CgoChoice  string `json:"cgoChoice"`
	Packages   int    `json:"packages"`
	Matched    int    `json:"matched"`
	// Exported counts the packages whose export data the second go list gave: every package not ours, but unsafe.
	Exported int `json:"exported"`
}

// ourReport is one of ours after the variational check.
type ourReport struct {
	Path              string `json:"path"`
	Files             int    `json:"files"`
	FilesInEveryBuild int    `json:"filesInEveryBuild"`
	Builds            int    `json:"builds"`
	Units             int    `json:"units"`
	Restarts          int    `json:"restarts"`
	Expressions       int    `json:"expressions"`
	ForkedExpressions int    `json:"forkedExpressions"`
}

// oursTotals sums ourReport over ours.
type oursTotals struct {
	Packages          int `json:"packages"`
	Files             int `json:"files"`
	FilesInEveryBuild int `json:"filesInEveryBuild"`
	Units             int `json:"units"`
	Restarts          int `json:"restarts"`
	Expressions       int `json:"expressions"`
	ForkedExpressions int `json:"forkedExpressions"`
}

// timingReport is every phase apart. Decode, decode wait and merge are the importer's own count of its work inside
// the phases that import and check: decode is the builds' reading summed over the builds, decode wait the merge's wall
// time waiting on it, the builds one object needs read at once, and merge the merge's wall time less that wait.
type timingReport struct {
	ListingGraph                 phaseTiming `json:"listingGraph"`
	ListingExports               phaseTiming `json:"listingExports"`
	Parse                        phaseTiming `json:"parse"`
	VariationalImport            phaseTiming `json:"variationalImport"`
	VariationalDecodeSeconds     float64     `json:"variationalDecodeSeconds"`
	VariationalDecodeWaitSeconds float64     `json:"variationalDecodeWaitSeconds"`
	VariationalMergeSeconds      float64     `json:"variationalMergeSeconds"`
	VariationalCheck             phaseTiming `json:"variationalCheck"`
	// VariationalOnSchedule is whether the variational import and check ran on the parallel schedule, when their rows
	// sum each package's wall alone and hold no CPU: the process's CPU over one job is every running job's, so CPU is
	// measured across the whole schedule, in the parallel report.
	VariationalOnSchedule  bool          `json:"variationalOnSchedule"`
	HostPlainImport        phaseTiming   `json:"hostPlainImport"`
	HostPlainCheck         phaseTiming   `json:"hostPlainCheck"`
	EveryBuildPlainImport  phaseTiming   `json:"everyBuildPlainImport"`
	EveryBuildPlainCheck   phaseTiming   `json:"everyBuildPlainCheck"`
	PlainCheckByBuild      []phaseTiming `json:"plainCheckByBuild"`
	HostPlainDecodeSeconds float64       `json:"hostPlainDecodeSeconds"`
	PlainDecodeSeconds     float64       `json:"plainDecodeSeconds"`
}

// parallelReport is ours checked in parallel along the import graph on the engine's schedule, one types.Checker a
// package: the variational check the proof projects, and the host's plain check, each on its own run of the schedule.
type parallelReport struct {
	Workers     int         `json:"workers"`
	Variational parallelRun `json:"variational"`
	HostPlain   parallelRun `json:"hostPlain"`
	// HostPlainWarmSeconds is the wall of reading whole, before the host plain schedule and outside its timing, every
	// package the schedule imports, so the schedule reads only decoded types.
	HostPlainWarmSeconds float64 `json:"hostPlainWarmSeconds"`
	// HostPlainDecodeSeconds is the parallel host plain importer's decode, each package read whole at its import, all
	// of it in the warm-up, where the serial baseline's host plain decode reads only what its checks look up.
	HostPlainDecodeSeconds float64 `json:"hostPlainDecodeSeconds"`
}

// checkErrors is every check's errors, which a sound run has none of.
type checkErrors struct {
	Variational      int      `json:"variational"`
	VariationalFirst []string `json:"variationalFirst,omitempty"`
	Plain            int      `json:"plain"`
	PlainFirst       []string `json:"plainFirst,omitempty"`
}

// buildProof is the projection to one build against the oracle's plain answers. Compared counts the expressions both
// hold, Matched and Mismatched split it, and each side's own expressions are counted apart, as are the ambiguous ones,
// more than one of whose answers holds the build.
type buildProof struct {
	Target              string       `json:"target"`
	Compared            int          `json:"compared"`
	Matched             int          `json:"matched"`
	Mismatched          int          `json:"mismatched"`
	OnlyProjected       int          `json:"onlyProjected"`
	OnlyPlain           int          `json:"onlyPlain"`
	Ambiguous           int          `json:"ambiguous"`
	Green               bool         `json:"green"`
	OracleDuplicates    int          `json:"oracleDuplicates"`
	NoPositionPlain     int          `json:"noPositionPlain"`
	NoPositionProjected int          `json:"noPositionProjected"`
	OraclePackageErrors int          `json:"oraclePackageErrors"`
	OracleFirstErrors   []string     `json:"oracleFirstErrors,omitempty"`
	OracleLoad          phaseTiming  `json:"oracleLoad"`
	FirstMismatches     []difference `json:"firstMismatches"`
	FirstOneSided       []difference `json:"firstOneSided"`
	FirstAmbiguous      []difference `json:"firstAmbiguous"`
}

// difference is one expression the projection and the oracle don't agree on.
type difference struct {
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Node       string `json:"node"`
	Expression string `json:"expression"`
	Projected  string `json:"projected"`
	Plain      string `json:"plain"`
}

// sortKey orders differences by file and offsets.
func (spelled difference) sortKey() string {
	return fmt.Sprintf("%s\x00%012d\x00%012d\x00%s\x00%s", spelled.File, spelled.Start, spelled.End, spelled.Node,
		spelled.Kind)
}

// differenceAtKey is where --plant-mismatch planted its mismatch.
type differenceAtKey struct {
	Build int    `json:"build"`
	File  string `json:"file"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// spikeNotes say what the spike measured and how, wherever a number is a choice.
var spikeNotes = []string{
	"Bit b of a mask is build b, the host first and each --build after in the order given.",
	"Ours are the packages any build's patterns matched; a file's mask is the builds whose go list compiles it.",
	"An expression is keyed by file, start and end offsets, and its syntax node's kind; its answer is the census's " +
		"spelling of its type and exact constant value.",
	"Listing is two go list runs a build: the graph with -deps -compiled, which compiles nothing, then -export " +
		"naming only the packages not ours, so ours are compiled only where a package not ours imports one.",
	"The plain baselines read ours as they checked them, from source, in dependency order, and everything else " +
		"from export data.",
	"The variational check imports everything not ours before each package is checked, so the import phase is " +
		"timed apart from the check; the plain baselines do the same with each build's own export data.",
	"The baselines import through a fresh merging importer's Plain, since an importer's Plain shares each build's " +
		"decoded packages with its merge: each run pays its own decode.",
	"The variational check is given an Info without a Types map, since Variation.Types is its answer; each plain " +
		"check records into a Types map, as it must to record at all.",
	"CPU is the process's own user and system time from getrusage around each phase; listing's go list runs are " +
		"children, counted apart. Every check runs on one goroutine, unless --check-workers runs them in parallel.",
	"With --check-workers, the variational check runs on the engine's schedule, each of ours once every one of " +
		"ours it imports is checked, and its import and check rows sum each package's wall alone; the schedule's " +
		"wall is the real critical path, and the process's CPU is measured across the whole schedule. The host's " +
		"plain check runs the same way beside the serial baselines, through a fresh importer that reads each " +
		"package whole at its import, so its checks read only decoded types.",
	"With --check-workers, every package the host plain schedule imports is read whole before it, untimed by it " +
		"and reported apart as the warm-up, so the plain wall holds no decode, while the variational wall holds " +
		"its own lazy decode and merge. The walk's measured cost over the plain check is so an upper bound.",
	"The oracle loads each build with go/packages, every package checked from source with go/types, as the " +
		"platform census does; its load time is not part of any price.",
}

// tableText is the run as a table a reader can take in at once.
func tableText(report *spikeReport) string {
	output := &strings.Builder{}
	fmt.Fprintf(output, "variational spike over %s, patterns %s\n", report.Root, strings.Join(report.Patterns, " "))
	fmt.Fprintf(output, "workspace: %s\n", report.Workspace)
	for _, build := range report.Builds {
		fmt.Fprintf(output, "  bit %d  %-40s %5d packages listed, %d matched, %d exported (cgo: %s)\n", build.Bit,
			build.Target, build.Packages, build.Matched, build.Exported, build.CgoChoice)
	}
	totals := report.Totals
	fmt.Fprintf(output, "\nours: %d packages, %d files (%d in every build), %d units, %d restarts, %d expressions, "+
		"%d with more than one answer\n", totals.Packages, totals.Files, totals.FilesInEveryBuild, totals.Units,
		totals.Restarts, totals.Expressions, totals.ForkedExpressions)
	forked := make([]ourReport, 0, len(report.Ours))
	for _, our := range report.Ours {
		if our.Restarts != 0 || our.ForkedExpressions != 0 {
			forked = append(forked, our)
		}
	}
	sort.SliceStable(forked, func(left int, right int) bool { return forked[left].Restarts > forked[right].Restarts })
	if len(forked) > 10 {
		forked = forked[:10]
	}
	for _, our := range forked {
		fmt.Fprintf(output, "  %-70s files %3d/%-3d units %5d restarts %4d forked %5d of %d\n", our.Path,
			our.FilesInEveryBuild, our.Files, our.Units, our.Restarts, our.ForkedExpressions, our.Expressions)
	}

	timing := report.Timing
	fmt.Fprintf(output, "\n%-34s %9s %9s %9s\n", "phase", "wall s", "cpu s", "child s")
	rows := []struct {
		name  string
		phase phaseTiming
		// onSchedule rows ran on the parallel schedule, so their CPU isn't measured by phase.
		onSchedule bool
	}{
		{"listing: graph, nothing compiled", timing.ListingGraph, false},
		{"listing: export data, not ours", timing.ListingExports, false},
		{"parse (ours, once)", timing.Parse, false},
		{"variational import", timing.VariationalImport, timing.VariationalOnSchedule},
		{"variational check", timing.VariationalCheck, timing.VariationalOnSchedule},
		{"baseline: host plain import", timing.HostPlainImport, false},
		{"baseline: host plain check", timing.HostPlainCheck, false},
		{"baseline: every build plain import", timing.EveryBuildPlainImport, false},
		{"baseline: every build plain check", timing.EveryBuildPlainCheck, false},
	}
	for _, row := range rows {
		if row.onSchedule {
			fmt.Fprintf(output, "%-34s %9.3f %9s %9s\n", row.name, row.phase.WallSeconds, "n/a", "n/a")
			continue
		}
		fmt.Fprintf(output, "%-34s %9.3f %9.3f %9.3f\n", row.name, row.phase.WallSeconds, row.phase.CpuSeconds(),
			row.phase.ChildrenCpuSeconds)
	}
	if timing.VariationalOnSchedule {
		fmt.Fprintf(output, "the variational rows ran on the parallel schedule: their wall sums each package's alone, "+
			"and their cpu, not measured by phase, is the schedule's process cpu in the parallel table below\n")
	}
	fmt.Fprintf(output, "importer: variational decode %.3fs summed over builds, %.3fs waited on, merge %.3fs wall; "+
		"host plain decode %.3fs; every build plain decode %.3fs\n", timing.VariationalDecodeSeconds,
		timing.VariationalDecodeWaitSeconds, timing.VariationalMergeSeconds, timing.HostPlainDecodeSeconds,
		timing.PlainDecodeSeconds)
	if !timing.VariationalOnSchedule && timing.VariationalCheck.CpuSeconds() > 0 &&
		timing.HostPlainCheck.CpuSeconds() > 0 && timing.EveryBuildPlainCheck.CpuSeconds() > 0 {
		fmt.Fprintf(output, "variational check cpu over host plain check: %.2fx; over every build summed: %.2fx\n",
			timing.VariationalCheck.CpuSeconds()/timing.HostPlainCheck.CpuSeconds(),
			timing.VariationalCheck.CpuSeconds()/timing.EveryBuildPlainCheck.CpuSeconds())
	}

	if report.Profile != nil {
		writeProfile(output, report.Profile)
	}
	if report.Schedule != nil {
		writeSchedule(output, report.Schedule)
	}
	if report.Parallel != nil {
		writeParallel(output, report.Parallel)
	}

	errors := report.Errors
	fmt.Fprintf(output, "\nerrors: variational check %d, plain checks %d\n", errors.Variational, errors.Plain)
	shortfalls := report.Shortfalls
	fmt.Fprintf(output, "merge shortfalls: %d kind conflicts, %d unbound signatures, %d stray type parameters, "+
		"%d generic alias variants, %d merged type sets\n", shortfalls.KindConflicts, shortfalls.UnboundSignatures,
		shortfalls.StrayTypeParameters, shortfalls.GenericAliasVariants, shortfalls.MergedTypeSets)
	for _, spelled := range errors.VariationalFirst {
		fmt.Fprintf(output, "  variational: %s\n", spelled)
	}
	for _, spelled := range errors.PlainFirst {
		fmt.Fprintf(output, "  plain: %s\n", spelled)
	}

	fmt.Fprintf(output, "\n%-40s %9s %9s %9s %9s %9s %9s %7s %8s\n", "proof", "compared", "matched", "mismatch",
		"proj only", "plain only", "ambiguous", "verdict", "oracle s")
	for _, proof := range report.Proof {
		verdict := "red"
		if proof.Green {
			verdict = "green"
		}
		fmt.Fprintf(output, "%-40s %9d %9d %9d %9d %9d %9d %7s %8.1f\n", proof.Target, proof.Compared, proof.Matched,
			proof.Mismatched, proof.OnlyProjected, proof.OnlyPlain, proof.Ambiguous, verdict,
			proof.OracleLoad.WallSeconds)
	}
	for _, proof := range report.Proof {
		fmt.Fprintf(output, "left out, no position: %d plain, %d projected; oracle keys held twice: %d (%s)\n",
			proof.NoPositionPlain, proof.NoPositionProjected, proof.OracleDuplicates, proof.Target)
	}
	for _, proof := range report.Proof {
		writeDifferences(output, "first mismatches in "+proof.Target, proof.FirstMismatches, differencesKept)
		writeDifferences(output, "first expressions one side holds in "+proof.Target, proof.FirstOneSided, 10)
		writeDifferences(output, "first ambiguous expressions in "+proof.Target, proof.FirstAmbiguous, 5)
	}
	fmt.Fprintf(output, "\npeak resident %.2f GiB\n", float64(report.PeakResidentBytes)/(1<<30))
	return output.String()
}

// writeDifferences writes the first limit of differences under title, and nothing when there are none.
//
// Mutates: output gets the lines.
func writeDifferences(output *strings.Builder, title string, differences []difference, limit int) {
	if len(differences) == 0 {
		return
	}
	fmt.Fprintf(output, "\n%s:\n", title)
	for index, spelled := range differences {
		if index == limit {
			fmt.Fprintf(output, "  and %d more in the json\n", len(differences)-limit)
			break
		}
		fmt.Fprintf(output, "  %s %s:%d [%d,%d) %s `%s`\n      projected %q\n      plain     %q\n", spelled.Kind,
			spelled.File, spelled.Line, spelled.Start, spelled.End, spelled.Node, spelled.Expression,
			spelled.Projected, spelled.Plain)
	}
}

// writeProfile writes the profiled repeats: each check's CPU, its collector's CPU and its allocation, then every
// repeat's CPU.
//
// Mutates: output gets the lines.
func writeProfile(output *strings.Builder, profiled *profileReport) {
	variational, hostPlain := summarize(profiled.Variational), summarize(profiled.HostPlain)
	fmt.Fprintf(output, "\nprofiled %d repeats of each check at %d Hz: %s and %s\n", profiled.Repeats,
		profiled.RateHertz, profiled.VariationalProfile, profiled.HostPlainProfile)
	fmt.Fprintf(output, "%-12s %-8s %10s %10s %10s %12s %14s %10s\n", "check", "cpu of", "median s", "least s",
		"mean s", "all gc cpu s", "of it idle s", "median MiB")
	for _, row := range []struct {
		name    string
		summary repeatSummary
	}{{"variational", variational}, {"host plain", hostPlain}} {
		fmt.Fprintf(output, "%-12s %-8s %10.4f %10.4f %10.4f %12.3f %14.3f %10.1f\n", row.name, "process",
			row.summary.medianCpu, row.summary.leastCpu, row.summary.meanCpu, row.summary.collectorCpu,
			row.summary.collectorIdleCpu, row.summary.medianAllocatedBytes/(1<<20))
		fmt.Fprintf(output, "%-12s %-8s %10.4f %10.4f %10.4f\n", "", "thread", row.summary.medianThreadCpu,
			row.summary.leastThreadCpu, row.summary.meanThreadCpu)
	}
	if hostPlain.medianCpu > 0 && hostPlain.medianThreadCpu > 0 && hostPlain.medianAllocatedBytes > 0 {
		fmt.Fprintf(output, "variational over host plain, process cpu: %.2fx median, %.2fx least, %.2fx mean; "+
			"thread cpu: %.2fx median, %.2fx least, %.2fx mean; %.2fx allocation\n",
			variational.medianCpu/hostPlain.medianCpu, variational.leastCpu/hostPlain.leastCpu,
			variational.meanCpu/hostPlain.meanCpu, variational.medianThreadCpu/hostPlain.medianThreadCpu,
			variational.leastThreadCpu/hostPlain.leastThreadCpu, variational.meanThreadCpu/hostPlain.meanThreadCpu,
			variational.medianAllocatedBytes/hostPlain.medianAllocatedBytes)
	}
	for _, row := range []struct {
		name    string
		repeats []profileRepeat
	}{{"variational", profiled.Variational}, {"host plain", profiled.HostPlain}} {
		fmt.Fprintf(output, "%s process cpu s by repeat:", row.name)
		for _, repeat := range row.repeats {
			fmt.Fprintf(output, " %.3f", repeat.Check.CpuSeconds())
		}
		fmt.Fprintf(output, "\n%s thread cpu s by repeat: ", row.name)
		for _, repeat := range row.repeats {
			fmt.Fprintf(output, " %.3f", repeat.ThreadCpuSeconds)
		}
		fmt.Fprintf(output, "\n")
	}
}

// writeSchedule writes the importer replayed on the engine's schedule and the critical path the model gives.
//
// Mutates: output gets the lines.
func writeSchedule(output *strings.Builder, schedule *scheduleReport) {
	order := "in parallel"
	if schedule.Serialized {
		order = "one lookup at a time, the importer not being safe for more"
	}
	fmt.Fprintf(output, "\nthe importer on the engine's schedule: %d workers, %d reads, %s\n", schedule.Workers,
		schedule.Reads, order)
	fmt.Fprintf(output, "%-34s %9s %9s\n", "replay", "wall s", "missed")
	for _, row := range []struct {
		name string
		run  scheduleRun
	}{
		{"variational importer", schedule.VariationalImport},
		{"host plain importer", schedule.HostPlainImport},
		{"every build's plain, side by side", schedule.EveryBuildImport},
	} {
		fmt.Fprintf(output, "%-34s %9.3f %9d\n", row.name, row.run.WallSeconds, row.run.Missed)
	}
	fmt.Fprintf(output, "critical path (model: warm checks plus measured lookups, on the import graph): variational "+
		"%.3fs, host plain %.3fs, so the walk adds %+.3fs; checks alone %.3fs and %.3fs\n", schedule.VariationalPath,
		schedule.HostPlainPath, schedule.VariationalPath-schedule.HostPlainPath, schedule.VariationalCheckOn,
		schedule.HostPlainCheckOn)
}

// writeParallel writes ours checked in parallel on the engine's schedule: each run's wall, which is its real critical
// path, the process's CPU across it and its jobs' wall summed.
//
// Mutates: output gets the lines.
func writeParallel(output *strings.Builder, parallel *parallelReport) {
	fmt.Fprintf(output, "\nours checked in parallel on the engine's schedule: %d workers, one checker a package\n",
		parallel.Workers)
	fmt.Fprintf(output, "%-34s %9s %9s %9s\n", "check", "wall s", "cpu s", "jobs s")
	for _, row := range []struct {
		name string
		run  parallelRun
	}{
		{"variational check (the proof's)", parallel.Variational},
		{"host plain check", parallel.HostPlain},
	} {
		jobs := 0.0
		for _, seconds := range row.run.JobSeconds {
			jobs += seconds
		}
		fmt.Fprintf(output, "%-34s %9.3f %9.3f %9.3f\n", row.name, row.run.WallSeconds, row.run.Process.CpuSeconds(),
			jobs)
	}
	fmt.Fprintf(
		output,
		"critical path, measured: the walk adds %+.3fs of wall over the host's plain check, an upper bound, "+
			"since the variational wall holds its own decode and the plain wall none; host plain warm-up, each "+
			"package read whole before its schedule: %.3fs wall, %.3fs decode\n",
		parallel.Variational.WallSeconds-parallel.HostPlain.WallSeconds,
		parallel.HostPlainWarmSeconds,
		parallel.HostPlainDecodeSeconds,
	)
	writeCpuRatio(output, "variational schedule", parallel.Variational, parallel.HostPlain)
}

// writeCpuRatio writes the process CPU of run over that of hostPlain, each across its whole schedule, or says it
// wasn't measured, never a ratio of nothing.
//
// Mutates: output gets the line.
func writeCpuRatio(output *strings.Builder, name string, run parallelRun, hostPlain parallelRun) {
	if run.Process.CpuSeconds() <= 0 || hostPlain.Process.CpuSeconds() <= 0 {
		fmt.Fprintf(output, "%s cpu over host plain schedule cpu: not measured, the platform gives no process cpu\n",
			name)
		return
	}
	fmt.Fprintf(output, "%s cpu over host plain schedule cpu: %.2fx\n", name,
		run.Process.CpuSeconds()/hostPlain.Process.CpuSeconds())
}
