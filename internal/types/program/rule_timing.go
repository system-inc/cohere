package program

import "time"

// RuleTiming is what one rule cost over a run, in CPU.
//
// CPU, not wall time, because wall time lied (#8qyzmxw). Each listener call used to be charged its
// wall time, and a goroutine descheduled mid-call was charged for the time it spent off the CPU. Under
// load that is most of the number: the better-tailwindcss family read about 1.2s on the old table,
// while eight interleaved cold pairs with the family off and on differed by a median of 0.049s, and a
// CPU profile put it under 1%. A rule called most often was interrupted most often, so the table
// ranked call counts and called them cost.
//
// Setup and listener time are kept apart because they are different defects. A rule burning time in
// Run and then declining the file is doing expensive work to decide it has nothing to do, which is
// the shape that made consistency-no-abbreviated-identifier 2,580ms of a 3,700ms run: it resolved a
// scope lookup on every identifier before testing whether the name even looked abbreviated. A cheap
// structural gate took it to 152ms, 17x, findings byte-identical. A rule burning time in its
// listeners is doing expensive work per node, which is a different fix.
type RuleTiming struct {
	Name string

	// SetupCpu is CPU inside the rule's Run function, deciding what to listen to.
	SetupCpu time.Duration

	// ListenerCpu is CPU inside the rule's listeners, walking nodes.
	ListenerCpu time.Duration

	// FilesListened is how many files the rule accepted, and FilesDeclined how many it refused.
	//
	// Declining is the cheapest and most valuable thing a rule can do, so a rule that declines
	// nothing is worth seeing: registering for a very common node kind and returning early is
	// legitimate, and it is also how a cheap-looking rule becomes expensive at scale.
	FilesListened int
	FilesDeclined int

	// NodesOffered is how many nodes the walk called this rule's listeners for.
	NodesOffered int

	// Findings is how many diagnostics the rule reported, before suppression.
	Findings int
}

// TotalCpu is what the rule cost altogether.
func (timing RuleTiming) TotalCpu() time.Duration {
	return timing.SetupCpu + timing.ListenerCpu
}
