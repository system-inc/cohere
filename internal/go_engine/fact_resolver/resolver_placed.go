package fact_resolver

import (
	"bytes"
	"cmp"
	"go/ast"
	"go/types"
	"iter"
	"maps"
	"slices"
	"sync"
)

// PlacedAnswer is one answer the run holds about a function a package declares, or one of its parameters, by place
// rather than by objectpath, so an answer about a function objectpath cannot name, an unexported one of the package's
// scope, has one too. Kind is the kind's name. File and Offset place the function's declaration as a build's unit is
// placed (build_dependence's newUnit): the file a `//line` directive names at the declaration's `func`, else the file
// as parsed, and the offset of that `func` in the file as parsed. Parameter is the parameter's index among the
// signature's parameters, its receiver not counted, or -1 for an answer about the function itself. Encoded is the
// answer as JSON, as Ask encodes one. A cache entry holds these as they are (check's cacheEntry), so a change to their
// shape is a change to its format.
type PlacedAnswer struct {
	Kind      string
	File      string
	Offset    int
	Parameter int
	Encoded   []byte
}

// PlacedIn is every answer the run holds about home's declared functions and their parameters, as Answered holds them,
// memoized or kept, each by its place (see PlacedAnswer). An answer about any other object, a result, a receiver, a
// variable or a type, or about a function none of home's files declares, is left out, which a reader that takes a
// missing answer as one that differs can afford. They are sorted by file, offset, parameter, kind, then the answer's
// bytes, so they come in the same order on every run.
//
// It reads home's own answers alone, never the run's, so a run that saves every package reads each answer once. An
// answer a question adds while it reads, as another package's whole asking of home's functions as the run saves, may
// be among them or not; either is an answer of home's key.
func (resolver *Resolver) PlacedIn(home *Package) []PlacedAnswer {
	home.answeredLock.Lock()
	keys := slices.Collect(maps.Keys(home.answered))
	home.answeredLock.Unlock()
	within := func(held *sync.Map) iter.Seq2[any, any] {
		return func(yield func(any, any) bool) {
			for _, key := range keys {
				if answer, isHeld := held.Load(key); isHeld && !yield(key, answer) {
					return
				}
			}
		}
	}
	places := home.declarationPlaces()
	var placed []PlacedAnswer
	for _, answer := range heldOnce(within(&resolver.answers), within(&resolver.kept)) {
		place, isPlaced := places[answer.Object]
		if !isPlaced {
			continue
		}
		placed = append(placed, PlacedAnswer{Kind: answer.Kind, File: place.file, Offset: place.offset,
			Parameter: place.parameter, Encoded: answer.Encoded})
	}
	slices.SortFunc(placed, ComparePlaced)
	return placed
}

// ComparePlaced orders two placed answers as PlacedIn sorts them: by file, offset, parameter, kind, then the answer's
// bytes.
func ComparePlaced(left PlacedAnswer, right PlacedAnswer) int {
	return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Offset, right.Offset),
		cmp.Compare(left.Parameter, right.Parameter), cmp.Compare(left.Kind, right.Kind),
		bytes.Compare(left.Encoded, right.Encoded))
}

// declarationPlace is where an answer about a declared function, or one of its parameters, is placed (see
// PlacedAnswer).
type declarationPlace struct {
	file      string
	offset    int
	parameter int
}

// declarationPlaces is the place of every function and method the package's files declare, and of each one's
// parameters, by object: a function's own at its declaration's `func`, as newUnit places its unit, with parameter -1,
// and each parameter's there too, with its index among the signature's parameters, the receiver not counted.
func (resolved *Package) declarationPlaces() map[types.Object]declarationPlace {
	places := map[types.Object]declarationPlace{}
	if resolved.info == nil {
		return places
	}
	for _, file := range resolved.files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			object, isDeclared := resolved.info.Defs[function.Name].(*types.Func)
			tokenFile := resolved.fileSet.File(function.Pos())
			if !isDeclared || tokenFile == nil {
				continue
			}
			place := declarationPlace{file: tokenFile.Position(function.Pos()).Filename,
				offset: tokenFile.Offset(function.Pos()), parameter: -1}
			places[object] = place
			parameters := object.Signature().Params()
			for index := range parameters.Len() {
				place.parameter = index
				places[parameters.At(index)] = place
			}
		}
	}
	return places
}
