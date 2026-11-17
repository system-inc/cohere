package go_engine

// shareGate is where the extra builds wait before their type checks when the width is the memory's to decide in a run
// without a findings store (#ghb3rcn), or in one with a store whose host mostly restored (takesShareGate, #ybjbqm2), a
// cold run with one taking the measured rule whole: every build starts at once, lists, builds its graph and restores,
// then reports its share, the packages it will check from source over the packages its load holds, and waits (report).
// A build that ends before it reaches the gate arrives with no share (end). Once every build has arrived the shares are
// judged: when none is over lowShareAtGate the width is decided from the host's live heap alone, and otherwise it is
// measured by the first build in start order, which passes the gate alone (open). Builds pass in start order, each once
// every build before it has passed or ended (turn, admit). Every field is read and written under extraBuildRuns'
// admitLock.
//
// A build waiting here holds its load open (loadPackages, loadMemory), so the late raise of the memory limit waits
// until the last waiting build's load has ended rather than landing while the first builds lint, as it did when a
// build started only once there was room for it (#ghb3rcn, #ph5532e measures it).
type shareGate struct {
	// shares are each build's share by position in start order, arrived the builds that reported or ended, settled
	// those admitted or ended, so a later build's turn waits for none of them, and arrivals how many have arrived.
	shares   []float64
	arrived  []bool
	settled  []bool
	arrivals int

	// judged is whether every build has arrived and the shares were judged, and measured whether a share over
	// lowShareAtGate left the width to be measured by the first build.
	judged   bool
	measured bool
}

// newShareGate is the gate for as many builds as builds, by position in the order they start in, none arrived.
func newShareGate(builds int) *shareGate {
	return &shareGate{
		shares:  make([]float64, builds),
		arrived: make([]bool, builds),
		settled: make([]bool, builds),
	}
}

// report keeps the share of the build at position, fromSource of its packages, and reports whether every build has now
// arrived. A load of no package has a share of none.
func (gate *shareGate) report(position int, fromSource int, packages int) bool {
	gate.shares[position] = float64(fromSource) / float64(max(packages, 1))
	gate.arrived[position] = true
	gate.arrivals++
	return gate.arrivals == len(gate.shares)
}

// end settles the build at position as it ends, and reports whether it was the last to arrive: a build that ended
// before it reported arrives with no share, and one that reported arrived already.
func (gate *shareGate) end(position int) bool {
	gate.settled[position] = true
	if gate.arrived[position] {
		return false
	}
	gate.arrived[position] = true
	gate.arrivals++
	return gate.arrivals == len(gate.shares)
}

// overLine reports whether any share reported is over lowShareAtGate, which leaves the width to be measured.
func (gate *shareGate) overLine() bool {
	for _, share := range gate.shares {
		if share > lowShareAtGate {
			return true
		}
	}
	return false
}

// open has the gate admit builds once the shares are judged, the width measured when measured is true.
func (gate *shareGate) open(measured bool) {
	gate.judged, gate.measured = true, measured
}

// turn reports whether the build at position may pass now, the width aside: once the shares are judged, every build
// before it has passed or ended, and the width is decided, or, when it is to be measured, the build is the first,
// which it is measured by.
func (gate *shareGate) turn(position int, widthDecided bool) bool {
	if !gate.judged || (!widthDecided && !(gate.measured && position == 0)) {
		return false
	}
	for _, settled := range gate.settled[:position] {
		if !settled {
			return false
		}
	}
	return true
}

// admit settles the build at position as it passes, so the build after it may take its turn.
func (gate *shareGate) admit(position int) {
	gate.settled[position] = true
}
