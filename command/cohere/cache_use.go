package main

import "strings"

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
