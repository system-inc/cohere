package fact_resolver

import (
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// declaredPackage is a package at path whose scope declares a function of no parameters for each of names, built from
// types alone, as a package the run restored is held: no syntax, nothing to compute in.
func declaredPackage(path string, names ...string) *types.Package {
	declared := types.NewPackage(path, path[strings.LastIndex(path, "/")+1:])
	for _, name := range names {
		declared.Scope().Insert(types.NewFunc(token.NoPos, declared, name,
			types.NewSignatureType(nil, nil, nil, nil, nil, false)))
	}
	declared.MarkComplete()
	return declared
}

// learnedPackage is packageTypes as the resolver holds a package ctrlflow finished on, stored there, with no syntax,
// which a test's compute never builds.
func learnedPackage(resolver *Resolver, packageTypes *types.Package) *Package {
	learned := &Package{resolver: resolver, Types: packageTypes}
	resolver.packages.Store(packageTypes, learned)
	return learned
}

// countingStore is a store holding answers by package, counting how many times a package's were read, and reloading
// nothing.
type countingStore struct {
	answers map[*types.Package]*Answers
	reads   atomic.Int64
}

func (store *countingStore) Answers(packageTypes *types.Package) *Answers {
	store.reads.Add(1)
	return store.answers[packageTypes]
}

func (*countingStore) Reloaded(*types.Package) bool {
	return false
}

// holdRegistered has the registry hold kinds alone until test ends, then what it held before. Only a test that does not
// run in parallel calls it: every kind of the process registers here, and a parallel test's Whole or Probe would read
// the swap.
func holdRegistered(test *testing.T, kinds ...registeredKind) {
	test.Helper()
	registeredLock.Lock()
	before := registered
	registered = kinds
	registeredLock.Unlock()
	test.Cleanup(func() {
		registeredLock.Lock()
		registered = before
		registeredLock.Unlock()
	})
}

// recoveredFrom is what calling run panicked with, nil when it returned.
func recoveredFrom(run func()) (recovered any) {
	defer func() { recovered = recover() }()
	run()
	return nil
}

// noBody is a bodiless answer a question about an object with a body never asks for.
func noBody() bool {
	return false
}

// A whole holds an answer only about an object of its own package that is its own origin, by the object's objectpath,
// as Ask reads a store only for such an object: home's whole kind answers about home's Own and about other's Shared,
// and the entry holds Own's answer alone, never Shared's under a path home's own Shared would have.
//
// The registry is the process's, which this test swaps, so it does not run in parallel.
func TestWholeKeepsOnlyObjectsOfItsPackage(test *testing.T) {
	home, other := declaredPackage("example.com/home", "Own"), declaredPackage("example.com/other", "Shared")
	kind := &Kind{Name: "whole_probe"}
	holdRegistered(test, registeredKind{kind: kind,
		whole: func(*Resolver, *Package) map[types.Object][]byte {
			return map[types.Object][]byte{home.Scope().Lookup("Own"): []byte("true"),
				other.Scope().Lookup("Shared"): []byte("false")}
		}})
	resolver := New(nil)
	whole := resolver.Whole(learnedPackage(resolver, home))
	want := map[string]map[objectpath.Path][]byte{kind.Name: {"Own": []byte("true")}}
	if !reflect.DeepEqual(whole, want) {
		test.Errorf("the whole is %q, want %q, home's own answer alone", whole, want)
	}
}

// A whole probe, as the run saves, that would reach a package the run restored and no store answers is abandoned, not
// reloaded: WholeIfComplete answers that it has no whole, schedules no reload and leaves home sure. Once it is done,
// an ordinary question reaching that package is a reload again, answered a placeholder, as a probe's never is.
//
// The registry is the process's, which this test swaps, so it does not run in parallel.
func TestAnAbandonedWholeProbeNeverSchedulesAReload(test *testing.T) {
	home, restored := declaredPackage("example.com/home", "Asks"), declaredPackage("example.com/restored", "Below")
	kind := &Kind{Name: "abandoned_probe"}
	below := restored.Scope().Lookup("Below")
	compute := func(*Package) bool { return true }
	holdRegistered(test, registeredKind{kind: kind,
		whole: func(resolver *Resolver, home *Package) map[types.Object][]byte {
			return map[types.Object][]byte{below: encodeAnswer(kind, below, Ask(resolver, home, kind, below, compute,
				noBody))}
		}})
	resolver := New(&countingStore{})
	learned := learnedPackage(resolver, home)
	var whole map[string]map[objectpath.Path][]byte
	var complete bool
	if recovered := recoveredFrom(func() { whole, complete = resolver.WholeIfComplete(learned) }); recovered != nil {
		test.Fatalf("the abandoned probe panicked with %v, want it recovered", recovered)
	}
	if whole != nil || complete {
		test.Errorf("the abandoned probe gave %q, complete %t, want no whole", whole, complete)
	}
	if reloads := resolver.Reloads(); len(reloads) != 0 || resolver.Unsure(home) {
		test.Errorf("the abandoned probe scheduled reloads %v and left home unsure %t, want neither", reloads,
			resolver.Unsure(home))
	}
	var answer bool
	recovered := recoveredFrom(func() { answer = Ask(resolver, learned, kind, below, compute, noBody) })
	if recovered != nil {
		test.Fatalf("an ordinary question after the probe panicked with %v, want a reload", recovered)
	}
	if reloads := resolver.Reloads(); answer || len(reloads) != 1 || reloads[0] != restored || !resolver.Unsure(home) {
		test.Errorf("an ordinary question after the probe answered %t with reloads %v and home unsure %t, want a "+
			"placeholder, restored reloaded and home unsure", answer, reloads, resolver.Unsure(home))
	}
}

// A whole that fails for any reason but a probe's abandonment fails WholeIfComplete as it would anything, so the
// failure is seen rather than read as a whole the run could not have.
//
// The registry is the process's, which this test swaps, so it does not run in parallel.
func TestAWholeThatFailsOtherwiseIsNotSwallowed(test *testing.T) {
	home := declaredPackage("example.com/home", "Fails")
	holdRegistered(test, registeredKind{kind: &Kind{Name: "failing_whole"},
		whole: func(*Resolver, *Package) map[types.Object][]byte {
			panic("the kind's whole failed")
		}})
	resolver := New(nil)
	learned := learnedPackage(resolver, home)
	if recovered := recoveredFrom(func() { resolver.WholeIfComplete(learned) }); recovered != "the kind's whole failed" {
		test.Errorf("a whole that failed otherwise recovered %v from WholeIfComplete, want its own failure", recovered)
	}
}

// A stored answer whose bytes do not decode fails the question loudly, never reads as the zero Value, a certain no
// fact that could hide a difference.
func TestAStoredAnswerThatDoesNotDecodeIsRefused(test *testing.T) {
	test.Parallel()
	restored := declaredPackage("example.com/restored", "Corrupt")
	kind := &Kind{Name: "corrupt_probe"}
	resolver := New(&countingStore{answers: map[*types.Package]*Answers{restored: {
		Values: map[string]map[objectpath.Path][]byte{kind.Name: {"Corrupt": []byte("!")}}}}})
	reader := learnedPackage(resolver, declaredPackage("example.com/reader"))
	recovered := recoveredFrom(func() {
		Ask(resolver, reader, kind, restored.Scope().Lookup("Corrupt"), func(*Package) bool { return true }, noBody)
	})
	if message, isMessage := recovered.(string); !isMessage || !strings.Contains(message, "does not decode") {
		test.Errorf("the corrupt stored answer recovered %v, want a failure to decode", recovered)
	}
}

// A package's stored answers are read through the store once, however many questions are asked of its objects.
func TestDistinctQuestionsReadOneStoreEntry(test *testing.T) {
	test.Parallel()
	restored := declaredPackage("example.com/restored", "First", "Second")
	kind := &Kind{Name: "read_once_probe"}
	store := &countingStore{answers: map[*types.Package]*Answers{restored: {
		Values: map[string]map[objectpath.Path][]byte{kind.Name: {"First": []byte("true"), "Second": []byte("true")}}}}}
	resolver := New(store)
	reader := learnedPackage(resolver, declaredPackage("example.com/reader"))
	for _, name := range []string{"First", "Second"} {
		if !Ask(resolver, reader, kind, restored.Scope().Lookup(name), func(*Package) bool { return false }, noBody) {
			test.Errorf("%s is answered no fact, want the store's true", name)
		}
	}
	if reads := store.reads.Load(); reads != 1 {
		test.Errorf("the store was read %d times, want once", reads)
	}
}

// An answer about a restored package's function the store names as declared without a body is taken from its types
// once and held as every answer is: asked again, it is the held answer, and the run's answers hold it (Answered), as
// the facts' reach reads them.
func TestABodilessAnswerIsHeldLikeAnyOther(test *testing.T) {
	test.Parallel()
	restored := declaredPackage("example.com/restored", "Assembly")
	kind := &Kind{Name: "bodiless_probe"}
	resolver := New(&countingStore{answers: map[*types.Package]*Answers{restored: {
		Bodiless: map[objectpath.Path]bool{"Assembly": true}, BodiesRecorded: true}}})
	// A learned package makes the program Answered reads; this one is never built.
	resolver.program = intermediate_representation.NewProgram(token.NewFileSet())
	reader := learnedPackage(resolver, declaredPackage("example.com/reader"))
	assembly := restored.Scope().Lookup("Assembly")
	bodilessAsked := 0
	for range 2 {
		if !Ask(resolver, reader, kind, assembly, func(*Package) bool { return false }, func() bool {
			bodilessAsked++
			return true
		}) {
			test.Error("Assembly is answered no fact, want its bodiless answer, true")
		}
	}
	if bodilessAsked != 1 {
		test.Errorf("the bodiless answer was asked %d times, want once", bodilessAsked)
	}
	var held []string
	for _, answer := range resolver.Answered() {
		held = append(held, answer.Kind.Name+" "+answer.Object.Name()+" "+string(answer.Encoded))
	}
	if want := []string{"bodiless_probe Assembly true"}; !reflect.DeepEqual(held, want) {
		test.Errorf("the run holds the answers %q, want %q", held, want)
	}
}

// An answer computed in a package left unsure while it computed is the one compute gave, unsure, so the pass that
// asked runs on as it would on that answer and asks everything else it would: Calls, whose compute reaches restored's
// Below and is handed a placeholder, answers true as computed, and its reader is unsure.
func TestAnUnsureComputedAnswerIsTheOneComputed(test *testing.T) {
	test.Parallel()
	home, restored := declaredPackage("example.com/home", "Calls"), declaredPackage("example.com/restored", "Below")
	kind := &Kind{Name: "unsure_probe"}
	resolver := New(&countingStore{})
	learned := learnedPackage(resolver, home)
	reader := learnedPackage(resolver, declaredPackage("example.com/reader"))
	answer := Ask(resolver, reader, kind, home.Scope().Lookup("Calls"), func(owner *Package) bool {
		Ask(resolver, owner, kind, restored.Scope().Lookup("Below"), func(*Package) bool { return true }, noBody)
		return true
	}, noBody)
	if !answer || !resolver.Unsure(reader.Types) || !resolver.Unsure(learned.Types) {
		test.Errorf("Calls is answered %t with its reader unsure %t and home unsure %t, want true and both unsure",
			answer, resolver.Unsure(reader.Types), resolver.Unsure(learned.Types))
	}
}
