package main

import "path/filepath"

// projectYield is what one project's run leaves to others, as its parent wrote it.
type projectYield struct {
	// Files are files the tsconfig includes that a nearer one owns, absolute.
	Files []string `json:"files"`

	// Directories are directories the run's format walk leaves out, absolute, each formatted by the run
	// of the project there. The project's own directory means the walk formats nothing: another tsconfig
	// in the same directory formats it. A Swift run's are the Swift projects below it, whose files its
	// not-checked notes leave out (see discovery.swiftYield).
	Directories []string `json:"directories"`

	// LeftOutExtensions are the extensions of the files the run's format walk leaves out, those of the languages
	// the two flags did not name (#s7gt4bj), lowercase and sorted. Left out of the encoding when empty, so a run that
	// named no languages keeps the replay key it had.
	LeftOutExtensions []string `json:"leftOutExtensions,omitempty"`
}

// yieldedFiles is the run's yielded files as the set program.Options.Yielded takes, nil when it yields none.
func (yield projectYield) yieldedFiles() map[string]struct{} {
	if len(yield.Files) == 0 {
		return nil
	}
	files := make(map[string]struct{}, len(yield.Files))
	for _, fileName := range yield.Files {
		files[filepath.Clean(fileName)] = struct{}{}
	}
	return files
}
