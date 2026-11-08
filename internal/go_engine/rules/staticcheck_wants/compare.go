//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling

package staticcheck_wants

import "slices"

// Finding is one finding as Compare matches it against the wants: where its start is, and its message.
type Finding struct {
	// File is the full path of the file the finding is in.
	File string
	// Line is the line the finding starts on.
	Line int
	// Message is the finding's message.
	Message string
}

// Compare matches findings against wants as staticcheck's testutil does: each finding takes one want on its file
// and line whose text or pattern its message matches, and a want is taken once. It returns the findings no want
// expected and the wants no finding took, both empty when the two agree.
func Compare(wants []Want, findings []Finding) (unexpected []Finding, untaken []Want) {
	type place struct {
		file string
		line int
	}
	remaining := map[place][]Want{}
	for _, want := range wants {
		key := place{file: want.File, line: want.Line}
		remaining[key] = append(remaining[key], want)
	}
	unexpected = []Finding{}
	for _, found := range findings {
		key := place{file: found.File, line: found.Line}
		index := slices.IndexFunc(remaining[key], func(want Want) bool {
			return want.Matches(found.Message)
		})
		if index < 0 {
			unexpected = append(unexpected, found)
			continue
		}
		remaining[key] = slices.Delete(remaining[key], index, index+1)
	}
	untaken = []Want{}
	for _, want := range wants {
		key := place{file: want.File, line: want.Line}
		// A want still remaining is one no finding took; taking it from remaining here lists each once, in the
		// order the fixture wrote them.
		if index := slices.Index(remaining[key], want); index >= 0 {
			remaining[key] = slices.Delete(remaining[key], index, index+1)
			untaken = append(untaken, want)
		}
	}
	return unexpected, untaken
}

// FoundFact is one fact an analyzer exported about an object of the package it ran on, as CompareFacts matches it
// against the fact wants: where the object is declared, its name, and the fact as printed.
type FoundFact struct {
	// File is the full path of the file the object is declared in.
	File string
	// Line is the line the object's declaration is on.
	Line int
	// Object is the object's name.
	Object string
	// Fact is the fact as staticcheck's runner prints it, fmt.Sprint of it.
	Fact string
}

// CompareFacts matches facts against wants as staticcheck's testutil does: each fact takes one want on its object's
// file and line that names its object and whose text or pattern the printed fact matches, and a want is taken once.
// It returns the facts no want expected and the wants no fact took, both empty when the two agree.
func CompareFacts(wants []FactWant, facts []FoundFact) (unexpected []FoundFact, untaken []FactWant) {
	type place struct {
		file string
		line int
	}
	remaining := map[place][]FactWant{}
	for _, want := range wants {
		key := place{file: want.File, line: want.Line}
		remaining[key] = append(remaining[key], want)
	}
	unexpected = []FoundFact{}
	for _, found := range facts {
		key := place{file: found.File, line: found.Line}
		index := slices.IndexFunc(remaining[key], func(want FactWant) bool {
			return want.Object == found.Object && want.Matches(found.Fact)
		})
		if index < 0 {
			unexpected = append(unexpected, found)
			continue
		}
		remaining[key] = slices.Delete(remaining[key], index, index+1)
	}
	untaken = []FactWant{}
	for _, want := range wants {
		key := place{file: want.File, line: want.Line}
		// As in Compare, a want still remaining is one no fact took, listed once in the order the fixture wrote it.
		if index := slices.Index(remaining[key], want); index >= 0 {
			remaining[key] = slices.Delete(remaining[key], index, index+1)
			untaken = append(untaken, want)
		}
	}
	return unexpected, untaken
}
