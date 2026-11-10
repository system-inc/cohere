package contract

// InputsRecord is additive within contract 3, and the Go engine's alone: it declares every path whose bytes,
// listing or absence the run's verdict was decided from, so the front door can replay the verdict without starting
// the engine when nothing it names has moved. The front door records what it can observe itself (the environment,
// the settings, the binary); what `go list` and the load read happens in subprocesses and in this process, where it
// cannot look, so the engine names it here.
//
// The standing rule: anything the loader, or a later cache of the engine's, reads to decide a verdict must appear in
// this record, since a read it leaves out is a replay that goes stale without a word. A superset only costs a miss.
//
// Every path is absolute and cleaned, each list sorted without duplicates, and each written `[]` when empty (see
// MarshalJSON).
type InputsRecord struct {
	// Files are the files whose bytes the verdict can depend on.
	Files []string `json:"files"`
	// Directories are the directories whose listing decides which packages or files exist.
	Directories []string `json:"directories"`
	// Absent are the paths whose absence the run depends on: a file that, once there, would change the load.
	Absent []string `json:"absent"`
	// TrackedIgnored names each git index among Files that the run read only for what it decides of a walk: which
	// paths its ignore rules exclude are tracked, and which are gitlinks. The front door keys such an index on
	// gitignore.TrackedIgnoredDigest over the walk rather than on its bytes, which git rewrites on a `git status`
	// (#m1daxxk). An index not named here is keyed on its bytes.
	TrackedIgnored []TrackedIgnoredScope `json:"trackedIgnored"`
}

// TrackedIgnoredScope is one index the run read for a walk: the index file, the root of the repository it belongs to,
// both absolute, and the directory walked, relative to that root and slash separated, "" for the root itself.
type TrackedIgnoredScope struct {
	Index string `json:"index"`
	Root  string `json:"root"`
	Scope string `json:"scope"`
}

func (InputsRecord) kind() Kind { return KindInputs }

// MarshalJSON writes each empty list as `[]`, never `null`, so a reader never has to tell "nothing read" from "not
// recorded".
func (record InputsRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has InputsRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler InputsRecord
	declared := withoutMarshaler(record)
	if declared.Files == nil {
		declared.Files = []string{}
	}
	if declared.Directories == nil {
		declared.Directories = []string{}
	}
	if declared.Absent == nil {
		declared.Absent = []string{}
	}
	if declared.TrackedIgnored == nil {
		declared.TrackedIgnored = []TrackedIgnoredScope{}
	}
	return encodeWithoutHtmlEscaping(declared)
}
