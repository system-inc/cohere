package main

import "encoding/json"

// engineLine is the part of one record of the engine's contract 3 stream the pair reads, by the names
// internal/go_engine/contract gives them: the project record's files in scope and the lint record's files walked,
// cache, builds and width. It is its own shape rather than the contract's types because two engines of different
// commits write the streams it reads, and a field another commit changed must not fail the read of the fields here.
type engineLine struct {
	Kind         string `json:"kind"`
	FilesInScope int    `json:"filesInScope"`
	FilesWalked  int    `json:"filesWalked"`
	Cache        struct {
		// StandardLibraryFromBinary is left out of the record when it is 0, so a record without it reads 0.
		StandardLibraryFromBinary int `json:"standardLibraryFromBinary"`
	} `json:"cache"`
	BuildsAtOnce         int             `json:"buildsAtOnce"`
	BuildsWidthLiveBytes uint64          `json:"buildsWidthLiveBytes"`
	Builds               json.RawMessage `json:"builds"`
}
