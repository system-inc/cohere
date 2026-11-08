// Package differences holds where the port parts from the original, each finding recorded on its line with the
// reason above it, and the shapes where the two could most plausibly part and agree, which record nothing.
package differences

// sliceOf is a constraint by name whose type set is slices alone.
type sliceOf interface {
	~[]int
}

// startsWhereItStood holds counters the query's any operator admits that leave the counter as it was, so the loop
// slides from wherever it stood, and copy over the slice's start would move elements the loop never touches.
func startsWhereItStood(values []int, n int, offset int) {
	i := 3
	// The counter starts at 3 plus nothing.
	for i += 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
	// The counter starts at 3 less nothing.
	for i -= 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
	// The counter keeps its bits under an or with none.
	for i |= 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
	// The counter keeps its bits under an exclusive or with none.
	for i ^= 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
	// The counter shifted left by nothing is itself.
	for i <<= 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
	// The counter shifted right by nothing is itself.
	for i >>= 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
	// The counter with no bits cleared is itself.
	for i &^= 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[offset+i]
	}
}

// movingEnds holds loops whose offset or limit is the counter itself, which the query binds as any identifier: no
// slide, and the fix would name the counter where its header no longer declares it.
func movingEnds(values []int, n int) {
	// The loop gathers every second element, values[2*i], no slide.
	for i := 0; i < n; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[i+i]
	}
	// The loop never runs, and the fix would copy elements.
	for i := 0; i < i; i++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[i] = values[n+i]
	}
	var j int
	// Assigned, not declared, the counter as the offset still gathers.
	for j = 0; j < n; j++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[j] = values[j+j]
	}
	// Assigned, not declared, the counter as the limit still never runs.
	for j = 0; j < j; j++ { // port drops: should use copy() instead of loop for sliding slice elements
		values[j] = values[n+j]
	}
}

// shadowedCopy declares copies of its own, where the original's fix calls them: an integer, which fails to compile,
// and a function, which compiles and calls the wrong thing.
func shadowedCopy(values []int, n int, offset int) {
	copy := 0
	// The fix would call an int.
	for i := 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
	_ = copy
	{
		copy := func(to []int, from []int) int { return len(to) + len(from) }
		// The fix would call the function literal, which copies nothing.
		for i := 0; i < n; i++ { // port withholds: Use copy() instead of loop
			values[i] = values[offset+i]
		}
		_ = copy
	}
	_ = func() {
		// The fix would call the outer function's int, from inside a function literal.
		for i := 0; i < n; i++ { // port withholds: Use copy() instead of loop
			values[i] = values[offset+i]
		}
	}
}

// laterCopy declares its copy after the loop, and a counter named copy, so copy is the builtin where each loop stands
// and both fixes stand.
func laterCopy(values []int, n int, offset int) {
	for copy := 0; copy < n; copy++ {
		values[copy] = values[offset+copy]
	}
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
	copy := 0
	_ = copy
}

// outerCounters holds counters declared outside the loop and assigned zero by its header, which the loop leaves at
// the limit and the original's fix leaves where it was. The port keeps each finding, a slide all the same, and
// withholds the fix wherever the header declares no counter, read after the loop or not.
func outerCounters(values []int, n int, offset int) int {
	i := 5
	// The return reads the counter, n after the loop and 0 after the fix.
	for i = 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
	read := i
	j := 5
	// Nothing reads the counter again, and the fix still drops the loop's assignments to a variable that outlives it.
	for j = 0; j < n; j++ { // port withholds: Use copy() instead of loop
		values[j] = values[offset+j]
	}
	// Parenthesized, the counter is the same outer variable.
	for (i) = 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
	// A product with zero zeroes the counter, which outlives the loop all the same.
	for i *= 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
	// A mask of none zeroes the counter, which outlives the loop all the same.
	for i &= 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
	return read
}

// acrossLines holds a loop whose offset and limit the fix prints from lines of their own, with comments the fix drops.
func acrossLines(values []int, n int, offset int) {
	for i := 0; i <
		n; i++ {
		// The comment goes with the loop.
		values[i] = values[offset+
			i] // So does this one.
	}
}

// nested holds loops inside loops: a row of rows is indexed, no identifier, a row declared in the body makes it two
// statements, and an outer loop whose one statement is a slide is no slide itself, so only that inner loop is
// reported.
func nested(rows [][]int, n int, offset int) {
	for row := 0; row < n; row++ {
		for i := 0; i < n; i++ {
			rows[row][i] = rows[row][offset+i]
		}
	}
	for row := 0; row < n; row++ {
		for i := 0; i < n; i++ {
			values := rows[row]
			values[i] = values[offset+i]
		}
	}
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			rows[i] = rows[j+i]
		}
	}
}

// constrained's slice is a type parameter under a named constraint of slices alone.
func constrained[S sliceOf](values S, n int, offset int) {
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
}
