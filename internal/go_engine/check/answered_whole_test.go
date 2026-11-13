package check

import (
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
)

// answeredWhole is the hand on the whole run's answers: it answers nothing and keeps every answer computed about an
// object of one of analyzed, by the object's place.
type answeredWhole struct {
	analyzed map[*types.Package]*packages.Package

	lock    sync.Mutex
	answers map[heldAnswerKey][]byte
}

func (whole *answeredWhole) Answer(string, types.Object) ([]byte, bool) {
	return nil, false
}

func (whole *answeredWhole) Computed(kindName string, object types.Object, encoded []byte) {
	owner := whole.analyzed[object.Pkg()]
	if owner == nil {
		return
	}
	at, isPlaced := positionAt(owner.Fset, object.Pos())
	if !isPlaced {
		return
	}
	whole.lock.Lock()
	defer whole.lock.Unlock()
	whole.answers[heldAnswerKey{packageIdentifier: owner.ID, kind: kindName, at: at}] = encoded
}
