package main

import "github.com/system-inc/cohere/internal/format/formatfiles"

// formatUniverse is every file the default format scope is drawn from: what the formatter handles in
// the repository the run writes, and nowhere else.
//
// It stops at the repository's own boundary. Writes stay inside one repository (@system_cohere,
// 2026-10-03): a submodule is formatted by a run inside it, which leaves the commit in the submodule,
// and a run here reads the submodules it declares and reports their drift instead of writing it (see
// checkNestedRepositories). The universe used to descend into every declared submodule, so a default
// run in ahra rewrote Structure and Nexus whenever their files were not on record, and the change
// landed in repositories nobody running here was committing to.
type formatUniverse struct {
	// root is the repository's own walk; its nested repositories are named and not walked.
	root formatfiles.Enumeration

	files []string
}

// describe says what the universe walked, in the words describeEnumeration uses for one walk.
func (universe formatUniverse) describe() string {
	return describeEnumeration(universe.root, len(universe.root.Files))
}
