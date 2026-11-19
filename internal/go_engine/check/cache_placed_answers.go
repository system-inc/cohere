package check

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// storedPlaced is a package's placed answers (fact_resolver.PlacedAnswer) as its entry holds them, each name once and
// by column, one row per answer, in the order PlacedIn sorts them, which gob writes as runs of numbers rather than a
// struct a row. Kinds is every kind's name among them, sorted; Files and FileDigests are every file they are placed
// in, its name and the digest of the bytes its offsets count in, one pair to an index, sorted by name and then digest,
// so two files parsed under one name, as cmd/cgo's are, are two. Each row is its answer's kind and file as their
// indexes in those (KindIndexes, FileIndexes), its offset less the one before it in the same file, the first's from 0,
// never negative since they come sorted (OffsetSteps), its parameter, -1 for the function itself (Parameters), and
// its answer, the bytes Ask encodes (Values). An entry held each answer with its kind's name and its file's whole path
// beside it, which made the store 8.6% larger at cohere's root and 10.7% at deeafbf4's (#k7k3wwc). It is one struct
// of slices, so the type gob describes at the head of every entry is one.
type storedPlaced struct {
	Kinds       []string
	Files       []string
	FileDigests [][]byte
	KindIndexes []int
	FileIndexes []int
	OffsetSteps []int
	Parameters  []int
	Values      [][]byte
}

// placedFile is one file of storedPlaced's table: its name and the digest of its bytes.
type placedFile struct {
	name   string
	digest string
}

// storedPlacedOf is answers as an entry holds them, which storedPlaced.placed reads back exactly, in PlacedIn's order
// whatever order answers came in.
func storedPlacedOf(answers []fact_resolver.PlacedAnswer) storedPlaced {
	if len(answers) == 0 {
		return storedPlaced{}
	}
	sorted := slices.SortedFunc(slices.Values(answers), fact_resolver.ComparePlaced)
	kindIndex, fileIndex := map[string]int{}, map[placedFile]int{}
	for _, answer := range sorted {
		kindIndex[answer.Kind] = 0
		fileIndex[placedFile{name: answer.File, digest: string(answer.FileDigest)}] = 0
	}
	stored := storedPlaced{Kinds: slices.Sorted(maps.Keys(kindIndex))}
	for index, kind := range stored.Kinds {
		kindIndex[kind] = index
	}
	files := slices.SortedFunc(maps.Keys(fileIndex), func(left placedFile, right placedFile) int {
		return cmp.Or(cmp.Compare(left.name, right.name), cmp.Compare(left.digest, right.digest))
	})
	for index, file := range files {
		fileIndex[file] = index
		stored.Files = append(stored.Files, file.name)
		stored.FileDigests = append(stored.FileDigests, []byte(file.digest))
	}
	previousFile, previousOffset := -1, 0
	for _, answer := range sorted {
		file := fileIndex[placedFile{name: answer.File, digest: string(answer.FileDigest)}]
		step := answer.Offset
		if file == previousFile {
			step -= previousOffset
		}
		stored.KindIndexes = append(stored.KindIndexes, kindIndex[answer.Kind])
		stored.FileIndexes = append(stored.FileIndexes, file)
		stored.OffsetSteps = append(stored.OffsetSteps, step)
		stored.Parameters = append(stored.Parameters, answer.Parameter)
		stored.Values = append(stored.Values, answer.Encoded)
		previousFile, previousOffset = file, answer.Offset
	}
	return stored
}

// placed is the answers stored holds, in PlacedIn's order, an empty digest read as none, or an error, which is a miss,
// for columns of different lengths or an index past its table, which only bytes no storedPlacedOf wrote have.
func (stored storedPlaced) placed() ([]fact_resolver.PlacedAnswer, error) {
	rows := len(stored.KindIndexes)
	if len(stored.FileIndexes) != rows || len(stored.OffsetSteps) != rows || len(stored.Parameters) != rows ||
		len(stored.Values) != rows || len(stored.FileDigests) != len(stored.Files) {
		return nil, fmt.Errorf("reading the placed answers: their columns hold %d kinds, %d files, %d offsets, %d "+
			"parameters and %d values, and their table %d files and %d digests", rows, len(stored.FileIndexes),
			len(stored.OffsetSteps), len(stored.Parameters), len(stored.Values), len(stored.Files),
			len(stored.FileDigests))
	}
	answers := make([]fact_resolver.PlacedAnswer, 0, rows)
	previousFile, previousOffset := -1, 0
	for row := range rows {
		kind, file := stored.KindIndexes[row], stored.FileIndexes[row]
		if kind < 0 || kind >= len(stored.Kinds) || file < 0 || file >= len(stored.Files) {
			return nil, fmt.Errorf("reading the placed answers: row %d names kind %d of %d and file %d of %d", row,
				kind, len(stored.Kinds), file, len(stored.Files))
		}
		answer := fact_resolver.PlacedAnswer{Kind: stored.Kinds[kind], File: stored.Files[file],
			Offset: stored.OffsetSteps[row], Parameter: stored.Parameters[row], Encoded: stored.Values[row]}
		if len(stored.FileDigests[file]) > 0 {
			answer.FileDigest = stored.FileDigests[file]
		}
		if file == previousFile {
			answer.Offset += previousOffset
		}
		answers = append(answers, answer)
		previousFile, previousOffset = file, answer.Offset
	}
	return answers, nil
}

// digestsOf is files' digests as PlacedIn and the placed hand read them, the sha256 of each file's bytes by the name
// the load parsed it under, none for a file files did not hash, as a package keyed by its path alone never is.
func digestsOf(files cache.FileDigests) func(parsed string) ([]byte, bool) {
	return func(parsed string) ([]byte, bool) {
		digest, hashed := files.Digest(parsed)
		if !hashed {
			return nil, false
		}
		return digest[:], true
	}
}
