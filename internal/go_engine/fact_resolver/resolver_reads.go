package fact_resolver

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/types/objectpath"
)

// Read is one answer a package's analysis took from another package (#w7zmvat wave 3b): the kind, the package that
// declares the object, the object's objectpath there, and the answer, as JSON, whose zero Value is no fact. A run that
// restores the reading package by anything but its own first key holds its findings and answers only while every read
// still holds for its owner as the run finds it.
type Read struct {
	Kind  string
	Owner *types.Package
	Path  objectpath.Path
	Value []byte
}

// readSet is one package's reads in a run, by kind and object, and whether one of them was of an object with no
// objectpath, which no entry can name, so the set cannot be checked whole.
type readSet struct {
	reads      map[answerKey]Read
	incomplete bool
}

// probeAbandoned is what a probe's question panics with when it reaches a package the run restored and no store
// answers, which Probe recovers as an answer it could not have, never a reload.
type probeAbandoned struct{}

// objectPath is an object's objectpath, memoized, and whether it has one.
type objectPath struct {
	path    objectpath.Path
	hasPath bool
}

// pathOf is object's objectpath within its package, and whether it has one, computed once per object in a run.
func (resolver *Resolver) pathOf(object types.Object) (objectpath.Path, bool) {
	if memoized, held := resolver.paths.Load(object); held {
		known, isPath := memoized.(objectPath)
		if !isPath {
			// Invariant: the map holds only what this function stores below.
			panic(fmt.Sprintf("fact_resolver: %s's objectpath is held as a %T", objectName(object), memoized))
		}
		return known.path, known.hasPath
	}
	path, pathError := objectpath.For(object)
	known := objectPath{path: path, hasPath: pathError == nil}
	resolver.paths.Store(object, known)
	return known.path, known.hasPath
}

// read adds the answer of kind about object, another package's, to reader's reads, encoding it only the first time.
func (resolver *Resolver) read(reader *types.Package, kind *Kind, object types.Object, encode func() []byte) {
	key := answerKey{kind: kind, object: object}
	resolver.readsLock.Lock()
	set := resolver.reads[reader]
	if set == nil {
		set = &readSet{reads: map[answerKey]Read{}}
		resolver.reads[reader] = set
	}
	_, held := set.reads[key]
	resolver.readsLock.Unlock()
	if held {
		return
	}
	path, hasPath := resolver.pathOf(object)
	encoded := encode()
	resolver.readsLock.Lock()
	defer resolver.readsLock.Unlock()
	if !hasPath {
		set.incomplete = true
		return
	}
	set.reads[key] = Read{Kind: kind.Name, Owner: object.Pkg(), Path: path, Value: encoded}
}

// Reads is every answer the analysis of the package whose types are reader took from another package in this run,
// sorted by owner, kind and objectpath, and whether the set is whole: false when a read was of an object no entry can
// name.
func (resolver *Resolver) Reads(reader *types.Package) ([]Read, bool) {
	resolver.readsLock.Lock()
	defer resolver.readsLock.Unlock()
	set := resolver.reads[reader]
	if set == nil {
		return nil, true
	}
	reads := slices.Collect(maps.Values(set.reads))
	slices.SortFunc(reads, func(left Read, right Read) int {
		return cmp.Or(cmp.Compare(left.Owner.Path(), right.Owner.Path()), cmp.Compare(left.Kind, right.Kind),
			cmp.Compare(left.Path, right.Path))
	})
	return reads, !set.incomplete
}

// Probe is the answer of the kind named kindName about the object at path in owner, a package the run learned, as Ask
// answers it there, encoded as a read holds it, and whether the run could have it now: not for a package the run never
// learned, a path that names nothing, a kind not registered, or an answer that would have to reach a package the run
// restored and no store answers, which a probe never reloads. It runs between the analysis's waves, with no pass
// running, so the one flag it sets reaches only its own questions.
func (resolver *Resolver) Probe(kindName string, owner *types.Package, path objectpath.Path) (encoded []byte,
	answered bool) {
	home := resolver.Package(owner)
	if home == nil {
		return nil, false
	}
	object, objectError := objectpath.Object(owner, path)
	if objectError != nil {
		return nil, false
	}
	var probe func(resolver *Resolver, home *Package, object types.Object) []byte
	for _, registered := range registeredKinds() {
		if registered.kind.Name == kindName {
			probe = registered.probe
		}
	}
	if probe == nil {
		return nil, false
	}
	resolver.probing.Store(true)
	defer resolver.probing.Store(false)
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if _, abandoned := recovered.(probeAbandoned); !abandoned {
			panic(recovered)
		}
		encoded, answered = nil, false
	}()
	return probe(resolver, home, object), true
}

// ZeroAnswer is the answer of no fact of the kind named kindName, encoded as a read holds it, and whether such a kind
// is registered.
func ZeroAnswer(kindName string) ([]byte, bool) {
	for _, registered := range registeredKinds() {
		if registered.kind.Name == kindName {
			return registered.zero, true
		}
	}
	return nil, false
}
