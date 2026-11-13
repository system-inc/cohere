package main

// goDirectory is a directory a go command reads a module graph in, a side's source or a corpus's root, by the name
// the preflight and --prepare say it with.
type goDirectory struct {
	label string
	path  string
}
