package main

// corpusLayer is one tree a corpus is made of: a repository's commit, extracted at a directory within the corpus.
type corpusLayer struct {
	// repository is the directory git reads the commit from, a checkout or a git directory.
	repository string
	commit     string
	// within is where in the corpus the tree goes, slash separated, empty for the corpus's root.
	within string
}
