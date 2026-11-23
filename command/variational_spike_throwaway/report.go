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

// timingReport is every phase apart. Decode and merge are the importer's own count of its work inside the phase that
// imported, wall time only.
type timingReport struct {
	ListingGraph             phaseTiming   `json:"listingGraph"`
	ListingExports           phaseTiming   `json:"listingExports"`
	Parse                    phaseTiming   `json:"parse"`
	VariationalImport        phaseTiming   `json:"variationalImport"`
	VariationalDecodeSeconds float64       `json:"variationalDecodeSeconds"`
	VariationalMergeSeconds  float64       `json:"variationalMergeSeconds"`
	VariationalCheck         phaseTiming   `json:"variationalCheck"`
	HostPlainImport          phaseTiming   `json:"hostPlainImport"`
	HostPlainCheck           phaseTiming   `json:"hostPlainCheck"`
	EveryBuildPlainImport    phaseTiming   `json:"everyBuildPlainImport"`
	EveryBuildPlainCheck     phaseTiming   `json:"everyBuildPlainCheck"`
	PlainCheckByBuild        []phaseTiming `json:"plainCheckByBuild"`
	HostPlainDecodeSeconds   float64       `json:"hostPlainDecodeSeconds"`
	PlainDecodeSeconds       float64       `json:"plainDecodeSeconds"`
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
		"children, counted apart. Every check runs on one goroutine.",
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
	}{
		{"listing: graph, nothing compiled", timing.ListingGraph},
		{"listing: export data, not ours", timing.ListingExports},
		{"parse (ours, once)", timing.Parse},
		{"variational import", timing.VariationalImport},
		{"variational check", timing.VariationalCheck},
		{"baseline: host plain import", timing.HostPlainImport},
		{"baseline: host plain check", timing.HostPlainCheck},
		{"baseline: every build plain import", timing.EveryBuildPlainImport},
		{"baseline: every build plain check", timing.EveryBuildPlainCheck},
	}
	for _, row := range rows {
		fmt.Fprintf(output, "%-34s %9.3f %9.3f %9.3f\n", row.name, row.phase.WallSeconds, row.phase.CPUSeconds(),
			row.phase.ChildrenCPUSeconds)
	}
	fmt.Fprintf(output, "importer (wall): variational decode %.3fs, merge %.3fs; host plain decode %.3fs; every build "+
		"plain decode %.3fs\n", timing.VariationalDecodeSeconds, timing.VariationalMergeSeconds,
		timing.HostPlainDecodeSeconds, timing.PlainDecodeSeconds)
	if timing.HostPlainCheck.CPUSeconds() > 0 && timing.EveryBuildPlainCheck.CPUSeconds() > 0 {
		fmt.Fprintf(output, "variational check cpu over host plain check: %.2fx; over every build summed: %.2fx\n",
			timing.VariationalCheck.CPUSeconds()/timing.HostPlainCheck.CPUSeconds(),
			timing.VariationalCheck.CPUSeconds()/timing.EveryBuildPlainCheck.CPUSeconds())
	}

	if report.Profile != nil {
		writeProfile(output, report.Profile)
	}

	errors := report.Errors
	fmt.Fprintf(output, "\nerrors: variational check %d, plain checks %d\n", errors.Variational, errors.Plain)
	shortfalls := report.Shortfalls
	fmt.Fprintf(output, "merge shortfalls: %d kind conflicts, %d unbound signatures, %d stray type parameters, "+
		"%d generic alias variants\n", shortfalls.KindConflicts, shortfalls.UnboundSignatures,
		shortfalls.StrayTypeParameters, shortfalls.GenericAliasVariants)
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
	fmt.Fprintf(output, "%-12s %10s %10s %10s %12s %14s %10s\n", "check", "median s", "least s", "mean s",
		"all gc cpu s", "of it idle s", "median MiB")
	for _, row := range []struct {
		name    string
		summary repeatSummary
	}{{"variational", variational}, {"host plain", hostPlain}} {
		fmt.Fprintf(output, "%-12s %10.4f %10.4f %10.4f %12.3f %14.3f %10.1f\n", row.name, row.summary.medianCPU,
			row.summary.leastCPU, row.summary.meanCPU, row.summary.collectorCPU, row.summary.collectorIdleCPU,
			row.summary.medianAllocatedBytes/(1<<20))
	}
	if hostPlain.medianCPU > 0 && hostPlain.medianAllocatedBytes > 0 {
		fmt.Fprintf(output, "variational over host plain: %.2fx median cpu, %.2fx least, %.2fx mean, %.2fx allocation\n",
			variational.medianCPU/hostPlain.medianCPU, variational.leastCPU/hostPlain.leastCPU,
			variational.meanCPU/hostPlain.meanCPU, variational.medianAllocatedBytes/hostPlain.medianAllocatedBytes)
	}
	for _, row := range []struct {
		name    string
		repeats []profileRepeat
	}{{"variational", profiled.Variational}, {"host plain", profiled.HostPlain}} {
		fmt.Fprintf(output, "%s cpu s by repeat:", row.name)
		for _, repeat := range row.repeats {
			fmt.Fprintf(output, " %.3f", repeat.Check.CPUSeconds())
		}
		fmt.Fprintf(output, "\n")
	}
}
