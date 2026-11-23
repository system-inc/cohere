package fact_resolver

import (
	"bytes"
	"cmp"
	"go/ast"
	"go/token"
	"iter"
	"maps"
	"slices"
	"sync"

	"github.com/system-inc/go-types/types"
)

// PlacedAnswer is one answer the run holds about a function a package declares, or one of its parameters, by place
// rather than by objectpath, so an answer about a function objectpath cannot name, an unexported one of the package's
// scope, has one too. Kind is the kind's name. File and Offset place the function's declaration as a build's unit is
// placed (build_dependence's newUnit): the file a `//line` directive names at the declaration's `func`, else the file
// as parsed, and the offset of that `func` in the file as parsed. Parameter is the parameter's index among the
// signature's parameters, its receiver not counted, or -1 for an answer about the function itself. FileDigest is the
// sha256 of the bytes of the file the offset counts in, as the run parsed it, or nil when the run held none, so a
// reader whose file under the same name holds other bytes, where the offset may name another function, never takes
// the answer (#k7k3wwc). Encoded is the answer as JSON, as Ask encodes one. A cache entry holds these by column
// (check's storedPlaced), so a change to their shape is a change to its format.
type PlacedAnswer struct {
	Kind       string
	File       string
	FileDigest []byte
	Offset     int
	Parameter  int
	Encoded    []byte
}

// PlacedIn is every answer the run holds about home's declared functions and their parameters, as Answered holds them,
// memoized or kept, each by its place (see PlacedAnswer). An answer about any other object, a result, a receiver, a
// variable or a type, or about a function none of home's files declares, is left out, which a reader that takes a
// missing answer as one that differs can afford. Each answer's FileDigest is digestOf's of the file its function is
// parsed from, by the name the load parsed it under, nil where digestOf holds none. They are sorted as ComparePlaced
// orders them, so they come in the same order on every run.
//
// It reads home's own answers alone, never the run's, so a run that saves every package reads each answer once. An
// answer a question adds while it reads, as another package's whole asking of home's functions as the run saves, may
// be among them or not; either is an answer of home's key.
func (resolver *Resolver) PlacedIn(home *Package, digestOf func(parsed string) ([]byte, bool)) []PlacedAnswer {
	home.answeredLock.Lock()
	keys := slices.Collect(maps.Keys(home.answered))
	home.answeredLock.Unlock()
	within := func(held *sync.Map) iter.Seq2[answerKey, []byte] {
		return func(yield func(answerKey, []byte) bool) {
			for _, key := range keys {
				answer, isHeld := held.Load(key)
				if isHeld && !yield(key, encodeAnswer(key.kind, key.object, answer)) {
					return
				}
			}
		}
	}
	places := DeclarationPlaces(home.fileSet, home.files, home.info)
	var placed []PlacedAnswer
	for _, answer := range heldOnce(within(&resolver.answers), within(&resolver.kept)) {
		place, isPlaced := places[answer.Object]
		if !isPlaced {
			continue
		}
		answered := PlacedAnswer{Kind: answer.Kind.Name, File: place.File, Offset: place.Offset,
			Parameter: place.Parameter, Encoded: answer.Encoded}
		if parsed := home.fileSet.File(answer.Object.Pos()); parsed != nil {
			answered.FileDigest, _ = digestOf(parsed.Name())
		}
		placed = append(placed, answered)
	}
	slices.SortFunc(placed, ComparePlaced)
	return placed
}

// ComparePlaced orders two placed answers as PlacedIn sorts them: by file, the file's digest, offset, parameter, kind,
// then the answer's bytes.
func ComparePlaced(left PlacedAnswer, right PlacedAnswer) int {
	return cmp.Or(cmp.Compare(left.File, right.File), bytes.Compare(left.FileDigest, right.FileDigest),
		cmp.Compare(left.Offset, right.Offset),
		cmp.Compare(left.Parameter, right.Parameter), cmp.Compare(left.Kind, right.Kind),
		bytes.Compare(left.Encoded, right.Encoded))
}

// Place is where an answer about a function a package declares, or one of its parameters, is placed (see
// PlacedAnswer): File and Offset, the function's declaration's `func`, and Parameter, the parameter's index, -1 for
// the function itself.
type Place struct {
	File      string
	Offset    int
	Parameter int
}

// DeclarationPlaces is the place of every function and method files declare, and of each one's parameters, by object,
// as PlacedIn places an answer: a function's own at its declaration's `func`, as newUnit places its unit, with
// Parameter -1, and each parameter's there too, with its index among the signature's parameters, the receiver not
// counted. fileSet holds the files' positions and info their objects, as the load that parsed and checked them gives
// them; a nil info places nothing. A reader of the placed answers an entry holds finds each object's answer here.
//
// A place two functions share is no place: neither function is placed, nor any of their parameters, so an answer is
// never placed there and never found there, on either side, since PlacedIn and a reader both place through here.
// cmd/cgo writes `//line <user file>:N` inside _cgo_gotypes.go, so a function it generates there can share the user
// file's name and an offset with one the user wrote (#k7k3wwc), as build_dependence's file table knows.
func DeclarationPlaces(fileSet *token.FileSet, files []*ast.File, info *types.Info) map[types.Object]Place {
	places := map[types.Object]Place{}
	if info == nil {
		return places
	}
	// Each function's place, by its file and offset, with how many functions are declared there.
	type functionAt struct {
		file   string
		offset int
	}
	declaredAt := map[functionAt]int{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			object, isDeclared := info.Defs[function.Name].(*types.Func)
			tokenFile := fileSet.File(function.Pos())
			if !isDeclared || tokenFile == nil {
				continue
			}
			place := Place{File: tokenFile.Position(function.Pos()).Filename, Offset: tokenFile.Offset(function.Pos()),
				Parameter: -1}
			places[object] = place
			declaredAt[functionAt{file: place.File, offset: place.Offset}]++
			parameters := object.Signature().Params()
			for index := range parameters.Len() {
				place.Parameter = index
				places[parameters.At(index)] = place
			}
		}
	}
	for object, place := range places {
		if declaredAt[functionAt{file: place.File, offset: place.Offset}] > 1 {
			delete(places, object)
		}
	}
	return places
}
