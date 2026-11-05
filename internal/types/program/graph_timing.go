package program

import "time"

// GraphTiming is what building the graph cost, by part, collected when Options.Timing is set.
//
// The graph phase was one number, `graph built in 547ms`, and nobody knew what it was made of
// (#cazsft3). The levers proposed for it were guesses: parallel directory enumeration, a resolution
// cache, a declaration pack. Each is worth something only if its part is a large share, and that is
// what this measures rather than reasons about.
//
// Two kinds of number, and they must not be read as one:
//
//   - Wall: Config, Program and Verify are each one stretch of the build's own goroutine, and they add
//     up to most of the phase.
//   - Summed: the compiler loads files on many goroutines at once, so the time spent inside its loads
//     and inside the disk is summed across all of them and runs far past Program's wall. It says
//     where the loaders' work goes, not how long anyone waited.
//
// Parse and module resolution are not separable from here. Both run inside the compiler's file
// loader, interleaved per file, behind one call. What can be measured from outside is each load,
// which is a read and a parse, and the disk beneath it; resolution and the loader's own bookkeeping
// are the part of Program's wall those leave unexplained.
type GraphTiming struct {
	// Builds is how many times the program was built: two when the first build's roots moved under it
	// and Build built again. Every duration below covers all of them.
	Builds int

	// Config is parsing the tsconfig, which includes enumerating the directories its `include`
	// patterns reach. That enumeration is the directory walk a parallel readDirectory would speed up.
	Config time.Duration

	// Program is compiler.NewProgram: every file read, parsed and its imports resolved.
	Program time.Duration

	// Verify is matching the config's files to the program's, and asking the disk about any missing.
	Verify time.Duration

	// SourceFileLoads and SourceFileSummed are the compiler host's GetSourceFile calls, each a read
	// and a parse, summed across the loaders.
	SourceFileLoads  int64
	SourceFileSummed time.Duration

	// ConfigDisk and ProgramDisk are the real filesystem calls made while the config was parsed and
	// while the program was built: beneath the stat and readdir cache, so a call it answered is not
	// here, and above the content pack, so a file the pack served is here at the cost of serving it.
	ConfigDisk  DiskTiming
	ProgramDisk DiskTiming

	// PackServed and PackRead are the content pack's own count of the reads above: files it served from
	// its mapping, and files it had to read from disk. Both zero when the build had no pack. A read the
	// pack served costs a copy, not an open, so the two must be told apart to read Reads' time.
	PackServed int
	PackRead   int

	// CheckedAnswers is how many of the existence checks and stats above were answered from the run cache's
	// check (Options.CheckedStats) rather than by the disk, across config and program alike.
	CheckedAnswers int64
}

// DiskCalls is a count of one kind of call and their summed duration.
type DiskCalls struct {
	Count  int64
	Summed time.Duration
}

const (
	diskRead = iota
	diskExistence
	diskStat
	diskListing
	diskRealpath
	diskCallKinds
)
