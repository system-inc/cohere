package check

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"sync"
)

// answeredFromWhole is the hand on a variant run's answers: every question about an object of a variant is answered
// with the whole run's answer about the object at its place, in the package variants names by the variant's types,
// and a question the whole run never answered is a miss, which the test fails on, since the variant would compute it
// from stubs. The whole run has finished, so its answers are only read.
type answeredFromWhole struct {
	whole    map[heldAnswerKey][]byte
	variants map[*types.Package]heldVariant

	lock   sync.Mutex
	misses []string
}

func (held *answeredFromWhole) Answer(kindName string, object types.Object) ([]byte, bool) {
	variant, isVariant := held.variants[object.Pkg()]
	if !isVariant {
		return nil, false
	}
	at, isPlaced := positionAt(variant.fileSet, object.Pos())
	if !isPlaced {
		held.miss(fmt.Sprintf("%s about %s, which has no place to find the whole run's answer by", kindName,
			object.Name()))
		return nil, false
	}
	encoded, isHeld := held.whole[heldAnswerKey{packageIdentifier: variant.originalIdentifier, kind: kindName, at: at}]
	if !isHeld {
		held.miss(fmt.Sprintf("%s about %s at %s:%d, which the whole run never answered", kindName, object.Name(),
			filepath.Base(at.file), at.offset))
	}
	return encoded, isHeld
}

func (held *answeredFromWhole) Computed(kindName string, object types.Object, _ []byte) {
	if _, isVariant := held.variants[object.Pkg()]; isVariant {
		held.miss(fmt.Sprintf("%s about %s computed in the variant", kindName, object.Name()))
	}
}

// heldVariant is a variant as its hand finds the whole run's answers for it: the file set its positions are in, and
// the ID of the package it stubs.
type heldVariant struct {
	fileSet            *token.FileSet
	originalIdentifier string
}

func (held *answeredFromWhole) miss(miss string) {
	held.lock.Lock()
	defer held.lock.Unlock()
	held.misses = append(held.misses, miss)
}
