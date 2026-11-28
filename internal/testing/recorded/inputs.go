package recorded

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Inputs is an inputs file a test and its recorder both read: what its texts are and where they came from,
// whether they are vendored, and the texts. A vendored text (a file of another project's, kept for the
// comparison) is answered by its Digest rather than by a second copy of it. A file that is one of a set's
// Parts says which part it is and of how many.
type Inputs struct {
	About    string  `json:"about"`
	Part     int     `json:"part,omitempty"`
	Parts    int     `json:"parts,omitempty"`
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

// Parts are an inputs or answers set split across numbered files, to keep each under 50 KB: directory's
// files named prefix, a number from 1 and .json, in number order. Every number up to the count on disk must
// be there, and each file must say it is its own number of that count, so a part added is read as soon as it
// exists and a part missing fails, the last one too, since the others still count it. None at all is an error
// too, and so is a compressed part.
func Parts(directory string, prefix string) ([]string, error) {
	compressed, compressedGlobError := filepath.Glob(filepath.Join(directory, prefix+"*.gz"))
	if compressedGlobError != nil {
		return nil, compressedGlobError
	}
	if len(compressed) != 0 {
		return nil, fmt.Errorf(
			"%s is compressed, and compressed fixtures never land on a public repository",
			compressed[0],
		)
	}
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
	// Only once every name is there are the parts read for their numbers, so a gap is named as the file
	// missing rather than as the count every other part gives.
	for index := range parts {
		numbering, numberingError := readNumbering(parts[index])
		if numberingError != nil {
			return nil, numberingError
		}
		if numbering.Part != index+1 || numbering.Parts != len(parts) {
			return nil, fmt.Errorf(
				"%s says it is part %d of %d, and it is part %d of the %d on disk",
				parts[index],
				numbering.Part,
				numbering.Parts,
				index+1,
				len(parts),
			)
		}
	}
	return parts, nil
}

// numbering is the two fields of a part that say where it stands among its set's Parts.
type numbering struct {
	Part  int `json:"part"`
	Parts int `json:"parts"`
}

// readNumbering is the numbering of the part at path, decoded alone: whatever else the part holds is the
// reader's to decode and check, by the type it declares.
func readNumbering(path string) (numbering, error) {
	var decoded numbering
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return decoded, readError
	}
	if unmarshalError := json.Unmarshal(contents, &decoded); unmarshalError != nil {
		return decoded, fmt.Errorf("%s: %w", path, unmarshalError)
	}
	return decoded, nil
}
