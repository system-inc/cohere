package main

import (
	"fmt"
	"strings"
)

// mergeConflict is a merge of the head onto main that git could not finish, with the tree it wrote, conflict
// markers and all, and the conflicted paths.
type mergeConflict struct {
	Main  string
	Tree  string
	Paths []string
}

func (conflict *mergeConflict) Error() string {
	return fmt.Sprintf("main at %s conflicts with this work in:\n%s\nmerge main and resolve them, then land again",
		shortCommit(conflict.Main), strings.Join(conflict.Paths, "\n"))
}
