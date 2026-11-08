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
