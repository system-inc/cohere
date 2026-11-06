package main

import (
	"fmt"
	"strings"
)

// cacheUse is what the run took from the cache.
type cacheUse struct {
	// Replayed is a run answered whole from the run cache, which ran no phase.
	Replayed bool `json:"replayed"`
	// FilesReplayed is the files whose findings came from the cache: in a run that computed the rest, those it
	// did not, and in a whole replay, every file in scope.
	FilesReplayed int `json:"filesReplayed"`
	// Off is `--no-cache`: nothing read, nothing written.
	Off bool `json:"off"`

	// MissReason is why a run the cache could have answered whole was not, and FindingsMissReason why its
	// files' findings were not replayed: nothing on record, a key whose named part moved, or files with no
	// entry or with changed bytes. Empty when there is nothing to explain (#547dhjz).
	MissReason         string `json:"missReason,omitempty"`
	FindingsMissReason string `json:"findingsMissReason,omitempty"`
}

// missLine is the --verbose line that says why the cache did not answer, empty when there is nothing to say.
func (cache cacheUse) missLine() string {
	var parts []string
	if cache.MissReason != "" {
		parts = append(parts, "not replayed whole: "+cache.MissReason)
	}
	if cache.FindingsMissReason != "" {
		parts = append(parts, "findings: "+cache.FindingsMissReason)
	}
	if len(parts) == 0 {
		return ""
	}
	return "cache: " + strings.Join(parts, "; ")
}

// packageCacheUse is what the Go engine's per-package findings cache did in the lint phase, by package, as the lint
// record's `cache` object says it (swift/Contract.md): hit, restored from an entry with nothing run on it; missed,
// analyzed from source, of which written got an entry and unstorable held a fact no entry can hold; and the facts
// the written entries left out, since nothing outside their package can name their object. On is false under
// `--no-cache`. Only a Go run has one; a Swift run's lint record carries none, so its output is as it was.
type packageCacheUse struct {
	On           bool `json:"on"`
	Hit          int  `json:"hit"`
	Missed       int  `json:"missed"`
	Written      int  `json:"written"`
	Unstorable   int  `json:"unstorable"`
	FactsDropped int  `json:"factsDropped"`
}

// line is the cache's clause of the --verbose account, after "lint: ": the three counts every run has, and the two
// that are usually zero only when they are not, so the line stays short where nothing went wrong.
func (cache packageCacheUse) line() string {
	if !cache.On {
		return "package cache off"
	}
	line := fmt.Sprintf("package cache hit %d, missed %d, wrote %d", cache.Hit, cache.Missed, cache.Written)
	if cache.Unstorable > 0 {
		line += fmt.Sprintf(", %d unstorable", cache.Unstorable)
	}
	if cache.FactsDropped > 0 {
		line += fmt.Sprintf(", %d facts dropped", cache.FactsDropped)
	}
	return line
}
