package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// packageCacheUse is what cohere-go's per-package findings cache did in the lint phase, by package, as the lint
// record's `cache` object says it (swift_language/Contract.md): hit, restored from an entry with nothing run on it;
// missed, analyzed from source, of which written got an entry and unstorable held a fact no entry can hold; the facts
// the written entries left out, since nothing outside their package can name their object; the restored facts skipped,
// whose object the package's export data does not hold; and the waves the lint phase analyzed packages in, at most two.
// FromSystemCache is the standard-library packages restored from cohere-go's system cache, its tables in the user's
// cache directory, every build's summed, apart from hit, which `--no-cache` reads too, since they are of cohere and the
// Go installation and nothing of the project; it is absent when 0. SystemCacheWritten is the entries the run's builds
// added to their tables of the system cache, summed, absent when 0, and SystemCacheOff is set under
// `--no-system-cache`, which neither reads nor writes it. SystemCacheNotKept is why the run kept no system cache though
// it was on, or could not keep it as it should, in one line, absent when there is none (#syscache). Reloads is the
// packages the run loaded again from source, though the cache held them, to answer a question of a fact computed on
// demand that no store answered (#w7zmvat wave 3), and ReloadRounds the loads and lints again that took, and Replays
// the questions of such a fact answered by replaying the push's walk of a package, which a cold run makes too, each
// absent when 0. Problems is what kept the cache from being read or written, every build's, which the child notes on
// stderr one by one and the footer counts, and WriteFailed every entry whose write failed, every build's, of which
// stderr notes only the first, each absent when 0 (#27r8r24). Packs is what the extra builds did with their packs,
// every build's summed, absent in a run with no extra build (#b2t9xmf). On is false under `--no-cache`. Only a Go run
// has one; a Swift run's lint record carries none, so its output is as it was.
type packageCacheUse struct {
	On                 bool   `json:"on"`
	Hit                int    `json:"hit"`
	FromSystemCache    int    `json:"standardLibraryFromSystemCache,omitempty"`
	SystemCacheWritten int    `json:"systemCacheWritten,omitempty"`
	SystemCacheOff     bool   `json:"systemCacheOff,omitempty"`
	SystemCacheNotKept string `json:"systemCacheNotKept,omitempty"`
	Missed             int    `json:"missed"`
	Written            int    `json:"written"`
	Unstorable         int    `json:"unstorable"`
	FactsDropped       int    `json:"factsDropped"`
	FactsSkipped       int    `json:"factsSkipped"`
	Waves              int    `json:"waves"`
	Reloads            int    `json:"reloads,omitempty"`
	ReloadRounds       int    `json:"reloadRounds,omitempty"`
	Replays            int    `json:"replays,omitempty"`
	Problems           int    `json:"problems,omitempty"`
	WriteFailed        int    `json:"writeFailed,omitempty"`

	Packs *buildPacksUse `json:"packs,omitempty"`
}

// buildPacksUse is what the extra builds did with their packs, as the lint record's `cache.packs` says it
// (swift_language/Contract.md): each build reads one file of each store holding every record its last run read or wrote
// there, rather than open each record, and serves a record from it only while the record's shard of the store is as it
// was. The packs read whole and those missing, which is no refusal, the records served, those whose shard had moved and
// those no pack held, each read from the store, the shard stamps read, each pack refused by the first word of why, and
// the packs written.
type buildPacksUse struct {
	Read          int            `json:"read"`
	Missing       int            `json:"missing"`
	RecordsServed int            `json:"recordsServed"`
	RecordsStale  int            `json:"recordsStale"`
	RecordsMissed int            `json:"recordsMissed"`
	ShardReads    int            `json:"shardReads"`
	RefusedBy     map[string]int `json:"refusedBy,omitempty"`
	Written       int            `json:"written"`
}

// line is the cache's clause of the --verbose account, after "lint: ": the three counts every run has, and the ones
// that are usually zero only when they are not, so the line stays short where nothing went wrong. Where the standard
// library came from is said whether the store was on or not (systemCacheClause).
func (cache packageCacheUse) line() string {
	if !cache.On {
		return "package cache off" + cache.systemCacheClause() + cache.packsClause() + cache.reloads()
	}
	line := fmt.Sprintf("package cache hit %d, missed %d, wrote %d", cache.Hit, cache.Missed, cache.Written)
	line += cache.systemCacheClause()
	if cache.Unstorable > 0 {
		line += fmt.Sprintf(", %d unstorable", cache.Unstorable)
	}
	if cache.FactsDropped > 0 {
		line += fmt.Sprintf(", %d facts dropped", cache.FactsDropped)
	}
	if cache.FactsSkipped > 0 {
		line += fmt.Sprintf(", %d facts skipped", cache.FactsSkipped)
	}
	return line + cache.packsClause() + cache.reloads()
}

// systemCacheClause is the account's clause of where the standard library came from and went (#syscache): the packages
// restored from the system cache and the entries written to it, or that the run kept it off, so a run that computed
// the standard library says why. Empty when neither moved, as in a run that restored nothing and wrote nothing new.
// The reason the system cache was not kept is the footer's (footer), which every run's reader sees.
func (cache packageCacheUse) systemCacheClause() string {
	if cache.SystemCacheOff {
		return ", system cache off (--no-system-cache)"
	}
	clause := ""
	if cache.FromSystemCache > 0 {
		clause += ", " + cache.segment()
	}
	if cache.SystemCacheWritten > 0 {
		clause += ", " + counted(cache.SystemCacheWritten, "entry", "entries") + " written to the system cache"
	}
	return clause
}

// packsClause is the account's clause of what the extra builds did with their packs (#b2t9xmf): the packs read and the
// records they served, then, when there were any, the packs missing, the records whose shard had moved, the records no
// pack held, the packs refused with why and the packs written, so a warm run whose packs served everything says only
// that. Empty in a run with no extra build.
func (cache packageCacheUse) packsClause() string {
	packs := cache.Packs
	if packs == nil {
		return ""
	}
	clause := fmt.Sprintf(", builds' packs: %s read, %s served", grouped(packs.Read),
		counted(packs.RecordsServed, "record", "records"))
	if packs.Missing > 0 {
		clause += ", " + grouped(packs.Missing) + " missing"
	}
	if packs.RecordsStale > 0 {
		clause += ", " + grouped(packs.RecordsStale) + " stale"
	}
	if packs.RecordsMissed > 0 {
		clause += ", " + grouped(packs.RecordsMissed) + " not held"
	}
	if len(packs.RefusedBy) > 0 {
		refused := 0
		reasons := make([]string, 0, len(packs.RefusedBy))
		for _, word := range slices.Sorted(maps.Keys(packs.RefusedBy)) {
			refused += packs.RefusedBy[word]
			reasons = append(reasons, fmt.Sprintf("%s %d", word, packs.RefusedBy[word]))
		}
		clause += fmt.Sprintf(", %s refused (%s)", grouped(refused), strings.Join(reasons, ", "))
	}
	if packs.Written > 0 {
		clause += ", " + grouped(packs.Written) + " written"
	}
	return clause
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

// segment is the standard library's packages restored from the system cache, as the footer and the account say them,
// so a cold run that reused them reads as such.
func (cache packageCacheUse) segment() string {
	return "standard library: " + grouped(cache.FromSystemCache) + " from the system cache"
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
