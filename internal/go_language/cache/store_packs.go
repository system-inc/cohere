package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A pack is one file of a store holding many of its entries, so a run that reads every one reads one file rather than
// open each (#b2t9xmf): an extra build's run opened about 1,560 record files a build, and the opens were most of its
// kernel time. Each entry is held under the key it was asked by and the key it was stored under, which differ for one
// read through an alias. Beside them it holds, for each shard its entries were read from, the shard directory's stamp
// (ShardStamps) taken before they were read, so a run serves an entry only while the store holds exactly what the pack
// was made of. A pack is the store's own file, the sha256 of its contents then its contents, as an entry is (Put),
// under the store's `packs` directory, which no shard's two hex characters can name, so no Get reads it, in the
// directory of the executable identity whose runs read it (PackPlace), so a new executable's runs can remove the packs
// no run will read again (RemoveOtherIdentitiesPacks). Its contents are packMagic, packFormatVersion, each stamp in
// order of its shard, and each entry in order of the key it was asked by.

// packDirectoryName is the directory of a store that holds its packs.
const packDirectoryName = "packs"

// packMagic opens every pack's contents, after its sum.
const packMagic = "cohere go build pack\x00"

// packFormatVersion is the pack's own format, which a reader refuses when it is not its own: a pack of another
// format is ignored whole. Format 2 holds the shards' stamps.
const packFormatVersion uint32 = 2

// PackMissing is why a pack was not read when there is none, which is no refusal: a build's first run, or a new
// executable's, has none, and writes one.
const PackMissing = "missing"

// The first word of why a pack that is there was refused, each ignored whole: a file that could not be read, one
// shorter than its sum, one whose contents do not match its sum, one of another format, or one whose contents, though
// they match their sum, do not read as stamps and entries.
const (
	PackUnreadable = "unreadable"
	PackShort      = "short"
	PackSum        = "sum"
	PackVersion    = "version"
	PackMalformed  = "malformed"
)

// PackEntry is one entry a pack holds: Entry is what the store held under Stored, as Get returned it, read for the
// key Asked, which is Stored unless an alias led from one to the other.
type PackEntry struct {
	Asked  Key
	Stored Key
	Entry  []byte
}

// Pack is what a pack holds: its entries, by the key each was asked by, and the stamp of each shard directory its
// entries were read from, by shard (ShardOf), taken before they were read. A shard with no stamp had none worth
// trusting when the pack was written, and no entry of it is served.
type Pack struct {
	Entries map[Key]PackEntry
	Stamps  map[byte]int64
}

// PackPlace is where a store keeps a pack: in the directory of the executable identity whose runs read it
// (PackIdentity), by its name (PackName).
type PackPlace struct {
	Identity Key
	Name     Key
}

// PackIdentity is the directory of the packs that runs of the executable identity read: its digest, since an identity
// is any string.
func PackIdentity(executableIdentity string) Key {
	return sha256.Sum256([]byte(executableIdentity))
}

// PackName is the name of a pack, from sections, each a list of parts, which the caller makes of whatever tells one
// pack from another: a section is hashed with its length first and each part ends in a zero byte, so no two lists of
// sections name the same pack.
func PackName(sections ...[]string) Key {
	digest := sha256.New()
	for _, section := range sections {
		// Ignored: a hash.Hash never returns an error from Write.
		_, _ = digest.Write(binary.AppendUvarint(nil, uint64(len(section))))
		for _, part := range section {
			// Ignored: as above, and the zero byte ends the part.
			_, _ = digest.Write(append([]byte(part), 0))
		}
	}
	return Key(digest.Sum(nil))
}

// ReadPack is the pack at place, or the first word of why it was not read (PackMissing, or a refusal), with nothing
// of it: a pack is read whole or not at all, so no entry is ever given from one that does not match its sum or is of
// another format.
func (store *Store) ReadPack(place PackPlace) (Pack, string) {
	contents, readError := os.ReadFile(store.packPath(place))
	if errors.Is(readError, fs.ErrNotExist) {
		return Pack{}, PackMissing
	}
	if readError != nil {
		return Pack{}, PackUnreadable
	}
	if len(contents) < sha256.Size {
		return Pack{}, PackShort
	}
	body := contents[sha256.Size:]
	sum := sha256.Sum256(body)
	if !bytes.Equal(contents[:sha256.Size], sum[:]) {
		return Pack{}, PackSum
	}
	return decodePack(body)
}

// stampSize is one shard's stamp in a pack's bytes: the shard, then its stamp.
const stampSize = 1 + 8

// HasPack reports whether a pack is at place, whole or not, by a stat alone, so a caller learns it without reading the
// pack: false exactly where ReadPack would answer PackMissing, and true for a pack it would refuse, damaged or
// unreadable, since a refused pack is there (#s8e6z0n).
func (store *Store) HasPack(place PackPlace) bool {
	_, statError := os.Stat(store.packPath(place))
	return !errors.Is(statError, fs.ErrNotExist)
}

// decodePack is what body holds, a pack's contents after its sum, or why it was refused.
func decodePack(body []byte) (Pack, string) {
	if !bytes.HasPrefix(body, []byte(packMagic)) {
		return Pack{}, PackMalformed
	}
	rest := body[len(packMagic):]
	if len(rest) < 4 {
		return Pack{}, PackMalformed
	}
	if binary.BigEndian.Uint32(rest) != packFormatVersion {
		return Pack{}, PackVersion
	}
	rest = rest[4:]
	stampCount, stampCountLength := binary.Uvarint(rest)
	// A count past what the rest could hold is malformed, and never sizes the map.
	if stampCountLength <= 0 || stampCount > uint64(len(rest)-stampCountLength)/stampSize {
		return Pack{}, PackMalformed
	}
	rest = rest[stampCountLength:]
	pack := Pack{Stamps: make(map[byte]int64, stampCount)}
	for range stampCount {
		pack.Stamps[rest[0]] = int64(binary.BigEndian.Uint64(rest[1:stampSize]))
		rest = rest[stampSize:]
	}
	count, countLength := binary.Uvarint(rest)
	// Each entry is two keys at the least, so a count past what the rest could hold is malformed, and never sizes the
	// map.
	if countLength <= 0 || count > uint64(len(rest))/(2*uint64(len(Key{}))) {
		return Pack{}, PackMalformed
	}
	rest = rest[countLength:]
	pack.Entries = make(map[Key]PackEntry, count)
	for range count {
		if len(rest) < 2*len(Key{}) {
			return Pack{}, PackMalformed
		}
		var entry PackEntry
		copy(entry.Asked[:], rest)
		copy(entry.Stored[:], rest[len(Key{}):])
		rest = rest[2*len(Key{}):]
		length, lengthLength := binary.Uvarint(rest)
		if lengthLength <= 0 || length > uint64(len(rest)-lengthLength) {
			return Pack{}, PackMalformed
		}
		rest = rest[lengthLength:]
		// Clipped, so nothing appended to an entry writes over the next one in the file's buffer.
		entry.Entry = slices.Clip(rest[:length])
		rest = rest[length:]
		pack.Entries[entry.Asked] = entry
	}
	if len(rest) != 0 {
		return Pack{}, PackMalformed
	}
	return pack, ""
}

// WritePack keeps entries and stamps as the pack at place, the stamps in order of their shard and the entries in
// order of the key each was asked by, so the same pack makes the same file. It writes as Put does, a temporary file
// renamed into place, so two runs writing one pack at once leave one of theirs whole, never a mix.
func (store *Store) WritePack(place PackPlace, entries []PackEntry, stamps map[byte]int64) error {
	ordered := slices.SortedFunc(slices.Values(entries), func(left PackEntry, right PackEntry) int {
		return bytes.Compare(left.Asked[:], right.Asked[:])
	})
	size := len(packMagic) + 4 + 2*binary.MaxVarintLen64 + stampSize*len(stamps)
	for _, entry := range ordered {
		size += 2*len(Key{}) + binary.MaxVarintLen64 + len(entry.Entry)
	}
	body := make([]byte, 0, size)
	body = append(body, packMagic...)
	body = binary.BigEndian.AppendUint32(body, packFormatVersion)
	body = binary.AppendUvarint(body, uint64(len(stamps)))
	for _, shard := range slices.Sorted(maps.Keys(stamps)) {
		body = append(body, shard)
		body = binary.BigEndian.AppendUint64(body, uint64(stamps[shard]))
	}
	body = binary.AppendUvarint(body, uint64(len(ordered)))
	for _, entry := range ordered {
		body = append(body, entry.Asked[:]...)
		body = append(body, entry.Stored[:]...)
		body = binary.AppendUvarint(body, uint64(len(entry.Entry)))
		body = append(body, entry.Entry...)
	}
	return store.writeWhole(store.packPath(place), body)
}

// RemoveOtherIdentitiesPacks removes every pack of the store that runs of another executable identity than identity
// read, once a run for the store (ShardStamps), file by file and then its identity's directory: each new executable, a
// release or a rebuild, leaves every pack of the one before it where no run reads it again, about 21 MB at cohere's
// root (#b2t9xmf). The project's store calls it, which nothing else trims; the system cache's trim ages its packs out.
// A directory that is not an identity's, or a file in one that is neither a pack nor a pack's temporary file, is left
// alone. It removes nothing recursively, so a link or a directory where a pack goes stays, and keeps its identity's
// directory with it.
func (store *Store) RemoveOtherIdentitiesPacks(identity Key) {
	if !store.stamps.firstSweep(store.directory) {
		return
	}
	directory := filepath.Join(store.directory, packDirectoryName)
	// Ignored: a store with no packs directory has nothing to remove, and one that cannot be listed is swept by a
	// later run's, its packs never read meanwhile.
	identities, _ := os.ReadDir(directory)
	for _, other := range identities {
		if !other.IsDir() || !isKeyName(other.Name()) || other.Name() == identity.String() {
			continue
		}
		otherDirectory := filepath.Join(directory, other.Name())
		// Ignored: as above, a directory that cannot be listed is left for a later run.
		packs, _ := os.ReadDir(otherDirectory)
		for _, pack := range packs {
			if !pack.Type().IsRegular() || !isKeyName(strings.Split(pack.Name(), ".")[0]) {
				continue
			}
			// Ignored: a pack that cannot be removed is read by no run, and a later run's sweep tries it again.
			_ = os.Remove(filepath.Join(otherDirectory, pack.Name()))
		}
		// Ignored: a directory left holding what was not removed stays, and a later run's sweep tries it again.
		_ = os.Remove(otherDirectory)
	}
}

// isKeyName reports whether name is a key's hex, as every pack's name and every identity's directory is.
func isKeyName(name string) bool {
	decoded, decodeError := hex.DecodeString(name)
	return decodeError == nil && len(decoded) == len(Key{})
}

// MarkPackUsed tells a trim the pack at place is in use, as MarkUsed tells it of an entry: a run that read the
// system cache's pack calls it, so the trim counts the pack in the size cap and deletes it only once it has gone
// unread for five days (TrimSystemCache), as it does any entry. It changes the pack's time alone, never its
// directory's, and never a shard's (ShardStamps).
func (store *Store) MarkPackUsed(place PackPlace) {
	markUsed(store.packPath(place), time.Now())
}

// packPath is where the pack at place lives: `packs/<identity>/<name>`, each a key's hex.
func (store *Store) packPath(place PackPlace) string {
	return filepath.Join(store.directory, packDirectoryName, place.Identity.String(), place.Name.String())
}

// ShardOf is the shard key's file is in: its first byte, whose two hex characters name the directory (path).
func ShardOf(key Key) byte {
	return key[0]
}

// shardStamp is the modification time, in nanoseconds, of the directory holding shard's files, and false when there
// is none or it cannot be read. Every change the store makes to a file of the shard adds or removes a name there,
// Put's temporary file and its rename, a damaged file's removal (forget) and a trim's, so the time moves with each:
// the store never writes a file in place, and MarkUsed changes a file's time alone.
func (store *Store) shardStamp(shard byte) (int64, bool) {
	information, statError := os.Stat(filepath.Join(store.directory, hex.EncodeToString([]byte{shard})))
	if statError != nil || !information.IsDir() {
		return 0, false
	}
	return information.ModTime().UnixNano(), true
}
