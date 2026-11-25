package contract

// TimingRecord is additive within contract 3, and the Go engine's alone: under `--timing` it says what the run's CPU
// went to, so the front door prints for a Go run the table `cohere --timing` prints for TypeScript, and a rule can be
// timed before it lands (#3apgjer). It is written once, after the lint record and before inputs and the summary.
//
// Every CPU figure is the kernel's thread clock read on a thread held for the work (cpu_clock.Thread), so a rule that
// waits, sleeps or is descheduled costs nothing, and none is wall time. Each figure has the measuring's own cost
// already taken out, and that cost is said once, in MeasuringCpuMicroseconds. Microseconds, because a rule's cost on
// a small tree is well under a millisecond and a 0 would read as a rule that never ran.
//
// What the rows cover and what they leave out are both said: ProcessCpuMicroseconds is every CPU microsecond the
// process spent, and what the rows do not account for (the collector's own threads, the runtime, the parts of the run
// no row measures) is the difference, which the front door prints as its own line rather than spreading it over the
// rows.
type TimingRecord struct {
	// Unavailable says why no CPU was measured, when the thread clock cannot be read on this platform. Every CPU
	// field is then 0 and means nothing; the counts are still real. Empty when the CPU was measured.
	Unavailable string `json:"unavailable,omitempty"`

	// ExtraBuildsFromSource is how many builds beside the host's the run checked, each of which restored nothing from
	// the host's run: a timed run keeps nothing for its extra builds (#3apgjer), so every package of every build was
	// checked from source, and the rows include that work (#4y4qd3z). A count rather than a flag, so the front door's
	// sentence says how many builds the rows were summed over. Written only when the run had extra builds, and
	// omitted otherwise.
	ExtraBuildsFromSource int `json:"extraBuildsFromSource,omitempty"`

	// ProcessCpuMicroseconds is the process's user and system CPU from its start to this record.
	ProcessCpuMicroseconds int64 `json:"processCpuMicroseconds"`

	// Load is the load's phases, summed over every build the run checked.
	Load LoadTiming `json:"load"`

	// Rules holds one entry for each rule that ran, by its name, most expensive first, summed over every package and
	// every build. Written `[]` when no rule ran.
	Rules []RuleTiming `json:"rules"`

	// Shared holds the analyzers that are no rule's own, by analyzer name, most expensive first: the intermediate
	// representation's build, the helpers several rules read (inspect, ctrlflow), and each dispatcher's walk less the
	// calls into its listeners, which are billed to their rules. Written `[]` when none ran.
	Shared []SharedTiming `json:"shared"`

	// MeasuredCalls is how many pieces of work were measured, each costing two reads of the clock.
	MeasuredCalls int64 `json:"measuredCalls"`
	// MeasuringCpuMicroseconds is what those reads cost, taken out of the rows they would have inflated.
	MeasuringCpuMicroseconds int64 `json:"measuringCpuMicroseconds"`
}

func (TimingRecord) kind() Kind { return KindTiming }

// MarshalJSON writes each empty list as `[]`, never `null`, so a reader never has to tell "nothing ran" from "not
// measured".
func (record TimingRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has TimingRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler TimingRecord
	declared := withoutMarshaler(record)
	if declared.Rules == nil {
		declared.Rules = []RuleTiming{}
	}
	if declared.Shared == nil {
		declared.Shared = []SharedTiming{}
	}
	return encodeWithoutHtmlEscaping(declared)
}

// LoadTiming is the CPU of the load's phases. Each phase runs on many goroutines at once, so each figure is summed
// across them and runs past the load's wall.
type LoadTiming struct {
	// ListCpuMicroseconds is this process's own CPU listing the packages, the in-process lister's or the reading of
	// go list's answer.
	ListCpuMicroseconds int64 `json:"listCpuMicroseconds"`
	// ListChildrenCpuMicroseconds is the CPU of the go commands the listing ran, read from the process's waited-for
	// children, which no thread of this process spent.
	ListChildrenCpuMicroseconds int64 `json:"listChildrenCpuMicroseconds"`
	// ParseCpuMicroseconds is the CPU reading and parsing ParsedFiles files.
	ParseCpuMicroseconds int64 `json:"parseCpuMicroseconds"`
	ParsedFiles          int   `json:"parsedFiles"`
	// TypeCheckCpuMicroseconds is the CPU type-checking TypeCheckedPackages packages from source.
	TypeCheckCpuMicroseconds int64 `json:"typeCheckCpuMicroseconds"`
	TypeCheckedPackages      int   `json:"typeCheckedPackages"`
	// ExportDataCpuMicroseconds is the CPU reading ExportDataPackages packages' types from export data in place of
	// their source.
	ExportDataCpuMicroseconds int64 `json:"exportDataCpuMicroseconds"`
	ExportDataPackages        int   `json:"exportDataPackages"`
}

// RuleTiming is one rule's CPU.
type RuleTiming struct {
	Name            string `json:"name"`
	CpuMicroseconds int64  `json:"cpuMicroseconds"`
	// Packages is how many packages the rule's own analyzer ran on, dependencies analyzed for facts included.
	Packages int `json:"packages"`
	// Calls is how many times a dispatcher called the rule's listener, 0 for a rule that walks on its own.
	Calls int64 `json:"calls"`
}

// SharedTiming is one analyzer's CPU that belongs to no single rule.
type SharedTiming struct {
	Name            string     `json:"name"`
	Kind            SharedKind `json:"kind"`
	CpuMicroseconds int64      `json:"cpuMicroseconds"`
	Packages        int        `json:"packages"`
}

// SharedKind says what a shared row is, in the contract's camelCase.
type SharedKind string

const (
	// SharedIntermediateRepresentation is the engine's intermediate representation build.
	SharedIntermediateRepresentation SharedKind = "intermediateRepresentation"
	// SharedDispatcher is a dispatcher's walk, less the calls into its listeners.
	SharedDispatcher SharedKind = "dispatcher"
	// SharedAnalyzer is any other analyzer several rules read, such as inspect or ctrlflow.
	SharedAnalyzer SharedKind = "analyzer"
)
