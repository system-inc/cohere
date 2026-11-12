package check

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// ReadsRendererVersion is the version of what a persisted read's digests are taken of: the closure digest of an
// object (ReadsBuild.closureDigest, every rendering it is made of), the digest of what each kind of use reads of one
// (ReadsBuild.useDigest, #mjdf7ah), and the digest of a fact's value (factValueDigest).
// A persisted digest compares a rendering one binary made against one this binary makes, so the version enters every
// persisted digest, and a record's header names it: a record of another renderer is declined, never matched. It
// changes whenever either digest would change for the same types or the same fact, which the golden of a fixed
// fixture's renderings catches (testdata/reads_renderings.golden). The store's keys hold the executable's identity
// already, but the binary's table of standard-library entries outlives a store, so the version stands on its own.
const ReadsRendererVersion = "reads-renderer-2"

// persistedReads are one package's reads of other packages as an entry persists them (#b2t9xmf), so a build can judge
// a package the host restored rather than checked, whose reads were recorded by the run that wrote its entry. Each
// read is what identifies it, an owner's path and an objectpath, and, where the verdict compares a rendering, the
// digest of the host's, mixed with the renderer's version (persistedDigest). The reads are kept in the order the
// verdict asks them of a run's reads, so the first that fails is the same read, with the same reason.
//
// A reference to an owner or an object is its place in Owners or Objects counted from one, zero for none, so gob,
// which leaves out a zero, writes nothing for it.
type persistedReads struct {
	// Renderer is the ReadsRendererVersion the digests were taken under.
	Renderer string

	// Assembly is whether the package has assembly files, whose reads were digested whole (Restoring.Assembly).
	Assembly bool

	// WordSize and MaximumAlignment are the sizes the host's memo measured another package's unexported fields by
	// (typeGraph.measureBy), as measuredSizes reads them. A digest that reaches such a field holds its size and
	// alignment, so a build measured by other sizes never renders it alike, and the verdict declines the record as
	// unknown rather than compare it (persistedTypesHold).
	WordSize         int64
	MaximumAlignment int64

	// ClosureOnly is whether every read of the package is judged by its closure, as a package with assembly files is
	// and one whose reads say so (build_reads.Reads.ClosureOnly), each use persisted as a use of the whole.
	ClosureOnly bool

	// Owners are the paths of the packages the reads name, each once, and FactTypes the fact types, by factTypeName,
	// each once.
	Owners    []string
	FactTypes []string

	// Objects are the host's objects the reads name, each once.
	Objects []persistedObject

	// Uses are the uses the package's type check made of foreign objects, each with its kind, as Reads.Uses orders
	// them, and Selections the selections it made through foreign named types, as Reads.Selections orders them.
	Uses       []persistedUse
	Selections []persistedSelection

	// Facts are the fact reads, as Reads.Facts orders them.
	Facts []persistedFact

	// Lookups are the rules' lookups, as Reads.Lookups orders them.
	Lookups []persistedLookup
}

// persistedObject is one host object a read names: its package, by reference to Owners, zero for an object of no
// package; its objectpath there, empty when it has none; its name when the objectpath is not its name; and, for an
// object whose rendering the verdict compares, its persisted closure digest.
type persistedObject struct {
	Owner  int
	Path   objectpath.Path
	Name   string
	Digest []byte
}

// persistedUse is one use the package's type check made of a host object (build_reads.ObjectUse): the object, by
// reference to Objects, the kind of use, and for a kind but the whole, the persisted digest of what that kind reads of
// the object (ReadsBuild.useDigest). A whole use is judged by the object's closure digest, which Objects holds.
type persistedUse struct {
	Object int
	Kind   build_reads.UseKind
	Digest []byte
}

// persistedSelection is one selection through a foreign named type (build_reads.Selection): its receiver and the member
// its lookup found, by reference to Objects, the name looked up, the index path the host's lookup found it at, and the
// persisted digest of the member's own line. A selection of a package judged by closure alone keeps no digest of its
// own: it is judged by its receiver's and its member's closure digests, which Objects holds.
type persistedSelection struct {
	Receiver int
	Name     string
	Index    []int
	Selected int
	Digest   []byte
}

// persistedFact is one fact read: its owner, by reference to Owners; the object it is of, by reference to Objects,
// zero for a fact of the owner itself; its type, by place in FactTypes; whether it was found; and when found, the
// persisted digest of its value, or Unencodable when the value did not digest. A miss carries no digest.
type persistedFact struct {
	Owner       int
	Object      int
	FactType    int
	Found       bool
	Value       []byte
	Unencodable bool
}

// persistedLookup is one rule's lookup (build_reads.Lookup): its question, the package asked of, by reference to
// Owners, and the name; whether it found an answer, the answer, by reference to Objects, and its text. A Deprecated
// lookup's subject is by reference to Objects, and InstanceField marks a subject that is a field of an instance,
// which has no objectpath. An Interface or Method lookup keeps the host's type names with a method of the name in its
// import closure, sorted, each by reference to Objects, and NoSubject when it had no subject to find the closure by.
type persistedLookup struct {
	Query         string
	Path          int
	Name          string
	Found         bool
	Answer        int
	Text          string
	Subject       int
	InstanceField bool
	Candidates    []int
	NoSubject     bool
}

// persistedKey is digest mixed with version, the renderer's, as a persisted read compares it.
func persistedKey(version string, digest cache.Key) cache.Key {
	return cache.DigestOf([]byte(version), digest[:])
}

// persistedDigest is digest as a persisted read holds it, mixed with version (persistedKey).
func persistedDigest(version string, digest cache.Key) []byte {
	mixed := persistedKey(version, digest)
	return mixed[:]
}

// encodePersistedReads is record as the store keeps it.
func encodePersistedReads(record persistedReads) ([]byte, error) {
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(record); encodeError != nil {
		return nil, fmt.Errorf("encoding the persisted reads: %w", encodeError)
	}
	return encoded.Bytes(), nil
}

// decodePersistedReads is a record the store kept, or an error for bytes gob refuses.
func decodePersistedReads(contents []byte) (persistedReads, error) {
	var record persistedReads
	if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&record); decodeError != nil {
		return persistedReads{}, fmt.Errorf("decoding the persisted reads: %w", decodeError)
	}
	return record, nil
}

// buildReadsKey is where the store keeps the persisted reads of the package whose entry is under key, beside the entry
// and apart from every other key. They live apart from the entry, which every hit decodes, since only an extra build
// ever reads them, and since the run writes them only once the type reads are whole, after the entry is written.
func buildReadsKey(key cache.Key) cache.Key {
	return sha256.Sum256(append([]byte("build reads "), key[:]...))
}

// putBuildReads keeps record beside the entry under key, which must be in the store already, so a reader that finds
// the record always finds its entry.
func putBuildReads(store *cache.Store, key cache.Key, record persistedReads) error {
	if _, stored := store.Get(key); !stored {
		return fmt.Errorf("persisting reads under %s: the store holds no entry there", key)
	}
	encoded, encodeError := encodePersistedReads(record)
	if encodeError != nil {
		return encodeError
	}
	return store.Put(buildReadsKey(key), encoded)
}

// getBuildReads is the persisted reads beside the entry under key, and whether there are any: none when the store
// holds no entry there, whatever it holds beside it, nor a record gob refuses.
func getBuildReads(store *cache.Store, key cache.Key) (persistedReads, bool) {
	if _, stored := store.Get(key); !stored {
		return persistedReads{}, false
	}
	contents, stored := store.Get(buildReadsKey(key))
	if !stored {
		return persistedReads{}, false
	}
	record, decodeError := decodePersistedReads(contents)
	if decodeError != nil {
		return persistedReads{}, false
	}
	return record, true
}
