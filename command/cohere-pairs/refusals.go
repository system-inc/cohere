package main

import (
	"fmt"
	"strings"
)

// refusals names every reason the records cannot be reported, empty when they can. A run refuses when it never
// started, could not be read, was stopped, or exited with anything but 0, a clean tree, or 1, findings; when it says
// it checked no file; and when its binary read no standard library entry from its table, since that run measured
// the work a local build does. A corpus refuses when its runs do not all share one findings md5: the engine's
// findings never depend on the side's cache, so two md5s in one corpus mean a run checked something else. Stderr that
// is not empty refuses nothing, and the table shows it.
func refusals(records []runRecord) []string {
	failures := []string{}
	corpora := []string{}
	findingsByCorpus := map[string]map[string][]string{}
	for _, record := range records {
		run := plannedRun{corpus: record.Corpus, round: record.Round, side: record.Side, mode: record.Mode}.name()
		// A run that never started has no output, so it is said once rather than once for each count it lacks.
		if record.StartError != "" {
			failures = append(failures, fmt.Sprintf("%s did not start: %s", run, record.StartError))
			continue
		}
		switch {
		case record.Stopped:
			failures = append(failures, fmt.Sprintf("%s was stopped after %s", run, runDeadline))
		case record.ExitCode != 0 && record.ExitCode != 1:
			failures = append(failures, fmt.Sprintf("%s exited %d", run, record.ExitCode))
		}
		if len(record.ChangedFiles) > 0 {
			failures = append(failures, fmt.Sprintf("%s changed tracked files: %s", run,
				strings.Join(record.ChangedFiles, ", ")))
		}
		if record.OutputError != "" {
			failures = append(failures, fmt.Sprintf("%s's output could not be read: %s", run, record.OutputError))
		}
		if record.FilesInScope == nil || *record.FilesInScope == 0 {
			failures = append(failures, fmt.Sprintf("%s has filesInScope %s", run, describeCount(record.FilesInScope)))
		}
		if record.FilesWalked == nil || *record.FilesWalked == 0 {
			failures = append(failures, fmt.Sprintf("%s has filesWalked %s", run, describeCount(record.FilesWalked)))
		}
		if record.StandardLibraryFromBinary == nil || *record.StandardLibraryFromBinary == 0 {
			failures = append(failures, fmt.Sprintf("%s has standardLibraryFromBinary %s", run,
				describeCount(record.StandardLibraryFromBinary)))
		}
		if _, seen := findingsByCorpus[record.Corpus]; !seen {
			corpora = append(corpora, record.Corpus)
			findingsByCorpus[record.Corpus] = map[string][]string{}
		}
		findingsByCorpus[record.Corpus][record.FindingsMd5] = append(
			findingsByCorpus[record.Corpus][record.FindingsMd5],
			run,
		)
	}
	for _, corpusName := range corpora {
		byMd5 := findingsByCorpus[corpusName]
		if len(byMd5) < 2 {
			continue
		}
		described := []string{}
		for _, record := range records {
			if runs, unsaid := byMd5[record.FindingsMd5]; record.Corpus == corpusName && unsaid {
				described = append(described, fmt.Sprintf("%s in %s", record.FindingsMd5, strings.Join(runs, ", ")))
				delete(byMd5, record.FindingsMd5)
			}
		}
		failures = append(failures, fmt.Sprintf("%s's runs do not share one findings md5: %s", corpusName,
			strings.Join(described, "; ")))
	}
	return failures
}

// describeCount is a count a run's output may not have written, as a refusal says it.
func describeCount(count *int) string {
	if count == nil {
		return "missing"
	}
	return fmt.Sprint(*count)
}
