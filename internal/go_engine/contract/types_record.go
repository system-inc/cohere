package contract

// TypesRecord summarizes the types phase.
type TypesRecord struct {
	Diagnostics         int `json:"diagnostics"`
	Files               int `json:"files"`
	ElapsedMilliseconds int `json:"elapsedMilliseconds"`
	// FilesWithoutRecord are files the engine cannot vouch for, each a gap that makes the run incomplete.
	FilesWithoutRecord []string `json:"filesWithoutRecord"`
	// Build says in words how the type information was had.
	Build string `json:"build"`

	// CensusMissed is additive within contract 3 (#yyv7nr2, #hd5bsqh): each file a package the load reached in the
	// workspace holds that the census never counted, with that package, sorted by file, and absent when there is none.
	// The census and the load read one tree, so a file the load reached and the census never counted is one no count
	// of the run accounts for, as at a root or a go.work use reached through a symbolic link, where the census once
	// walked nothing. Such a run is never complete: the front door refuses a summary that calls it complete.
	CensusMissed []CensusMissedFile `json:"censusMissed,omitempty"`

	// WithheldInDependencyCgo is additive within contract 3. It names each file of a dependency whose type errors
	// the run did not report, since no cgo tool ran and the faked package "C" is what they are about, sorted by
	// file, each with at least one error. The front door counts them in a sentence under the types line, then names
	// each file with its package, so a run that withheld them never reads like one with nothing to withhold. It is
	// always written as a list, `[]` when empty (see MarshalJSON).
	WithheldInDependencyCgo []WithheldFile `json:"withheldInDependencyCgo"`

	// WithheldDependencyCgoDeclarations is additive within contract 3 too. It counts the declarations of dependencies
	// with cgo files that use the faked package "C", what their cgo files declare, or what either left without a
	// valid type: functions, which every rule was given as an assembly function is given, without a body, and
	// package-level variables, given without their initial values, since the analyzers that read every expression
	// stop on one. The front door says the count in the same sentence under the types line, so a run whose rules
	// saw less of a dependency never reads like one that saw it whole.
	WithheldDependencyCgoDeclarations int `json:"withheldDependencyCgoDeclarations"`

	// CgoTranslation is additive within contract 3 too. The engine type-checks and never builds, but for one
	// exception: for a project with cgo files of its own, go list -compiled has the cgo tool translate every package
	// with cgo files, the project's and its dependencies'. CgoTranslation says how much of that a run had, so the
	// exception is never invisible: how many packages the cgo tool translated, and how long that took. On a load that
	// translates nothing it is written as 0 and 0, never left out.
	CgoTranslation CgoTranslation `json:"cgoTranslation"`

	// Memory is additive within contract 3 too: the memory limit the engine's process ran under and where it came
	// from, so `--verbose` can say why a run collected when it did. Left out when the engine was not started as a
	// process of its own, as in a test running it in process, since nothing was decided then.
	Memory *Memory `json:"memory,omitempty"`
}

// MemorySource is where the engine's memory limit came from.
type MemorySource string

const (
	// MemorySourceEnvironment is a `GOGC` or `GOMEMLIMIT` the user set, which decides the collector, so the engine
	// sets nothing.
	MemorySourceEnvironment MemorySource = "Environment"
	// MemorySourceCap is the engine's cap, lower than half the memory available.
	MemorySourceCap MemorySource = "Cap"
	// MemorySourceAvailable is half the memory available when the run started, lower than the cap.
	MemorySourceAvailable MemorySource = "Available"
	// MemorySourceUnreadable is the cap, set as a bound alone, since available memory could not be read and the
	// collector runs at Go's default.
	MemorySourceUnreadable MemorySource = "Unreadable"
)

// Memory is the memory limit a run's process held its heap under. LimitBytes is 0 when no limit applied, which only
// the environment can decide.
type Memory struct {
	LimitBytes int64        `json:"limitBytes"`
	CapBytes   int64        `json:"capBytes"`
	Source     MemorySource `json:"source"`
	// AvailableBytes is the memory available when the run started, 0 when it could not be read or did not matter.
	AvailableBytes uint64 `json:"availableBytes"`
	// Environment names the variables that set the collector, as `GOGC=100`, when Source is Environment.
	Environment string `json:"environment,omitempty"`
}

// CgoTranslation is how many packages a run's cgo tool translated, and the milliseconds the go list that ran it
// took.
type CgoTranslation struct {
	Packages     int `json:"packages"`
	Milliseconds int `json:"milliseconds"`
}

// CensusMissedFile is one file a package the load reached holds that the census never counted, with that package's
// import path.
type CensusMissedFile struct {
	Package string `json:"package"`
	File    string `json:"file"`
}

// WithheldFile is one file whose type errors a run withheld, with the package it is in and how many there were.
type WithheldFile struct {
	Package string `json:"package"`
	File    string `json:"file"`
	Errors  int    `json:"errors"`
}

func (TypesRecord) kind() Kind { return KindTypes }

// MarshalJSON writes the withheld files as `[]` when there are none, never `null`, so a reader of the field never
// has to tell "none withheld" from "not counted".
func (record TypesRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has TypesRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler TypesRecord
	counted := withoutMarshaler(record)
	if counted.WithheldInDependencyCgo == nil {
		counted.WithheldInDependencyCgo = []WithheldFile{}
	}
	return encodeWithoutHtmlEscaping(counted)
}
