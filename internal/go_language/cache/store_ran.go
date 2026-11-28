package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// A build's ran marker (#s8e6z0n) is an empty file of the store, its sum alone, at the place of a pack a run of the
// build wrote, or would have written had nothing changed, under the store's `ran` directory rather than `packs`: the
// first pack a run writes removes every other executable's packs (RemoveOtherIdentitiesPacks), so a pack alone cannot
// tell a build whose last run was under another executable from one that has never run, and the marker outlives it.
// It is in no shard's directory, so writing it moves no shard's stamp (ShardStamps), and it is written only when
// missing, so a settled run only stats it.

// ranDirectoryName is the directory of a store that holds its ran markers, which no shard's two hex characters can
// name, so no Get reads one.
const ranDirectoryName = "ran"

// HasRun reports whether a run has marked place (MarkRan), by a stat alone: true for anything there but nothing, as
// HasPack is, so a marker that cannot be read is not taken for none.
func (store *Store) HasRun(place PackPlace) bool {
	_, statError := os.Stat(store.ranPath(place))
	return !errors.Is(statError, fs.ErrNotExist)
}

// MarkRan keeps the ran marker of place, unless one is there already, so a run that writes it once writes nothing
// again. Two runs marking at once both write, and the rename leaves one whole file (writeWhole).
func (store *Store) MarkRan(place PackPlace) error {
	if store.HasRun(place) {
		return nil
	}
	return store.writeWhole(store.ranPath(place), nil)
}

// ranPath is the file of place's ran marker.
func (store *Store) ranPath(place PackPlace) string {
	return filepath.Join(store.directory, ranDirectoryName, place.Identity.String(), place.Name.String())
}
