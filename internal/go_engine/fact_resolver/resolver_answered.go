package fact_resolver

import (
	"bytes"
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"slices"
)

// Answered is one answer a run holds: the kind's name, the object the answer is about, and the answer, as JSON, as Ask
// encodes one, whose zero Value is no fact.
type Answered struct {
	Kind    string
	Object  types.Object
	Encoded []byte
}

// Answered is every answer the run holds, by kind and object: the memo Ask reads, the store's answers, a test's hand's
// and the computed ones, and with them the answers a kind kept of what it judged inside another judgment (see Keep),
// which no question asked of. An answer both kept and memoized is held once, and Answered panics when the two differ.
// Recorded cannot serve a reader that needs them all (#kemy8sq). It holds an answer by its objectpath, and objectpath
// names only what another package can reach, so an unexported function of a package's scope has no path and no record.
// And it holds only what the run computed through Ask and its store did not give it, so nothing a store answered,
// nothing a kind kept, and nothing at all in a run without a store. An unsure answer is never among them, a placeholder
// or one computed in an unsure package, since nothing unsure is memoized or kept.
//
// It is read once the run's passes are done, when no question is adding to what the run holds. The answers are sorted
// by the object's package path, then by where the object is declared, its file, line and column, then by kind, then
// by the answer's bytes, so they come in the same order on every run, whatever order the questions were asked in and
// the load parsed the files in. A place is used rather than a position, which counts from wherever the load happened
// to add the file.
//
// Each answer is encoded here from the value held, as an any, and the bytes are the ones encodeAnswer gave for the
// kind's typed value when it recorded it or told a hand of it: encodeAnswer hands json.Marshal its value as an any
// either way, and an any keeps the value's own type, so a nil slice, no fact, encodes as null from both.
func (resolver *Resolver) Answered() []Answered {
	resolver.mutex.Lock()
	program := resolver.program
	resolver.mutex.Unlock()
	if program == nil {
		// No package was learned, so no question was asked: a question is asked by a learned package's reader.
		return nil
	}
	var answers []Answered
	places := map[types.Object]token.Position{}
	memoized := map[answerKey][]byte{}
	keyOf := func(key any) answerKey {
		answered, isKey := key.(answerKey)
		if !isKey {
			// Invariant: answerOf and Keep store every answer under an answerKey.
			panic(fmt.Sprintf("fact_resolver: an answer is held under a %T", key))
		}
		return answered
	}
	hold := func(answered answerKey, encoded []byte) {
		answers = append(answers, Answered{Kind: answered.kind.Name, Object: answered.object, Encoded: encoded})
		places[answered.object] = program.FileSet.Position(answered.object.Pos())
	}
	resolver.answers.Range(func(key any, answer any) bool {
		answered := keyOf(key)
		memoized[answered] = encodeAnswer(answered.kind, answered.object, answer)
		hold(answered, memoized[answered])
		return true
	})
	// A kept answer a question was asked of afterwards is memoized too, and is held once. It is the same answer, since
	// a kept answer is final (see Keep), and so the same bytes.
	resolver.kept.Range(func(key any, answer any) bool {
		answered := keyOf(key)
		encoded := encodeAnswer(answered.kind, answered.object, answer)
		held, isMemoized := memoized[answered]
		if !isMemoized {
			hold(answered, encoded)
			return true
		}
		if !bytes.Equal(held, encoded) {
			// Invariant: a kind keeps only a final answer, the one its compute answers from then on, so a question
			// asked afterwards memoizes the same; another is a mistake in the kind, which kept a verdict it revised.
			panic(fmt.Sprintf("fact_resolver: %s's answer about %s was kept as %s and memoized as %s",
				answered.kind.Name, objectName(answered.object), encoded, held))
		}
		return true
	})
	slices.SortFunc(answers, func(left Answered, right Answered) int {
		return cmp.Or(cmp.Compare(left.Object.Pkg().Path(), right.Object.Pkg().Path()),
			cmp.Compare(places[left.Object].Filename, places[right.Object].Filename),
			cmp.Compare(places[left.Object].Line, places[right.Object].Line),
			cmp.Compare(places[left.Object].Column, places[right.Object].Column),
			cmp.Compare(left.Kind, right.Kind), bytes.Compare(left.Encoded, right.Encoded))
	})
	return answers
}
