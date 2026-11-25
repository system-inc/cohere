package main

import "encoding/json"

// goChildLine is the part of one record of the Go child's contract 3 stream the pair reads, by the names
// internal/go_language/contract gives them: the project record's files in scope and the lint record's files walked,
// cache, builds and width. It is its own shape rather than the contract's types because two Go children of different
// commits write the streams it reads, and a field another commit changed must not fail the read of the fields here.
type goChildLine struct {
	Kind         string `json:"kind"`
	FilesInScope int    `json:"filesInScope"`
	FilesWalked  int    `json:"filesWalked"`
	Cache        struct {
		// StandardLibraryFromSystemCache is the standard-library packages the run restored from the Go child's system
		// cache, every build's summed (#syscache), SystemCacheWritten the entries it added to it, SystemCacheOff
		// whether it ran under `--no-system-cache`, and SystemCacheNotKept why it could not keep it. Each is left out
		// of the record when it is 0, false or empty, so a record without it reads so.
		StandardLibraryFromSystemCache int    `json:"standardLibraryFromSystemCache"`
		SystemCacheWritten             int    `json:"systemCacheWritten"`
		SystemCacheOff                 bool   `json:"systemCacheOff"`
		SystemCacheNotKept             string `json:"systemCacheNotKept"`
		// StandardLibraryFromBinary is what a release before the system cache wrote of the standard-library packages it
		// restored from the table its binary carried, the same count under its old name. Read because a pair's base is
		// often such a release, an external binary whose records this code base does not write: the one read of an old
		// name here. It can go once no pair's base predates the system cache. Left out when 0, as the new name is.
		StandardLibraryFromBinary int `json:"standardLibraryFromBinary"`
	} `json:"cache"`
	BuildsAtOnce         int             `json:"buildsAtOnce"`
	BuildsWidthLiveBytes uint64          `json:"buildsWidthLiveBytes"`
	Builds               json.RawMessage `json:"builds"`
}
