package load

// walkPriority is a type-read walk's priority for a load's slot: below every package's work, whose priority is its
// longest path, 1 or more (see prioritize), so a waiting walk is granted a slot only once no package's work waits.
const walkPriority = 0

// WalkSlots are a load's slots as the type-read walks of the rules beside it, or after it, take them (#b2t9xmf): each
// at a priority below every package's, so a walk never displaces the load's chain, and at once once the load has
// ended, when nothing else holds them. They are build_reads.Slots.
type WalkSlots struct {
	slots *prioritySlots
}

// Acquire takes one of the load's slots for a walk, waiting until no package's work waits before it.
func (walkSlots *WalkSlots) Acquire() {
	walkSlots.slots.acquire(walkPriority)
}

// Release gives the walk's slot back.
func (walkSlots *WalkSlots) Release() {
	walkSlots.slots.release()
}
