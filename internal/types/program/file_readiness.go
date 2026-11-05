package program

import (
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// fileReadiness accumulates one file's record while the dispatcher walks it.
type fileReadiness struct {
	counts      map[string]int32
	skipped     []string
	measureOnly map[string]bool
}

// count adds a finding of ruleName, when it is a set rule.
func (readiness *fileReadiness) count(ruleName string) {
	if readiness == nil {
		return
	}
	if current, measured := readiness.counts[ruleName]; measured {
		readiness.counts[ruleName] = current + 1
	}
}

// hidden is whether ruleName's findings and notes stay out of the run: a rule running measure-only.
func (readiness *fileReadiness) hidden(ruleName string) bool {
	return readiness != nil && readiness.measureOnly[ruleName]
}

// note keeps a set rule's skip. A skip covered by another check that ran leaves nothing unchecked, so it
// is no gap here either.
func (readiness *fileReadiness) note(ruleName string, key string) {
	if readiness == nil {
		return
	}
	if _, measured := readiness.counts[ruleName]; measured && strings.HasPrefix(key, rule.SkippedNotePrefix) {
		readiness.skipped = append(readiness.skipped, ruleName)
	}
}

// record is the file's measurement, sorted, with a pair for every set rule that ran, zeros included.
func (readiness *fileReadiness) record() *AdamicRecord {
	if readiness == nil {
		return nil
	}
	record := &AdamicRecord{Counts: make([]AdamicCount, 0, len(readiness.counts))}
	for name, findings := range readiness.counts {
		record.Counts = append(record.Counts, AdamicCount{Rule: name, Findings: findings})
	}
	sort.Slice(
		record.Counts,
		func(left, right int) bool { return record.Counts[left].Rule < record.Counts[right].Rule },
	)
	record.Skipped = sortedUnique(readiness.skipped)
	return record
}
