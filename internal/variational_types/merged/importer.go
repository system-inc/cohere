// Package merged is the spike's importer (#yraxcp9): every package the variational check doesn't check from source
// (the standard library and third-party modules) comes through it. It reads each build's export data, through that
// build's Source, into the build's own universe, then merges the builds into one universe of the fork's objects, one
// object per declaration with its properties by build. A declared named type is one *types.Named across builds, and
// only its underlying type varies; a package-level constant, variable or function keeps one identity, and its type or
// value varies; an object declared in only some builds says which.
//
// The importer is safe for many goroutines at once. A merged object already merged is looked up without a lock. A
// merge runs under the importer's lock, so there is one merge of each object and a lookup of an object being merged
// waits on it, and each build is read behind a lock of its own, the builds one object needs read at once.
package merged

import (
	"go/token"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/system-inc/go-types/types"
)

// Importer merges every build's export data into one universe, a package at a time as the check imports them.
type Importer struct {
	fileSet *token.FileSet
	all     types.Mask
	builds  []*build // one for each build in all

	// mutex is the merge's lock. Every field below is held under it, and a merge runs whole while holding it. Merges
	// of different objects don't run at once: one merge's types reach another's (a field's type, an embedded
	// interface), and its end compares and completes them, which a merge still running elsewhere could be writing.
	mutex sync.Mutex

	packages map[string]*types.Package // the merged universe, by path
	checked  map[string]*types.Package // packages checked from source, which stand in for the export data
	objects  map[objectKey]types.Object
	entries  map[objectKey]*lazyEntry // each merged package's lazy entries, by the name each holds
	locals   map[localKey]*namedRecord
	aliases  map[*types.TypeName]*aliasRecord
	context  *types.Context // instances are shared across builds through it

	// resolving are the objects whose merge is running, so one reached again before its merge has kept it fails
	// loudly instead of waiting forever on its own lazy entry.
	resolving map[objectKey]bool

	shortfalls Shortfalls
	failures   []Failure
	failed     map[failureKey]bool
	decodeWait time.Duration // the merge's wall time waiting on the builds' reads
	merge      time.Duration // the merge's wall time less its waits on reads
}

// Shortfalls counts what the merge could not keep exactly, for the spike's report.
type Shortfalls struct {
	// KindConflicts are names declared as different kinds of object in different builds. The first build's kind is
	// kept, and the builds that declare the name as another kind read it as absent.
	KindConflicts int
	// UnboundSignatures are a generic function's or method's signatures that differ by build: a build whose
	// signature differs from the first build's reads the first's type parameters, unbound, since a type parameter can
	// be bound by one signature alone.
	UnboundSignatures int
	// StrayTypeParameters are type parameters reached outside every declaration that binds them, each made anew
	// without a binding.
	StrayTypeParameters int
	// GenericAliasVariants are generic aliases whose right-hand side differs by build, kept as the first build's.
	GenericAliasVariants int
	// MergedTypeSets are interfaces embedding others whose type sets weren't given as their builds have them (a
	// constraint's type terms), each completed from the merged view, which outside every unit reads an embedded
	// interface that differs by build as its first build declares it.
	MergedTypeSets int
}

// Timing is how long the importer has spent so far.
type Timing struct {
	// Decode is the time the builds' sources spent reading export data, summed over the builds, as the merge read
	// them and as their plain importers did.
	Decode time.Duration
	// DecodeWait is the merge's wall time waiting on the builds' reads, the builds one object needs read at once.
	DecodeWait time.Duration
	// Merge is the merge's wall time less its waits on reads.
	Merge time.Duration
}

// objectKey names a package-level object across builds.
type objectKey struct {
	path string
	name string
}

// localKey names a function-local named type across builds: its package, name and where it is declared.
type localKey struct {
	path     string
	name     string
	position string
}

// lazyEntry is a merged package's lazy entry for one name. made is the merged object once merged, so an entry resolved
// after its object was merged (reached from another's type) gives it without the importer's lock.
type lazyEntry struct {
	made atomic.Pointer[madeObject]
}

// madeObject holds a merged object for an atomic pointer.
type madeObject struct{ object types.Object }

// New makes an importer over builds: sources[b] reads build b's export data, for each build b in all.
func New(fileSet *token.FileSet, sources []Source, all types.Mask) *Importer {
	importer := &Importer{
		fileSet:  fileSet,
		all:      all,
		packages: map[string]*types.Package{},
		checked:  map[string]*types.Package{},
		objects:  map[objectKey]types.Object{},
		entries:  map[objectKey]*lazyEntry{},
		locals:   map[localKey]*namedRecord{},
		aliases:  map[*types.TypeName]*aliasRecord{},
		context:  types.NewContext(),

		resolving: map[objectKey]bool{},
		failed:    map[failureKey]bool{},
	}
	for index, source := range sources {
		mask := types.Mask(1) << index
		if all&mask == 0 {
			continue
		}
		importer.builds = append(importer.builds, &build{
			index:      index,
			mask:       mask,
			source:     source,
			translated: map[types.Type]types.Type{},
		})
	}
	return importer
}

// Import is path's package in the merged universe. Its scope holds a lazy entry for every name any build declares,
// and an object is merged across the builds that compile path the first time it is looked up. A build whose export
// data for path can't be read is left out, its failure kept, and the import fails only when no build could read it.
func (importer *Importer) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	importer.mutex.Lock()
	defer importer.mutex.Unlock()
	if checked := importer.checked[path]; checked != nil {
		return checked, nil
	}
	if merged := importer.packages[path]; merged != nil && merged.Complete() {
		return merged, nil
	}
	var importedPackage *types.Package
	var importError error
	importer.resolve(func(resolution *resolution) { importedPackage, importError = resolution.mergePackage(path) })
	return importedPackage, importError
}

// resolve runs work, a merge the check asked for, under the importer's lock, outside every unit of the variational
// check, since a split's panic in the middle of a merge would leave it half done, and ends the merge. A merge can reach
// other objects (a type's underlying type reaching another package's object), which it merges as its own work and
// leaves their comparisons to its end.
//
// Requires: the importer's lock is held.
func (importer *Importer) resolve(work func(*resolution)) {
	types.Outside(func() {
		started := time.Now()
		waitBefore := importer.decodeWait
		defer func() {
			importer.merge += time.Since(started) - (importer.decodeWait - waitBefore)
		}()
		resolution := &resolution{importer: importer}
		work(resolution)
		resolution.finish()
		for _, placed := range resolution.placed {
			placed.entry.made.Store(&madeObject{object: placed.made})
		}
	})
}

// Add makes checked, a package checked from source, the answer Import gives for its path.
func (importer *Importer) Add(checked *types.Package) {
	importer.mutex.Lock()
	defer importer.mutex.Unlock()
	importer.checked[checked.Path()] = checked
}

// Plain is build b's own export data, unmerged, for the plain baseline. It shares build b's universe with the merge,
// so each package is decoded once whichever reads it first, and as the merge does it reads each object the first
// time the check looks it up, the time counted in Timing's decode. A plain check reads build b's types without its
// lock, so it runs while nothing merges.
func (importer *Importer) Plain(build int) types.Importer {
	for _, candidate := range importer.builds {
		if candidate.index == build {
			return plainImporter{build: candidate}
		}
	}
	return plainImporter{}
}

// Timing is the time spent reading export data and the time spent merging what was read.
func (importer *Importer) Timing() Timing {
	importer.mutex.Lock()
	defer importer.mutex.Unlock()
	timing := Timing{DecodeWait: importer.decodeWait, Merge: importer.merge}
	for _, build := range importer.builds {
		build.mutex.Lock()
		timing.Decode += build.source.Elapsed()
		build.mutex.Unlock()
	}
	return timing
}

// Shortfalls counts what the merge could not keep exactly so far.
func (importer *Importer) Shortfalls() Shortfalls {
	importer.mutex.Lock()
	defer importer.mutex.Unlock()
	return importer.shortfalls
}

// Failures are the builds' export data the merge couldn't read so far, each once.
func (importer *Importer) Failures() []Failure {
	importer.mutex.Lock()
	defer importer.mutex.Unlock()
	return slices.Clone(importer.failures)
}

// noteFailure keeps build's failure to read export data while merging key, unless it is kept already.
//
// Requires: the importer's lock is held.
func (importer *Importer) noteFailure(build *build, key objectKey, failure error) {
	seen := failureKey{build: build.index, path: key.path, message: failure.Error()}
	if importer.failed[seen] {
		return
	}
	importer.failed[seen] = true
	importer.failures = append(importer.failures, Failure{Builds: build.mask, Path: key.path, Name: key.name,
		Cause: failure})
}

// mergedPackage is the merged universe's package of path, made empty when it is first reached.
//
// Requires: the importer's lock is held.
func (importer *Importer) mergedPackage(path string, name string) *types.Package {
	if path == "unsafe" {
		return types.Unsafe
	}
	if checked := importer.checked[path]; checked != nil {
		return checked
	}
	if merged := importer.packages[path]; merged != nil {
		return merged
	}
	merged := types.NewPackage(path, name)
	importer.packages[path] = merged
	return merged
}

// mergedPackageOf is the merged universe's package of fromPackage, a build's package or a merged one, or nil for the
// universe scope.
//
// Requires: the importer's lock is held.
func (importer *Importer) mergedPackageOf(fromPackage *types.Package) *types.Package {
	if fromPackage == nil {
		return nil
	}
	return importer.mergedPackage(fromPackage.Path(), fromPackage.Name())
}

// resolver is the lazy entry of key, a merged package's name: the first Lookup merges the object, outside every unit,
// unless it was merged already, reached from another's type. An object no build could read resolves to an invalid
// one, never to nil.
func (importer *Importer) resolver(entry *lazyEntry, key objectKey) func() types.Object {
	return func() types.Object {
		if made := entry.made.Load(); made != nil {
			return made.object
		}
		importer.mutex.Lock()
		defer importer.mutex.Unlock()
		var made types.Object
		importer.resolve(func(resolution *resolution) { made = resolution.objectAt(key) })
		if made == nil {
			made = invalidObject(importer.packages[key.path], key.name)
			importer.objects[key] = made
			entry.made.Store(&madeObject{object: made})
		}
		return made
	}
}

// invalidObject stands in for name of objectPackage when no build could read it: a variable of the invalid type,
// which the check reads as an invalid operand.
func invalidObject(objectPackage *types.Package, name string) types.Object {
	return types.NewVar(token.NoPos, objectPackage, name, types.Typ[types.Invalid])
}
