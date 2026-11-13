package main

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// renderTable is the pair's table in markdown, the shape a pair is posted in, from the header and the records alone,
// so what it prints is what runs.jsonl holds: the binaries and the host, what every run of each corpus agreed on, each
// corpus's wall seconds by mode with the medians and each round's delta, the medians of user seconds, system seconds
// and peak resident memory, and the medians of what each extra build restored and checked, the branch's builds beside
// base's.
func renderTable(header tableHeader, records []runRecord) string {
	table := &strings.Builder{}
	corpora := corporaOf(records)

	fmt.Fprintf(table, "# cohere pair: base %s against branch %s\n\n", shortCommit(header.base.commit),
		shortCommit(header.branch.commit))
	for _, binary := range []builtBinary{header.base, header.branch} {
		fmt.Fprintf(table, "- %s `%s`, binary sha256 `%s`, %s\n", binary.side, binary.commit, prefix(binary.sha256, 8),
			binary.tableLine)
	}
	fmt.Fprintf(table, "- host %s, %s/%s, %d cores, %s\n", header.host, header.goOperatingSystem,
		header.goArchitecture, header.cores, header.goVersion)
	fmt.Fprintf(table, "- %d rounds per corpus, base first in odd rounds and branch first in even ones, each side a "+
		"cache-on cold run then a `--no-cache` run, `go-engine --contract 3 --no-fix --other-builds`; one-minute load "+
		"before each run %s\n", header.rounds, describeLoads(records))
	// A corpus's name says which tree it is only alongside its commit and its files: two trees called deeafbf4 can
	// differ by tens of thousands of findings, so every number below compares only with a pair over the same commits.
	corpusLines := []string{}
	for _, corpusName := range corpora {
		ofCorpus := recordsOf(records, corpusName, "", "")
		corpusLines = append(corpusLines, fmt.Sprintf("%s at `%s` (files in scope %s)", corpusName,
			shortCommit(ofCorpus[0].CorpusCommit),
			tally(ofCorpus, func(record runRecord) string { return describeCount(record.FilesInScope) })))
	}
	fmt.Fprintf(table, "- corpora: %s; a number here compares only with a pair over these same commits\n",
		strings.Join(corpusLines, ", "))

	table.WriteString("\n## Checked in all\n\n")
	for _, corpusName := range corpora {
		ofCorpus := recordsOf(records, corpusName, "", "")
		fmt.Fprintf(table, "- **%s** at `%s`, %d runs: exit %s; files in scope %s; findings %s, md5 %s; "+
			"standardLibraryFromBinary %s; buildsAtOnce base %s, branch %s; stderr bytes %s; "+
			"tracked files changed %s\n",
			corpusName, shortCommit(ofCorpus[0].CorpusCommit), len(ofCorpus),
			tally(ofCorpus, func(record runRecord) string { return fmt.Sprint(record.ExitCode) }),
			tally(ofCorpus, func(record runRecord) string { return describeCount(record.FilesInScope) }),
			tally(ofCorpus, func(record runRecord) string { return fmt.Sprint(record.Findings) }),
			tally(ofCorpus, func(record runRecord) string { return "`" + prefix(record.FindingsMd5, 8) + "`" }),
			tally(ofCorpus, func(record runRecord) string { return describeCount(record.StandardLibraryFromBinary) }),
			tally(recordsOf(records, corpusName, sideBase, ""), func(record runRecord) string {
				return fmt.Sprint(record.BuildsAtOnce)
			}),
			tally(recordsOf(records, corpusName, sideBranch, ""), func(record runRecord) string {
				return fmt.Sprint(record.BuildsAtOnce)
			}),
			tally(ofCorpus, func(record runRecord) string { return fmt.Sprint(record.StandardErrorBytes) }),
			tally(ofCorpus, func(record runRecord) string { return fmt.Sprint(len(record.ChangedFiles)) }))
	}

	fmt.Fprintf(table, "\n## Tracked files a run changed, each %s\n\n", changedFileOutcome)
	changedLines := changedFileLines(records)
	if len(changedLines) == 0 {
		table.WriteString("None.\n")
	}
	for _, line := range changedLines {
		table.WriteString(line)
	}

	table.WriteString("\n## Wall seconds\n\n")
	table.WriteString("| corpus | mode | base | branch | base median | branch median | median delta | per round |\n")
	table.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, corpusName := range corpora {
		for _, mode := range []string{modeCache, modeNoCache} {
			base := wallSeconds(recordsOf(records, corpusName, sideBase, mode))
			branch := wallSeconds(recordsOf(records, corpusName, sideBranch, mode))
			if len(base) == 0 && len(branch) == 0 {
				continue
			}
			fmt.Fprintf(table, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", corpusName, mode, joinSeconds(base),
				joinSeconds(branch), describeMedian(base), describeMedian(branch), describeDelta(base, branch),
				describeRoundDeltas(recordsOf(records, corpusName, sideBase, mode),
					recordsOf(records, corpusName, sideBranch, mode)))
		}
	}

	table.WriteString("\n## User seconds, system seconds and peak resident GB, medians\n\n")
	table.WriteString("| corpus | mode | base user s | branch user s | base sys s | branch sys s | base peak GB | " +
		"branch peak GB |\n")
	table.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, corpusName := range corpora {
		for _, mode := range []string{modeCache, modeNoCache} {
			base := recordsOf(records, corpusName, sideBase, mode)
			branch := recordsOf(records, corpusName, sideBranch, mode)
			if len(base) == 0 && len(branch) == 0 {
				continue
			}
			fmt.Fprintf(table, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", corpusName, mode,
				describeMedian(userSeconds(base)), describeMedian(userSeconds(branch)),
				describeMedian(systemSeconds(base)), describeMedian(systemSeconds(branch)),
				describeMedian(peakGigabytes(base)), describeMedian(peakGigabytes(branch)))
		}
	}

	table.WriteString("\n## Extra builds, medians, base / branch\n\n")
	table.WriteString("| corpus | mode | build | checkedFromSource | restoredFromHost | declinedAtLoad |\n")
	table.WriteString("|---|---|---|---|---|---|\n")
	for _, corpusName := range corpora {
		for _, mode := range []string{modeCache, modeNoCache} {
			base := recordsOf(records, corpusName, sideBase, mode)
			branch := recordsOf(records, corpusName, sideBranch, mode)
			for _, target := range extraBuildTargets(branch) {
				fmt.Fprintf(table, "| %s | %s | %s | %s | %s | %s |\n", corpusName, mode, target,
					describeBuildCounts(base, branch, target, func(build extraBuild) int {
						return build.CheckedFromSource
					}),
					describeBuildCounts(base, branch, target, func(build extraBuild) int {
						return build.RestoredFromHost
					}),
					describeBuildCounts(base, branch, target, func(build extraBuild) int {
						return build.DeclinedAtLoad
					}))
			}
		}
	}
	return table.String()
}

// corporaOf is every corpus the records hold, in the order they were measured.
func corporaOf(records []runRecord) []string {
	corpora := []string{}
	for _, record := range records {
		if !slices.Contains(corpora, record.Corpus) {
			corpora = append(corpora, record.Corpus)
		}
	}
	return corpora
}

// recordsOf is the records of one corpus, and of one side and one mode when they are not empty, in round order.
func recordsOf(records []runRecord, corpusName string, side string, mode string) []runRecord {
	selected := []runRecord{}
	for _, record := range records {
		if record.Corpus == corpusName && (side == "" || record.Side == side) && (mode == "" || record.Mode == mode) {
			selected = append(selected, record)
		}
	}
	slices.SortStableFunc(selected, func(first runRecord, second runRecord) int { return first.Round - second.Round })
	return selected
}

// tally says what records agree on: "1 in all", or each value with how many records have it, in the order first
// seen, when they differ.
func tally(records []runRecord, valueOf func(runRecord) string) string {
	values := []string{}
	counts := map[string]int{}
	for _, record := range records {
		value := valueOf(record)
		if counts[value] == 0 {
			values = append(values, value)
		}
		counts[value]++
	}
	if len(values) == 1 {
		return values[0] + " in all"
	}
	described := make([]string, 0, len(values))
	for _, value := range values {
		described = append(described, fmt.Sprintf("%s (%d)", value, counts[value]))
	}
	return strings.Join(described, ", ")
}

// changedFileLines is a markdown list item for every tracked file each run changed, in the order the runs ran, as
// "deeafbf4 base-1-cache: go.work.sum": the corpus, then the side, round and mode of the run, so a change is never
// only a count.
func changedFileLines(records []runRecord) []string {
	lines := []string{}
	for _, record := range records {
		for _, changedPath := range record.ChangedFiles {
			lines = append(lines, fmt.Sprintf("- %s %s-%d-%s: %s\n", record.Corpus, record.Side, record.Round,
				record.Mode, changedPath))
		}
	}
	return lines
}

// describeLoads is the range of the one-minute load averages read before the runs.
func describeLoads(records []runRecord) string {
	loads := []float64{}
	for _, record := range records {
		if record.LoadBefore != nil {
			loads = append(loads, *record.LoadBefore)
		}
	}
	if len(loads) == 0 {
		return "unknown"
	}
	described := fmt.Sprintf("%.2f to %.2f", slices.Min(loads), slices.Max(loads))
	if len(loads) < len(records) {
		described += fmt.Sprintf(", unread before %d runs", len(records)-len(loads))
	}
	return described
}

// wallSeconds, userSeconds, systemSeconds and peakGigabytes are a measure of each record, in the records' order.
func wallSeconds(records []runRecord) []float64 {
	return measureOf(records, func(record runRecord) float64 { return record.WallSeconds })
}

func userSeconds(records []runRecord) []float64 {
	return measureOf(records, func(record runRecord) float64 { return record.UserSeconds })
}

func systemSeconds(records []runRecord) []float64 {
	return measureOf(records, func(record runRecord) float64 { return record.SystemSeconds })
}

func peakGigabytes(records []runRecord) []float64 {
	return measureOf(records, func(record runRecord) float64 { return float64(record.PeakResidentBytes) / 1e9 })
}

func measureOf(records []runRecord, valueOf func(runRecord) float64) []float64 {
	values := make([]float64, 0, len(records))
	for _, record := range records {
		values = append(values, valueOf(record))
	}
	return values
}

// median is the middle of values, the mean of the two middles of an even count, and NaN of none.
func median(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}
	return sorted[middle]
}

// joinSeconds is values to two places, in order.
func joinSeconds(values []float64) string {
	if len(values) == 0 {
		return "none"
	}
	described := make([]string, 0, len(values))
	for _, value := range values {
		described = append(described, fmt.Sprintf("%.2f", value))
	}
	return strings.Join(described, ", ")
}

// describeMedian is values' median to two places.
func describeMedian(values []float64) string {
	if len(values) == 0 {
		return "none"
	}
	return fmt.Sprintf("%.2f", median(values))
}

// describeDelta is the branch's median less base's, in seconds and as a percent of base's.
func describeDelta(base []float64, branch []float64) string {
	if len(base) == 0 || len(branch) == 0 {
		return "none"
	}
	delta := median(branch) - median(base)
	if median(base) == 0 {
		return fmt.Sprintf("%+.2f s", delta)
	}
	return fmt.Sprintf("%+.2f s (%+.1f%%)", delta, delta/median(base)*100)
}

// describeRoundDeltas is each round's branch wall seconds less base's, in round order, for the rounds both ran.
func describeRoundDeltas(base []runRecord, branch []runRecord) string {
	described := []string{}
	for _, baseRecord := range base {
		for _, branchRecord := range branch {
			if branchRecord.Round == baseRecord.Round {
				described = append(described, fmt.Sprintf("%+.2f", branchRecord.WallSeconds-baseRecord.WallSeconds))
			}
		}
	}
	if len(described) == 0 {
		return "none"
	}
	return strings.Join(described, ", ")
}

// extraBuildsOf is a record's builds other than the host's, none when its lint record carried no builds or they do
// not read as builds.
func extraBuildsOf(record runRecord) []extraBuild {
	builds := []extraBuild{}
	if len(record.Builds) == 0 || json.Unmarshal(record.Builds, &builds) != nil {
		return nil
	}
	extra := []extraBuild{}
	for _, build := range builds {
		if !build.Host {
			extra = append(extra, build)
		}
	}
	return extra
}

// extraBuildTargets is every extra build the records name, in the order first named.
func extraBuildTargets(records []runRecord) []string {
	targets := []string{}
	for _, record := range records {
		for _, build := range extraBuildsOf(record) {
			if !slices.Contains(targets, build.Target) {
				targets = append(targets, build.Target)
			}
		}
	}
	return targets
}

// describeBuildCounts is the median of one count of the build target across base's records and across the branch's,
// "base / branch", with "absent" for a side none of whose records names the build.
func describeBuildCounts(
	base []runRecord,
	branch []runRecord,
	target string,
	countOf func(extraBuild) int,
) string {
	described := []string{}
	for _, records := range [][]runRecord{base, branch} {
		counts := []float64{}
		for _, record := range records {
			for _, build := range extraBuildsOf(record) {
				if build.Target == target {
					counts = append(counts, float64(countOf(build)))
				}
			}
		}
		if len(counts) == 0 {
			described = append(described, "absent")
			continue
		}
		described = append(described, fmt.Sprint(median(counts)))
	}
	return strings.Join(described, " / ")
}

// prefix is the first length characters of text, or all of it when it is shorter.
func prefix(text string, length int) string {
	if len(text) <= length {
		return text
	}
	return text[:length]
}
