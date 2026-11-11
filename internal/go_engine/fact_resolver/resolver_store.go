package fact_resolver

import (
	"encoding/json"
	"fmt"
	"go/types"
	"maps"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/types/objectpath"
)

// Store is where a run reads the answers earlier runs computed, and the answers the binary's standard-library table
// holds, for the packages it restored, which it holds no syntax of and cannot compute in (#w7zmvat wave 3). A package's
// answers depend only on its cache key, a digest of its files and of every key below it, so an answer stored under a
// package's key holds for any run in which the package has that key.
type Store interface {
	// Answers is what the store holds of the package whose types are packageTypes, or nil for none.
	Answers(packageTypes *types.Package) *Answers

	// Reloaded reports whether the run loaded the package whose types are packageTypes from source because an
	// earlier pass asked of it, restored, what no store answered. A question that reaches such a package unlearned
	// is a reload that failed.
	Reloaded(packageTypes *types.Package) bool
}

// Answers is what a store holds of one package: by kind's name, each answer by its object's objectpath, as JSON, and
// the kinds the package was computed whole in, the push's way, in which an object with no answer has no fact.
//
// Bodiless are the objectpaths of the functions and methods the package declares without a body, and of their
// parameters and results, and BodiesRecorded is whether the run that checked the package from source recorded them
// (#b2t9xmf). Export data cannot tell a function without a body from one with a body the run never loaded, so only a
// recorded Bodiless says that an object it does not name has a body.
type Answers struct {
	Values         map[string]map[objectpath.Path][]byte
	Complete       map[string]bool
	Bodiless       map[objectpath.Path]bool
	BodiesRecorded bool
}

// answerKey is one memoized answer: a kind's, about one object.
type answerKey struct {
	kind   *Kind
	object types.Object
}

// Ask is kind's answer about object, as the package that declares object holds it, which a fact on object would say:
// the zero Value for no fact. An object of no package, as the error interface's Error, and an instance's object, which
// no package declares, have none. Otherwise the answer is the store's when it holds one, the zero Value when the store
// holds the package computed whole and no answer for object, and else compute's, which runs in the package, learned,
// and is kept for the store. A package the run restored, with no answer for object in the store, is never answered
// no fact the run keeps: the package is among Reloads, so the run loads it from source and runs again, and the
// question is answered the zero Value as a placeholder, so the pass that asked runs on and asks everything else it
// would, and one round collects every package the pass needs. A placeholder is never memoized or stored, and the
// reader is unsure (see Unsure): an answer computed from one is unsure too, and so is every reader of that answer,
// so nothing that rests on a placeholder is kept, and the round is dropped.
//
// reader is the package whose analysis asks, the asking rule's package or the package a judgment runs in. An answer
// about another package's object is among reader's reads (see Reads), which its entry keeps, so a later run restoring
// reader by anything but its own first key can tell whether the answer still holds (#w7zmvat wave 3b).
//
// compute is the kind's own judgment in home, which takes home's lock itself. bodiless is the kind's answer about
// object from its types alone, for an object the store names as a function declared without a body, or a parameter or
// result of one, in a package the run restored: exactly what compute would answer were the package loaded, so the
// question is answered without loading it again (#b2t9xmf). It is an argument rather than a field of Kind, which knows
// no Value, so the compiler holds it to the type compute returns. Each answer is asked of compute, bodiless or the
// store once in a run, and memoized.
func Ask[Value any](
	resolver *Resolver,
	reader *Package,
	kind *Kind,
	object types.Object,
	compute func(home *Package) Value,
	bodiless func() Value,
) Value {
	var zero Value
	if object.Pkg() == nil || !isOrigin(object) {
		return zero
	}
	answer, unsure := answerOf(resolver, kind, object, compute, bodiless)
	if unsure {
		resolver.markUnsure(reader.Types)
		return answer
	}
	if resolver.store != nil && object.Pkg() != reader.Types {
		resolver.read(reader.Types, kind, object, func() []byte { return encodeAnswer(kind, object, answer) })
	}
	return answer
}

// answerOf is Ask's answer about object, an object of a package and its own origin: memoized, the store's, bodiless's
// for a restored package's object the store names as declared without a body, or compute's, and whether it is unsure:
// a placeholder for a package the run must reload, or computed from one, which compute's home was handed while it
// ran.
func answerOf[Value any](
	resolver *Resolver,
	kind *Kind,
	object types.Object,
	compute func(home *Package) Value,
	bodiless func() Value,
) (Value, bool) {
	key := answerKey{kind: kind, object: object}
	if memoized, held := resolver.answers.Load(key); held {
		answer, isValue := memoized.(Value)
		if !isValue {
			// Invariant: a kind asks with one Value type, which is what this function stores below.
			panic(fmt.Sprintf("fact_resolver: %s's answer about %s is a %T", kind.Name, objectName(object), memoized))
		}
		return answer, false
	}
	home := resolver.Package(object.Pkg())
	if resolver.store == nil {
		// A run with no store restored nothing, so every package below a question was learned; Owner fails loudly
		// on one that was not.
		home = resolver.Owner(object)
	}
	path, hasPath := resolver.pathOf(object)
	if hasPath && resolver.store != nil {
		if stored, held := resolver.stored(kind, object.Pkg(), path); held {
			var answer Value
			if len(stored) > 0 {
				if decodeError := json.Unmarshal(stored, &answer); decodeError != nil {
					// Invariant: the store holds what Ask encoded with the same Value type; bytes it cannot read
					// are a mistake in the store, never an answer of no fact.
					panic(fmt.Sprintf("fact_resolver: the stored %s answer about %s does not decode: %v",
						kind.Name, objectName(object), decodeError))
				}
			}
			resolver.storeHits.Add(1)
			resolver.answers.Store(key, answer)
			return answer, false
		}
	}
	if home == nil {
		// A restored package's object the store names as declared without a body is answered from its types, as a
		// computed answer is. An object it does not name, in a package whose bodies were recorded, has a body; one in
		// a package whose bodies were not may have one. Either way only its package, loaded, can answer.
		if hasPath && resolver.storedBodiless(object.Pkg(), path) {
			answer := bodiless()
			resolver.record(kind, object.Pkg(), path, encodeAnswer(kind, object, answer))
			resolver.answers.Store(key, answer)
			return answer, false
		}
		resolver.reload(kind, object)
		var placeholder Value
		return placeholder, true
	}
	// home's computations ask with home as the reader, so a placeholder anywhere below this one marks home unsure.
	// Once home is unsure, every answer computed in it is: a kind keeps the verdicts it computed in home, and one
	// made from a placeholder would answer a later question without asking again.
	answer := compute(home)
	if resolver.Unsure(home.Types) {
		return answer, true
	}
	if hasPath && resolver.store != nil {
		resolver.record(kind, object.Pkg(), path, encodeAnswer(kind, object, answer))
	}
	resolver.answers.Store(key, answer)
	return answer, false
}

// isOrigin reports whether object is its own origin, as a declared function or parameter is, and an instance's never.
func isOrigin(object types.Object) bool {
	switch object := object.(type) {
	case *types.Func:
		return object.Origin() == object
	case *types.Var:
		return object.Origin() == object
	default:
		return true
	}
}

// stored is the store's answer of kind at path in packageTypes, read through the store once per package: its JSON, or
// nothing, for no fact, where the package was computed whole in kind and holds no answer at path.
func (resolver *Resolver) stored(kind *Kind, packageTypes *types.Package, path objectpath.Path) ([]byte, bool) {
	answers := resolver.storedAnswersOf(packageTypes)
	if answers == nil {
		return nil, false
	}
	if value, answered := answers.Values[kind.Name][path]; answered {
		return value, true
	}
	return nil, answers.Complete[kind.Name]
}

// storedBodiless reports whether the store names the object at path in packageTypes as a function declared without a
// body, or a parameter or result of one, which only a store that recorded the package's bodies does.
func (resolver *Resolver) storedBodiless(packageTypes *types.Package, path objectpath.Path) bool {
	answers := resolver.storedAnswersOf(packageTypes)
	return answers != nil && answers.BodiesRecorded && answers.Bodiless[path]
}

// storedAnswersOf is the store's answers of packageTypes, read through the store once per package, or nil for none.
func (resolver *Resolver) storedAnswersOf(packageTypes *types.Package) *Answers {
	read, _ := resolver.storedAnswers.LoadOrStore(packageTypes, &storedRead{})
	held, isRead := read.(*storedRead)
	if !isRead {
		// Invariant: the map holds only what LoadOrStore stored above.
		panic(fmt.Sprintf("fact_resolver: %s's stored answers are a %T", packageTypes.Path(), read))
	}
	held.once.Do(func() { held.answers = resolver.store.Answers(packageTypes) })
	return held.answers
}

// storedRead is one package's answers as the store gave them, read once.
type storedRead struct {
	once    sync.Once
	answers *Answers
}

// record keeps encoded, the answer of kind at path in packageTypes, for the store.
func (resolver *Resolver) record(kind *Kind, packageTypes *types.Package, path objectpath.Path, encoded []byte) {
	resolver.recordedLock.Lock()
	defer resolver.recordedLock.Unlock()
	if resolver.recorded[packageTypes] == nil {
		resolver.recorded[packageTypes] = map[string]map[objectpath.Path][]byte{}
	}
	if resolver.recorded[packageTypes][kind.Name] == nil {
		resolver.recorded[packageTypes][kind.Name] = map[objectpath.Path][]byte{}
	}
	resolver.recorded[packageTypes][kind.Name][path] = encoded
}

// reload notes that a question about object, of kind, reached a package the run restored and no store answered: the
// package is among Reloads, so the run loads it from source and runs again, and Ask answers the question with a
// placeholder. A package the run already loaded from source for this, and still never learned, is a reload that
// failed: the question ends the pass that asked it, whose failure names the package. A probe's question ends the
// probe, which never reloads (see Probe).
func (resolver *Resolver) reload(kind *Kind, object types.Object) {
	if resolver.probing.Load() > 0 {
		// Invariant: a probe runs only inside Probe or WholeIfComplete, which recover this one value and answer that
		// they could not.
		panic(probeAbandoned{})
	}
	if resolver.store.Reloaded(object.Pkg()) {
		// Invariant: a kind's question returns no error, so one that has no answer ends the pass that asked it, which
		// the engine's recovering run turns into that pass's error (check's recoveringRun), never into an answer.
		panic(fmt.Sprintf("fact_resolver: asked %s of %s, of %s, which the run loaded from source to answer it and "+
			"still never learned, so it has no answer", kind.Name, objectName(object), object.Pkg().Path()))
	}
	resolver.reloads.Store(object.Pkg(), true)
}

// markUnsure notes that reader was handed an unsure answer, a placeholder or one computed from a placeholder.
func (resolver *Resolver) markUnsure(reader *types.Package) {
	count, _ := resolver.unsure.LoadOrStore(reader, new(atomic.Int64))
	held, isCount := count.(*atomic.Int64)
	if !isCount {
		// Invariant: the map holds only what LoadOrStore stored above.
		panic(fmt.Sprintf("fact_resolver: %s's unsure count is a %T", reader.Path(), count))
	}
	held.Add(1)
}

// unsureCount is how many unsure answers the package whose types are reader was handed so far.
func (resolver *Resolver) unsureCount(reader *types.Package) int64 {
	count, held := resolver.unsure.Load(reader)
	if !held {
		return 0
	}
	known, isCount := count.(*atomic.Int64)
	if !isCount {
		// Invariant: markUnsure stores only counts.
		panic(fmt.Sprintf("fact_resolver: %s's unsure count is a %T", reader.Path(), count))
	}
	return known.Load()
}

// Unsure reports whether the analysis of the package whose types are packageTypes was handed an unsure answer this
// run, a placeholder for a package the run must reload or an answer computed from one, so nothing it found or
// computed may be kept: its pass ran on a guess.
func (resolver *Resolver) Unsure(packageTypes *types.Package) bool {
	return resolver.unsureCount(packageTypes) > 0
}

// Reloads is every package a question reached restored, with no answer in the store, which the run must load from
// source and run again.
func (resolver *Resolver) Reloads() []*types.Package {
	var reloads []*types.Package
	resolver.reloads.Range(func(packageTypes any, _ any) bool {
		if reloaded, isPackage := packageTypes.(*types.Package); isPackage {
			reloads = append(reloads, reloaded)
		}
		return true
	})
	return reloads
}

// Recorded is every answer the run computed in the package whose types are packageTypes, and the store did not give
// it, by kind's name and objectpath, as JSON, for the store to keep under the package's key: a copy, which a question
// asked after does not change.
func (resolver *Resolver) Recorded(packageTypes *types.Package) map[string]map[objectpath.Path][]byte {
	resolver.recordedLock.Lock()
	defer resolver.recordedLock.Unlock()
	recorded := make(map[string]map[objectpath.Path][]byte, len(resolver.recorded[packageTypes]))
	for kind, answers := range resolver.recorded[packageTypes] {
		recorded[kind] = maps.Clone(answers)
	}
	return recorded
}

// StoreHits counts the answers the store gave the run, each computed in an earlier one.
func (resolver *Resolver) StoreHits() int64 {
	return resolver.storeHits.Load()
}

// Whole is every answer of every kind registered in home, computed whole, the push's way, as the kind's whole computes
// it (see Register), by kind's name and objectpath, as JSON, each answer of no fact left out: the answers a store holds
// of a package complete in those kinds. An answer whose object has no objectpath is left out too, since no other
// package can ask of it, and so is one about another package's object, or an instance's, which Ask never reads from a
// store.
func (resolver *Resolver) Whole(home *Package) map[string]map[objectpath.Path][]byte {
	whole := map[string]map[objectpath.Path][]byte{}
	for _, registered := range registeredKinds() {
		answers := map[objectpath.Path][]byte{}
		for object, encoded := range registered.whole(resolver, home) {
			if object.Pkg() != home.Types || !isOrigin(object) {
				continue
			}
			if path, pathError := objectpath.For(object); pathError == nil {
				answers[path] = encoded
			}
		}
		whole[registered.kind.Name] = answers
	}
	return whole
}

// WholeIfComplete is Whole, and whether it is: false when computing it would have to reach a package the run
// restored and no store answers, which it never reloads, as a probe never does, or when it rests on an answer the run
// was unsure of. It runs as the run saves, with no pass running.
func (resolver *Resolver) WholeIfComplete(home *Package) (whole map[string]map[objectpath.Path][]byte,
	complete bool) {
	resolver.probing.Add(1)
	defer resolver.probing.Add(-1)
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if _, abandoned := recovered.(probeAbandoned); !abandoned {
			panic(recovered)
		}
		whole, complete = nil, false
	}()
	whole = resolver.Whole(home)
	return whole, !resolver.Unsure(home.Types)
}

// registeredKind is one kind as it registered: the kind; its whole, each answer encoded as Ask encodes it; probe, its
// answer about one object of home, encoded, asked as Ask asks it; and zero, its answer of no fact, encoded.
type registeredKind struct {
	kind  *Kind
	whole func(resolver *Resolver, home *Package) map[types.Object][]byte
	probe func(resolver *Resolver, home *Package, object types.Object) []byte
	zero  []byte
}

// registered is every kind registered, in the order each registered, under registeredLock.
var (
	registeredLock sync.Mutex
	registered     []registeredKind
)

// Register adds kind to the kinds Whole computes and Probe asks: whole computes every answer of kind in a package,
// each answer of no fact left out, as Ask's compute would answer it, for every entry a run writes and the standard
// library's table, compute is the kind's own judgment of one object of home, as the compute it hands Ask, and
// bodiless its answer about an object declared without a body, as the bodiless it hands Ask. A kind registers as its
// package initializes.
func Register[Value any](
	kind *Kind,
	whole func(resolver *Resolver, home *Package) map[types.Object]Value,
	compute func(resolver *Resolver, home *Package, object types.Object) Value,
	bodiless func(object types.Object) Value,
) {
	var zero Value
	encodedZero, encodeError := json.Marshal(zero)
	if encodeError != nil {
		// Invariant: a kind's Value is plain data, a boolean or a slice of structs of integers, which JSON always
		// encodes.
		panic(fmt.Sprintf("fact_resolver: %s's answer of no fact does not encode: %v", kind.Name, encodeError))
	}
	registeredLock.Lock()
	defer registeredLock.Unlock()
	registered = append(registered, registeredKind{
		kind: kind,
		whole: func(resolver *Resolver, home *Package) map[types.Object][]byte {
			answers := whole(resolver, home)
			encoded := make(map[types.Object][]byte, len(answers))
			for object, answer := range answers {
				encoded[object] = encodeAnswer(kind, object, answer)
			}
			return encoded
		},
		probe: func(resolver *Resolver, home *Package, object types.Object) []byte {
			answer := Ask(resolver, home, kind, object, func(owner *Package) Value {
				return compute(resolver, owner, object)
			}, func() Value { return bodiless(object) })
			return encodeAnswer(kind, object, answer)
		},
		zero: encodedZero,
	})
}

// registeredKinds is every kind registered.
func registeredKinds() []registeredKind {
	registeredLock.Lock()
	defer registeredLock.Unlock()
	return append([]registeredKind{}, registered...)
}

// encodeAnswer is answer, of kind about object, as JSON, which is the same bytes in any process, so a table holding it
// is written the same however many processes computed it.
func encodeAnswer[Value any](kind *Kind, object types.Object, answer Value) []byte {
	encoded, encodeError := json.Marshal(answer)
	if encodeError != nil {
		// Invariant: a kind's Value is plain data, a boolean or a slice of structs of integers, which JSON always
		// encodes.
		panic(fmt.Sprintf("fact_resolver: %s's answer about %s does not encode: %v", kind.Name, objectName(object),
			encodeError))
	}
	return encoded
}
