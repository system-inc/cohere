package program

// AdamicRecord is one file's Adamic readiness measurement, kept per rule so an entry whose type-aware rules
// ran again can be merged rule by rule (refreshClasses), as its findings are.
//
// Measure-only findings are counts, never LintCacheFindings: they cannot be printed, cannot fail a run and
// carry no fixes, so nothing about them needs replaying beyond how many there were.
type AdamicRecord struct {
	// Counts is every cacheable cohere:adamic rule that ran on the file with how many findings it had at the
	// set's own options, before suppression, sorted by rule. A rule that found nothing has a zero pair, so a
	// rule that ran clean and one that did not run can be told apart.
	Counts []AdamicCount

	// Skipped is the cacheable cohere:adamic rules that skipped the file, sorted. A skip leaves the file
	// unmeasured, not ready.
	Skipped []string
}

// Findings is the record's total. AdamicRecord itself is the findings cache's, in lint_cache.go.
func (record *AdamicRecord) Findings() int {
	total := 0
	for _, count := range record.Counts {
		total += int(count.Findings)
	}
	return total
}

// Measured is whether the record can say ready or not: a file has one, and no set rule skipped it. A file
// with none was replayed from an entry its run never measured, which the findings cache refuses to a run that
// measures (FindingsReuse.MeasureReadiness), so it reads unmeasured rather than as clean.
func (record *AdamicRecord) Measured() bool {
	return record != nil && len(record.Skipped) == 0
}
