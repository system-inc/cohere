package check

import (
	"fmt"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// storedPlaced is a package's placed answers (fact_resolver.PlacedAnswer) as its entry holds them, each name once and
// by column, one row per answer, in the order PlacedIn sorts them, which gob writes as runs of numbers rather than a
// struct a row: Kinds and Files are every kind's and file's name among them, sorted, and each row is its answer's
// kind and file as their indexes in those (KindIndexes, FileIndexes), its offset less the one before it in the same
// file, the first's from 0, never negative since they come sorted (OffsetSteps), its parameter, -1 for the function
// itself (Parameters), and its answer, the bytes Ask encodes (Values). An entry held each answer with its kind's name
// and its file's whole path beside it, which made the store 8.6% larger at cohere's root and 10.7% at deeafbf4's
// (#k7k3wwc). It is one struct of slices, so the type gob describes at the head of every entry is one.
type storedPlaced struct {
	Kinds       []string
	Files       []string
	KindIndexes []int
	FileIndexes []int
	OffsetSteps []int
	Parameters  []int
	Values      [][]byte
}

// storedPlacedOf is answers as an entry holds them, which storedPlaced.placed reads back exactly, in PlacedIn's order
// whatever order answers came in.
func storedPlacedOf(answers []fact_resolver.PlacedAnswer) storedPlaced {
	if len(answers) == 0 {
		return storedPlaced{}
	}
	sorted := slices.SortedFunc(slices.Values(answers), fact_resolver.ComparePlaced)
	kindIndex, fileIndex := map[string]int{}, map[string]int{}
	for _, answer := range sorted {
		kindIndex[answer.Kind], fileIndex[answer.File] = 0, 0
	}
	stored := storedPlaced{Kinds: slices.Sorted(maps.Keys(kindIndex)), Files: slices.Sorted(maps.Keys(fileIndex))}
	for index, kind := range stored.Kinds {
		kindIndex[kind] = index
	}
	for index, file := range stored.Files {
		fileIndex[file] = index
	}
	previous := fact_resolver.PlacedAnswer{}
	for _, answer := range sorted {
		step := answer.Offset
		if answer.File == previous.File {
			step -= previous.Offset
		}
		stored.KindIndexes = append(stored.KindIndexes, kindIndex[answer.Kind])
		stored.FileIndexes = append(stored.FileIndexes, fileIndex[answer.File])
		stored.OffsetSteps = append(stored.OffsetSteps, step)
		stored.Parameters = append(stored.Parameters, answer.Parameter)
		stored.Values = append(stored.Values, answer.Encoded)
		previous = answer
	}
	return stored
}

// placed is the answers stored holds, in PlacedIn's order, or an error, which is a miss, for columns of different
// lengths or an index past its table, which only bytes no storedPlacedOf wrote have.
func (stored storedPlaced) placed() ([]fact_resolver.PlacedAnswer, error) {
	rows := len(stored.KindIndexes)
	if len(stored.FileIndexes) != rows || len(stored.OffsetSteps) != rows || len(stored.Parameters) != rows ||
		len(stored.Values) != rows {
		return nil, fmt.Errorf("reading the placed answers: their columns hold %d kinds, %d files, %d offsets, %d "+
			"parameters and %d values", rows, len(stored.FileIndexes), len(stored.OffsetSteps),
			len(stored.Parameters), len(stored.Values))
	}
	answers := make([]fact_resolver.PlacedAnswer, 0, rows)
	previous := fact_resolver.PlacedAnswer{}
	for row := range rows {
		kind, file := stored.KindIndexes[row], stored.FileIndexes[row]
		if kind < 0 || kind >= len(stored.Kinds) || file < 0 || file >= len(stored.Files) {
			return nil, fmt.Errorf("reading the placed answers: row %d names kind %d of %d and file %d of %d", row,
				kind, len(stored.Kinds), file, len(stored.Files))
		}
		answer := fact_resolver.PlacedAnswer{Kind: stored.Kinds[kind], File: stored.Files[file],
			Offset: stored.OffsetSteps[row], Parameter: stored.Parameters[row], Encoded: stored.Values[row]}
		if answer.File == previous.File {
			answer.Offset += previous.Offset
		}
		answers = append(answers, answer)
		previous = answer
	}
	return answers, nil
}
