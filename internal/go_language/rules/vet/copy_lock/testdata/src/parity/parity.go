// Package parity holds every branch of copylock's judgment, each with a clean case beside it, and the shapes most
// likely to set the port and x/tools' copylock apart: generics and their constraints, aliases, method values,
// closures, embedded fields, dot imports, arrays, tuples, and files at different Go versions. The differential test
// runs both over each and compares.
//
// This file has no //go:build line, and the fixtures load in GOPATH mode, where a package has no Go version, so its
// version is unknown, which both take as later than every release.
package parity

import (
	"sync"
	"unsafe"
)

type holder struct {
	name  string
	mutex sync.Mutex
}

type pointerHolder struct {
	mutex *sync.Mutex
}

func newMutex() *sync.Mutex { return new(sync.Mutex) }

func mutexValue() sync.Mutex { return sync.Mutex{} }

// Assignment: a copy from a variable, a dereference, a parenthesized value, an index, a field, and the second of two
// values, beside a composite literal, a call, the dereference of a call, through parentheses too, and a pointer.
func assignments(pointer *sync.Mutex, table map[string]holder, mutexes []sync.Mutex) {
	var mutex sync.Mutex
	var target sync.Mutex
	var count int
	target = mutex
	target = *pointer
	target = (mutex)
	target = mutexes[0]
	target = table["key"].mutex
	count, target = 1, mutex
	target, count = mutex, 2
	target = sync.Mutex{}
	target = mutexValue()
	target = *newMutex()
	target = *(newMutex())
	target = (*newMutex())
	target = *(&mutex)
	copied := mutex
	reference := &mutex
	held, present := table["key"]
	found := table["key"]
	_, _, _, _, _, _ = target, count, copied, reference, held, present
	_ = found
	var safe pointerHolder
	safeCopy := safe
	_ = safeCopy
	// An assignment that is not a for loop's init declares nothing per iteration.
	if inIf := mutexValue(); inIf.TryLock() {
	}
	switch inSwitch := mutexValue(); {
	case inSwitch.TryLock():
	}
}

// Declaration: var with a copied value, several names, a block, and a constant beside it, which cannot hold a lock.
func declarations(pointer *sync.Mutex) {
	var mutex sync.Mutex
	var copied = mutex
	var first, second = *pointer, 1
	var (
		third  = mutex
		fourth = sync.Mutex{}
		fifth  = mutexValue()
	)
	const constant = 1
	_, _, _, _, _, _ = copied, first, second, third, fourth, fifth
}

var packageMutex sync.Mutex

var packageCopy = packageMutex

// Literal: unkeyed and keyed elements, a map's value and not its key, an array of locks, and a nested literal.
func literals(pointer *sync.Mutex) {
	var mutex sync.Mutex
	_ = []sync.Mutex{mutex, *pointer, {}}
	_ = holder{name: "name", mutex: mutex}
	_ = holder{"name", *pointer}
	_ = map[sync.Mutex]int{mutex: 1}
	_ = map[int]sync.Mutex{1: mutex, 2: sync.Mutex{}}
	_ = [2]sync.Mutex{mutex}
	_ = []holder{{mutex: mutex}, {mutex: sync.Mutex{}}}
	_ = []*sync.Mutex{&mutex, pointer}
}

// Return: a copied result, a dereference, a second result, beside a zero value, a call, and a pointer.
func returnCopied(pointer *sync.Mutex) sync.Mutex {
	return *pointer
}

func returnSecond(mutex *sync.Mutex) (int, sync.Mutex) {
	return 1, *mutex
}

func returnZero() sync.Mutex {
	return sync.Mutex{}
}

func returnCall() sync.Mutex {
	return mutexValue()
}

func returnPointer(mutex *sync.Mutex) *sync.Mutex {
	return mutex
}

func returnInterface(mutex *sync.Mutex) any {
	return *mutex
}

func takesAny(value any) {}

type receiver struct{}

func (receiver) method(value any) {}

func generic[T any](value T) {}

// Call: a function, a method, a method value, a function literal called on the spot, a generic function, a variadic
// builtin, beside the builtins that only read their argument's type.
func calls(pointer *sync.Mutex) {
	var mutex sync.Mutex
	var mutexes [2]sync.Mutex
	takesAny(mutex)
	takesAny(*pointer)
	takesAny(&mutex)
	takesAny(sync.Mutex{})
	takesAny(mutexValue())
	var value receiver
	value.method(mutex)
	methodValue := value.method
	methodValue(mutex)
	receiver.method(value, mutex)
	func(any) {}(mutex)
	generic(mutex)
	generic[sync.Mutex](mutex)
	_ = append([]sync.Mutex{}, mutex)
	println(mutex)
	_ = len(mutexes)
	_ = cap(mutexes)
	_ = unsafe.Sizeof(mutex)
	_ = unsafe.Alignof(mutex)
	_ = unsafe.Offsetof(holder{}.mutex)
	closure := func() {
		takesAny(mutex)
		nested := func(copied sync.Mutex) {}
		nested(mutex)
	}
	closure()
}

// Function: a receiver and parameters by value, a field naming several parameters, a variadic parameter, a result,
// and pointers beside them.
func (holder) valueReceiver() {}

func (*holder) pointerReceiver() {}

func parameters(first, second sync.Mutex, third *sync.Mutex, fourth holder) {}

func variadic(mutexes ...sync.Mutex) {}

func unnamed(sync.Mutex, int) {}

func result() (mutex sync.Mutex) { return }

var literalParameter = func(mutex sync.Mutex, pointer *sync.Mutex) {}

// Range: keys and values declared or assigned, a blank, a field, and what each kind of collection yields.
func ranges(pointer *[3]sync.Mutex) {
	var mutex sync.Mutex
	var index int
	var mutexes []sync.Mutex
	var table map[sync.Mutex]holder
	var channel chan sync.Mutex
	var target holder
	for index = range mutexes {
	}
	for _, mutex = range mutexes {
	}
	for _, element := range mutexes {
		_ = element
	}
	for _, _ = range mutexes {
	}
	for key, value := range table {
		_, _ = key, value
	}
	for key := range table {
		_ = key
	}
	for _, target.mutex = range mutexes {
	}
	for target.mutex = range table {
	}
	for received := range channel {
		_ = received
	}
	for _, element := range pointer {
		_ = element
	}
	for _, element := range *pointer {
		_ = element
	}
	for index, character := range "text" {
		_, _ = index, character
	}
	_, _, _ = index, &mutex, &target
}

// Loop: a variable a for loop's `:=` declares holding a lock is copied on every iteration in this file, whose
// version is unknown. A blank, a value the assignment already reports, `=`, and a pointer are not.
func loops(pointer *sync.Mutex) {
	var mutex sync.Mutex
	for index, loopMutex := 0, (sync.Mutex{}); index < 1; index++ {
		_ = loopMutex.TryLock()
	}
	for loopMutex, loopHolder := mutexValue(), (holder{}); ; {
		_, _ = loopMutex.TryLock(), loopHolder
		break
	}
	for _, index := (sync.Mutex{}), 0; index < 1; index++ {
	}
	for loopMutex, other := mutex, (sync.Mutex{}); ; {
		_, _ = loopMutex.TryLock(), other.TryLock()
		break
	}
	for loopPointer := pointer; loopPointer != nil; loopPointer = nil {
	}
	var assigned sync.Mutex
	for assigned = (sync.Mutex{}); ; {
		_ = assigned.TryLock()
		break
	}
	for {
		inBody := sync.Mutex{}
		_ = inBody.TryLock()
		break
	}
}
