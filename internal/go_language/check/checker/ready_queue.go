package checker

// readyQueue is a binary heap of the ready actions of the granted groups, the one to run next on top: the action on
// the package with the highest priority, of those alike the larger cost, then the package ID that sorts first, then
// the action's place in its group, so the order is the same every run. It is written out rather than built on
// container/heap, whose interface passes every action through any and back.
type readyQueue struct {
	actions []*scheduledAction
}

// length is how many actions are queued.
func (queue *readyQueue) length() int { return len(queue.actions) }

// add queues a ready action, moving it up past every parent it runs before.
func (queue *readyQueue) add(scheduled *scheduledAction) {
	queue.actions = append(queue.actions, scheduled)
	child := len(queue.actions) - 1
	for child > 0 {
		parent := (child - 1) / 2
		if !runsBefore(queue.actions[child], queue.actions[parent]) {
			return
		}
		queue.actions[child], queue.actions[parent] = queue.actions[parent], queue.actions[child]
		child = parent
	}
}

// take removes and returns the action on top, which the caller has checked is there, moving the last action into
// its place and down past every child that runs before it.
func (queue *readyQueue) take() *scheduledAction {
	top := queue.actions[0]
	last := len(queue.actions) - 1
	queue.actions[0] = queue.actions[last]
	// The emptied slot lets go of the action, so a finished action is not held by the queue's backing array.
	queue.actions[last] = nil
	queue.actions = queue.actions[:last]
	parent := 0
	for {
		earliest := parent
		for _, child := range []int{2*parent + 1, 2*parent + 2} {
			if child < len(queue.actions) && runsBefore(queue.actions[child], queue.actions[earliest]) {
				earliest = child
			}
		}
		if earliest == parent {
			return top
		}
		queue.actions[parent], queue.actions[earliest] = queue.actions[earliest], queue.actions[parent]
		parent = earliest
	}
}

// runsBefore reports whether first is to run before second.
func runsBefore(first, second *scheduledAction) bool {
	if first.group == second.group {
		return first.order < second.order
	}
	if first.group.priority != second.group.priority {
		return first.group.priority > second.group.priority
	}
	if first.group.cost != second.group.cost {
		return first.group.cost > second.group.cost
	}
	return first.group.analyzedPackage.ID < second.group.analyzedPackage.ID
}
