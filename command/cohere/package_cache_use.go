package main

import "fmt"

// packageCacheUse is what the Go engine's per-package findings cache did in the lint phase, by package, as the lint
// record's `cache` object says it (swift/Contract.md): hit, restored from an entry with nothing run on it; missed,
// analyzed from source, of which written got an entry and unstorable held a fact no entry can hold; the facts the
// written entries left out, since nothing outside their package can name their object; the restored facts
// skipped, whose object the package's export data does not hold; and the waves the lint phase analyzed packages in, at
// most two. FromBinary is the standard-library packages restored from the entries a release binary carries, apart
// from hit, which `--no-cache` reads too, since they are part of cohere; it is absent when 0. Reloads is the packages
// the run loaded again from source, though the cache held them, to answer a question of a fact computed on demand
// that no store answered (#w7zmvat wave 3), and ReloadRounds the loads and lints again that took, and Replays the
// questions of such a fact answered by replaying the push's walk of a package, which a cold run makes too, each absent
// when 0. Problems is what kept the cache from being read or written, every build's, which the engine notes on stderr
// one by one and the footer counts, and WriteFailed every entry whose write failed, every build's, of which stderr
// notes only the first, each absent when 0 (#27r8r24).
// On is false under `--no-cache`. Only a Go run has one; a Swift run's lint record carries none, so its output is as
// it was.
type packageCacheUse struct {
	On           bool `json:"on"`
	Hit          int  `json:"hit"`
	FromBinary   int  `json:"standardLibraryFromBinary,omitempty"`
	Missed       int  `json:"missed"`
	Written      int  `json:"written"`
	Unstorable   int  `json:"unstorable"`
	FactsDropped int  `json:"factsDropped"`
	FactsSkipped int  `json:"factsSkipped"`
	Waves        int  `json:"waves"`
	Reloads      int  `json:"reloads,omitempty"`
	ReloadRounds int  `json:"reloadRounds,omitempty"`
	Replays      int  `json:"replays,omitempty"`
	Problems     int  `json:"problems,omitempty"`
	WriteFailed  int  `json:"writeFailed,omitempty"`
}

// line is the cache's clause of the --verbose account, after "lint: ": the three counts every run has, and the ones
// that are usually zero only when they are not, so the line stays short where nothing went wrong. The standard
// library's packages from the binary are said whether the store was on or not.
func (cache packageCacheUse) line() string {
	if !cache.On {
		line := "package cache off"
		if cache.FromBinary > 0 {
			line += ", " + cache.segment()
		}
		return line + cache.reloads()
	}
	line := fmt.Sprintf("package cache hit %d, missed %d, wrote %d", cache.Hit, cache.Missed, cache.Written)
	if cache.FromBinary > 0 {
		line += ", " + cache.segment()
	}
	if cache.Unstorable > 0 {
		line += fmt.Sprintf(", %d unstorable", cache.Unstorable)
	}
	if cache.FactsDropped > 0 {
		line += fmt.Sprintf(", %d facts dropped", cache.FactsDropped)
	}
	if cache.FactsSkipped > 0 {
		line += fmt.Sprintf(", %d facts skipped", cache.FactsSkipped)
	}
	return line + cache.reloads()
}

// reloads is the account's clause of the packages loaded again from source and the rounds that took, and of the
// answers replayed, empty when there were none.
func (cache packageCacheUse) reloads() string {
	clause := ""
	if cache.Reloads > 0 {
		clause = fmt.Sprintf(", %d reloaded from source in %d rounds", cache.Reloads, cache.ReloadRounds)
	}
	if cache.Replays > 0 {
		clause += fmt.Sprintf(", %d answers replayed", cache.Replays)
	}
	if cache.Problems > 0 || cache.WriteFailed > 0 {
		clause += ", " + cache.problemsSegment()
	}
	return clause
}

// segment is the standard library's packages restored from the binary, as the footer and the account say them, so a
// cold run that reused them reads as such.
func (cache packageCacheUse) segment() string {
	return "standard library: " + grouped(cache.FromBinary) + " from the binary"
}

// problemsSegment is what kept the cache from being read or written, as the footer and the account say it, so a run
// whose cache could not be written says so where people look, and not on stderr alone: the problems, and every write
// that failed, since stderr notes only the first of those.
func (cache packageCacheUse) problemsSegment() string {
	segment := counted(cache.Problems, "findings cache problem", "findings cache problems")
	if cache.WriteFailed > 0 {
		segment += ", " + counted(cache.WriteFailed, "write failed", "writes failed")
	}
	return segment
}
