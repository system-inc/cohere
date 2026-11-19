package fact_resolver_test

import (
	"go/types"
	"sync"
)

// keepHand is a hand on a run's answers that answers every question with answer, when it has one, and keeps every
// answer it heard computed, by its object's name, in the order it heard them.
type keepHand struct {
	answer []byte
	lock   sync.Mutex
	heard  map[string][]string
}

func (hand *keepHand) Answer(string, types.Object) ([]byte, bool) {
	return hand.answer, hand.answer != nil
}

func (hand *keepHand) Computed(_ string, object types.Object, encoded []byte) {
	hand.lock.Lock()
	defer hand.lock.Unlock()
	hand.heard[object.Name()] = append(hand.heard[object.Name()], string(encoded))
}
