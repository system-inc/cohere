package test_failure_in_goroutine

import (
	"fmt"
	"go/token"
	"go/types"
	"sync"
)

// heardAnswers is a test's hand on a run's answers (fact_resolver.Held): it hears each question the run would compute
// and each answer it computes, by the kind and the object's place in the run's files, and answers a question with the
// answer held at that place, when it holds one, as its JSON. Its methods are called from every pass at once.
type heardAnswers struct {
	fileSet *token.FileSet
	held    map[string][]byte

	lock     sync.Mutex
	asked    []heardQuestion
	computed map[string][]byte
}

// heardQuestion is one question a run would compute: the kind's name and the object it is about.
type heardQuestion struct {
	kind   string
	object types.Object
}

// newHeardAnswers is a hand on the answers of a run whose files fileSet holds, answering with held, the answers
// another hand heard computed, and with none when held is nil.
func newHeardAnswers(fileSet *token.FileSet, held map[string][]byte) *heardAnswers {
	return &heardAnswers{fileSet: fileSet, held: held, computed: map[string][]byte{}}
}

// key names the answer of the kind named kindName about object as two loads of the same files name it alike.
func (heard *heardAnswers) key(kindName string, object types.Object) string {
	position := heard.fileSet.Position(object.Pos())
	return fmt.Sprintf("%s %s:%d", kindName, position.Filename, position.Offset)
}

func (heard *heardAnswers) Answer(kindName string, object types.Object) ([]byte, bool) {
	heard.lock.Lock()
	defer heard.lock.Unlock()
	heard.asked = append(heard.asked, heardQuestion{kind: kindName, object: object})
	encoded, isHeld := heard.held[heard.key(kindName, object)]
	return encoded, isHeld
}

func (heard *heardAnswers) Computed(kindName string, object types.Object, encoded []byte) {
	heard.lock.Lock()
	defer heard.lock.Unlock()
	heard.computed[heard.key(kindName, object)] = encoded
}
