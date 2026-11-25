package main

// main.go in package main is named for the program's entry point, so a helper type with methods beside it is not
// judged against the name.
type heldSlot struct {
	index int
}

func (slot *heldSlot) release() {
	slot.index = -1
}

func main() {
	slot := &heldSlot{}
	slot.release()
	engine := &Engine{}
	engine.Run()
}
