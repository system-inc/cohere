package recorded

import (
	"fmt"
	"path/filepath"
)

// Inputs is an inputs file a test and its recorder both read: what its texts are and where they came from,
// whether they are vendored, and the texts. A vendored text (a file of another project's, kept for the
// comparison) is answered by its Digest rather than by a second copy of it.
type Inputs struct {
	About    string  `json:"about"`
	Vendored bool    `json:"vendored,omitempty"`
	Inputs   []Input `json:"inputs"`
}

// Input is one text under a name unique in its file, with what it is when the name alone does not say.
type Input struct {
	Name  string `json:"name"`
	Text  string `json:"text"`
	About string `json:"about,omitempty"`
}

// Entries are the inputs as Match pairs them: each name with its text, or with the text's Digest when the
// file is vendored.
func (inputs Inputs) Entries() []Entry {
	entries := make([]Entry, len(inputs.Inputs))
	for index, input := range inputs.Inputs {
		entries[index] = Entry{Name: input.Name, Input: input.Text}
		if inputs.Vendored {
			entries[index].Input = Digest([]byte(input.Text))
		}
	}
	return entries
}

// ReadInputs reads inputs files, in order, and returns their inputs, and beside each the entry Match pairs
// it by: its text, or its Digest when its own file is vendored.
func ReadInputs(paths ...string) ([]Input, []Entry, error) {
	var inputs []Input
	var entries []Entry
	for _, path := range paths {
		file, readJsonError := ReadJson[Inputs](path)
		if readJsonError != nil {
			return nil, nil, readJsonError
		}
		if len(file.Inputs) == 0 {
			return nil, nil, fmt.Errorf("%s holds no inputs", path)
		}
		inputs = append(inputs, file.Inputs...)
		entries = append(entries, file.Entries()...)
	}
	return inputs, entries, nil
}

// Parts are an inputs set split across numbered files, to keep each under 50 KB: directory's files named
// prefix, a number from 1 and .json, in number order. Every number up to the count on disk must be there, so
// a part added is read as soon as it exists and a part missing fails. None at all is an error too.
func Parts(directory string, prefix string) ([]string, error) {
	matches, globError := filepath.Glob(filepath.Join(directory, prefix+"*.json"))
	if globError != nil {
		return nil, globError
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s holds no %s*.json", directory, prefix)
	}
	present := make(map[string]bool, len(matches))
	for _, match := range matches {
		present[match] = true
	}
	parts := make([]string, len(matches))
	for index := range parts {
		// Glob sorts by name, which puts a tenth part before the second, so the parts are named by number.
		parts[index] = filepath.Join(directory, fmt.Sprintf("%s%d.json", prefix, index+1))
		if !present[parts[index]] {
			return nil, fmt.Errorf(
				"%s holds %d %s*.json files, and %s is not one",
				directory,
				len(matches),
				prefix,
				parts[index],
			)
		}
	}
	return parts, nil
}
