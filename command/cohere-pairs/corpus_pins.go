package main

// The corpora's pins. Every pair since 2026-11-10 was measured on these commits, so a pair measured here reads beside
// every pair before it: a new pin moves every number at once and starts a new series, which is a decision of its
// own, made here and said in the commit that makes it. Each nested pin, a submodule's commit, is read from the tree
// that pins it (gitlinkOf), never written down, so it cannot disagree with the commit above it.
const (
	// deeafbf4CohereCommit is cohere at the commit the corpus is named for.
	deeafbf4CohereCommit = "deeafbf43e5dfb39c7bacdcdc3f165b571b97276"
	// adamicCommit is adamic, which pins cohere at cohere/, and cohere its compiler at cohere/TypeScript/: adamic's
	// go.work uses ./cohere/TypeScript/tsc, so a corpus without both checks a workspace that does not build.
	adamicCommit = "217863c10426a4a9e03f74cefb9d789721d45d3f"
	// loomCommit is loom, release-0.8.1195, which has no submodules.
	loomCommit = "9510c6baddda7b04f1ac4fa5f927d627f9a01cb3"
)

// The corpora's names, which are their directories under --out's corpora and the first word of each raw output.
const (
	corpusRoot     = "root"
	corpusDeeafbf4 = "deeafbf4"
	corpusAdamic   = "adamic"
	corpusLoom     = "loom"
)

// corpusOrder is the order the corpora are measured in: the smallest first, so a pair that is going to fail on
// something every corpus shares fails in the first minutes.
var corpusOrder = []string{corpusRoot, corpusDeeafbf4, corpusLoom, corpusAdamic}

// The clones under --corpora the pins are read from.
const (
	adamicCloneName = "adamic"
	loomCloneName   = "loom"
)
