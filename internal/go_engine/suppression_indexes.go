package go_engine

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// suppressionIndexes are the run's suppression indexes, one per file for the bytes the run holds of it, which every
// suppress of the run reads: the host's lint pass, each build's outcome when builds merge, and the rename's rounds.
// Every build's run reads the same held bytes, so before these each build indexed every file it compiles again, 67 ms
// of a warm run checking every build against the host's 8 ms (#qqyq61s). An index is read-only once built
// (suppression.Index), so one is shared by every reader. Its zero value is ready, and it is safe for concurrent use,
// since suppress indexes many files at once.
type suppressionIndexes struct {
	lock   sync.Mutex
	byFile map[string]*heldIndex

	// built, set only by a test, is called each time a file is indexed, on the goroutine that indexes it, so a test can
	// count that a run indexes each file once.
	built func(file string)
}

// heldIndex is one file's index, built the first time it is read, of source, the bytes the run held of the file when
// it was asked for.
type heldIndex struct {
	source []byte
	index  func() *suppression.Index
}

// of is file's index for source, the bytes the run holds of it now, built once for the run while it holds those bytes.
// The run replaces a file's bytes rather than writing into them (held): a reread after the fix phase writes, the
// rename's rewrite, the editor's save formatting its buffer. So bytes at the same address and of the same length are
// the bytes the index was built from, and any others index the file again, since a directive's lines may have moved.
// The index held keeps its source alive, so its address is never reused for other bytes while it is compared.
func (indexes *suppressionIndexes) of(file string, source []byte) *suppression.Index {
	indexes.lock.Lock()
	held, found := indexes.byFile[file]
	if !found || !sameBytes(held.source, source) {
		held = &heldIndex{source: source, index: sync.OnceValue(func() *suppression.Index {
			if indexes.built != nil {
				indexes.built(file)
			}
			return suppression.Build(file, source)
		})}
		if indexes.byFile == nil {
			indexes.byFile = map[string]*heldIndex{}
		}
		indexes.byFile[file] = held
	}
	indexes.lock.Unlock()
	// Built outside the lock, so the files suppress indexes at once are scanned at once.
	return held.index()
}

// sameBytes is whether left and right are the same bytes in memory, not merely equal: the same length, at the same
// address. Two empty slices are the same, since an empty file holds no directive wherever it sits.
func sameBytes(left []byte, right []byte) bool {
	return len(left) == len(right) && (len(left) == 0 || &left[0] == &right[0])
}
