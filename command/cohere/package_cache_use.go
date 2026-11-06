package main

import "fmt"

// packageCacheUse is what the Go engine's per-package findings cache did in the lint phase, by package, as the lint
// record's `cache` object says it (swift/Contract.md): hit, restored from an entry with nothing run on it; missed,
// analyzed from source, of which written got an entry and unstorable held a fact no entry can hold; the facts the
// written entries left out, since nothing outside their package can name their object; and the restored facts
// skipped, whose object the package's export data does not hold. On is false under `--no-cache`. Only a Go run has
// one; a Swift run's lint record carries none, so its output is as it was.
type packageCacheUse struct {
	On           bool `json:"on"`
	Hit          int  `json:"hit"`
	Missed       int  `json:"missed"`
	Written      int  `json:"written"`
	Unstorable   int  `json:"unstorable"`
	FactsDropped int  `json:"factsDropped"`
	FactsSkipped int  `json:"factsSkipped"`
}

// line is the cache's clause of the --verbose account, after "lint: ": the three counts every run has, and the ones
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
	if cache.FactsSkipped > 0 {
		line += fmt.Sprintf(", %d facts skipped", cache.FactsSkipped)
	}
	return line
}
