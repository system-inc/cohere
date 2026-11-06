package micromark

import "slices"

// splice is upstream's micromark-util-chunked splice, Array#splice without the stack limit: remove
// `remove` items at `start` and insert `items` there. Returns the list, since Go slices can move.
func splice[T any](list []T, start int, remove int, items []T) []T {
	end := len(list)

	// Make start between zero and `end` (included).
	clampedStart := start
	if start < 0 {
		if -start > end {
			clampedStart = 0
		} else {
			clampedStart = end + start
		}
	} else if start > end {
		clampedStart = end
	}

	clampedRemove := remove
	if remove < 0 {
		clampedRemove = 0
	}
	if clampedStart+clampedRemove > end {
		clampedRemove = end - clampedStart
	}

	return slices.Insert(slices.Delete(list, clampedStart, clampedStart+clampedRemove), clampedStart, items...)
}

func spliceEvents(list []Event, start int, remove int, items []Event) []Event {
	return splice(list, start, remove, items)
}

// pushChunks is upstream's push: append items to list, or return items when list is empty.
func pushChunks(list []Chunk, items []Chunk) []Chunk {
	if len(list) > 0 {
		return append(list, items...)
	}
	return items
}

// pushEvents is upstream's push for events.
func pushEvents(list []Event, items []Event) []Event {
	if len(list) > 0 {
		return append(list, items...)
	}
	return items
}

// resolveAllEntries is upstream's micromark-util-resolve-all, for the tokenizer's own list.
func resolveAllEntries(entries []resolvable, events []Event, context *TokenizeContext) []Event {
	resolvedEvents := events
	var called []*Resolver

	for _, entry := range entries {
		resolve := entry.resolveAll()

		if resolve != nil && !slices.Contains(called, resolve) {
			resolvedEvents = resolve.Resolve(resolvedEvents, context)
			called = append(called, resolve)
		}
	}

	return resolvedEvents
}

// resolveAll is upstream's micromark-util-resolve-all over constructs.
func resolveAll(constructs []*Construct, events []Event, context *TokenizeContext) []Event {
	resolvedEvents := events
	var called []*Resolver

	for _, construct := range constructs {
		resolve := construct.ResolveAll

		if resolve != nil && !slices.Contains(called, resolve) {
			resolvedEvents = resolve.Resolve(resolvedEvents, context)
			called = append(called, resolve)
		}
	}

	return resolvedEvents
}
