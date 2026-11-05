package program

import (
	"crypto/sha256"
	"sync"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// programFingerprintMemo is one walk's program fingerprints, one for each rule and its options, shared by every
// worker. A fingerprint depends on the rule and its options, never on the worker or the selection asking, and
// computing one per selection on each worker was 330 computations on an ahra edit run instead of 6, about
// 1.85 GB and most of its 24M mallocs (#f96cnry's profile).
type programFingerprintMemo struct {
	mutex   sync.Mutex
	entries map[programFingerprintKey]*programFingerprintEntry
}

// fingerprint is subject's program fingerprint under the options it is bound to, computed once for the walk
// through a Program viewed under the rule's own reads. optionsKey names the options; see
// ruleSelection.optionsKeys. The second return is false when the fingerprint panicked, every time it is asked.
func (memo *programFingerprintMemo) fingerprint(
	graph *Graph,
	subject rule.Bound,
	optionsKey string,
) ([sha256.Size]byte, bool) {
	key := programFingerprintKey{rule: subject.Name, options: optionsKey}
	memo.mutex.Lock()
	entry, found := memo.entries[key]
	if !found {
		entry = &programFingerprintEntry{}
		memo.entries[key] = entry
	}
	memo.mutex.Unlock()

	entry.once.Do(func() {
		defer func() {
			if recover() != nil {
				entry.usable = false
			}
		}()
		entry.fingerprint = subject.ProgramFingerprint(rule.ViewProgram(graph.Program, nil, subject.Traits))
		entry.usable = true
	})
	return entry.fingerprint, entry.usable
}
