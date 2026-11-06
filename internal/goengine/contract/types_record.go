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

	// WithheldInDependencyCgo is additive within contract 3, which the front door ignores until it reads it, as it
	// ignores any field the contract does not list. It names each file of a dependency the cgo tool would translate
	// whose type errors the run did not report, since no cgo tool ran and the faked package "C" is what they are
	// about, sorted by file. Without it a run that withheld them reads like one with nothing to withhold. It is
	// always written as a list, `[]` when empty (see MarshalJSON).
	WithheldInDependencyCgo []WithheldFile `json:"withheldInDependencyCgo"`

	// WithheldDependencyCgoDeclarations is additive within contract 3 too. It counts the declarations of dependencies
	// with cgo files that use the faked package "C", what their cgo files declare, or what either left without a
	// valid type: functions, which every rule was given as an assembly function is given, without a body, and
	// package-level variables, given without their initial values, since the analyzers that read every expression
	// stop on one. Without it a run whose rules saw less of a dependency reads like one that saw it whole.
	WithheldDependencyCgoDeclarations int `json:"withheldDependencyCgoDeclarations"`
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
