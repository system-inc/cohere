package program

import (
	"encoding/json"
	"sort"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// Readiness is the cohere:adamic set a readiness run measures every file against (#drbrp8c).
//
// A file is Adamic-ready when the type phase reports nothing in it and no cohere:adamic rule finds anything
// in it, at the set's own options, whatever the project's chain says: Adamic will not read a disable
// comment or a departure, so readiness cannot either. So a rule the chain enables is counted as it runs, its
// findings counted before suppression, and a rule the chain leaves off or never names runs measure-only:
// its findings are counted and never reported, never fail the run, and never carry a fix to the edit engine.
//
// A rule the chain enables with options of its own is counted at the chain's options, not the set's. Today
// only ban-ts-comment can differ, and only in a chain that sets it; running it twice to measure the set's
// options is the refinement, if a project shows the difference matters.
type Readiness struct {
	// Options is each set rule's raw options, as the set writes them.
	Options map[string][]json.RawMessage
}

// Measures is whether ruleName is one of the set's.
func (readiness *Readiness) Measures(ruleName string) bool {
	if readiness == nil {
		return false
	}
	_, measured := readiness.Options[ruleName]
	return measured
}

// newFileReadiness starts a file's record over the set rules among the rules about to walk.
func newFileReadiness(readiness *Readiness, rules []rule.Entry, measureOnly map[string]bool) *fileReadiness {
	if readiness == nil {
		return nil
	}
	record := &fileReadiness{counts: map[string]int32{}, measureOnly: measureOnly}
	for _, subject := range rules {
		if readiness.Measures(subject.Name) {
			record.counts[subject.Name] = 0
		}
	}
	return record
}

func sortedUnique(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	unique := names[:1]
	for _, name := range names[1:] {
		if name != unique[len(unique)-1] {
			unique = append(unique, name)
		}
	}
	return unique
}
