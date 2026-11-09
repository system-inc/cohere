package contract

// LintRecord summarizes the lint phase, for the lint line and its coverage notes.
type LintRecord struct {
	Findings            int `json:"findings"`
	RulesRun            int `json:"rulesRun"`
	FilesWalked         int `json:"filesWalked"`
	NodesVisited        int `json:"nodesVisited"`
	ElapsedMilliseconds int `json:"elapsedMilliseconds"`
	// ReusedFrom names the phase whose walk was reused, and is empty when lint walked on its own.
	ReusedFrom           string         `json:"reusedFrom"`
	RulesSilent          []string       `json:"rulesSilent"`
	RulesWatchedAndQuiet int            `json:"rulesWatchedAndQuiet"`
	Crashes              []Crash        `json:"crashes"`
	RulesScopedOff       map[string]int `json:"rulesScopedOff"`
	RulesNotConfigured   []string       `json:"rulesNotConfigured"`
	// ConfigNote is the line saying which defaults ran when no config was read, empty when one was.
	ConfigNote string `json:"configNote"`
	// FilesReplayed is additive within contract 3 (swift/Contract.md): of FilesWalked, the files whose every rule's
	// verdict the engine read from a per-file verdict store rather than computed, as the Swift engine's lint verdict
	// cache does. This engine leaves it unset and so unwritten: its cache restores packages (Cache below), and how
	// many files a restored package stands for is not a count it keeps, so what the field means for a Go run is for
	// this engine's owner to define. It is a pointer so a Swift record's count, 0 included, survives a reencode.
	FilesReplayed *int `json:"filesReplayed,omitempty"`

	// WithheldBySuppression, WithheldAsGenerated and WithheldAsCopied are additive within contract 3, which the
	// front door ignores until it reads them, as it ignores any field the contract does not list. Each counts by
	// rule the findings a run found and did not write: those a //cohere:ignore comment withheld, those of a house
	// rule in a generated file, and those of a house rule in a file that declares itself a copy of upstream source.
	// Without them a run that hid everything reads like a run that found nothing. All are always written as
	// objects, `{}` when empty (see MarshalJSON).
	WithheldBySuppression map[string]int `json:"withheldBySuppression"`
	WithheldAsGenerated   map[string]int `json:"withheldAsGenerated"`
	WithheldAsCopied      map[string]int `json:"withheldAsCopied"`

	// Cache is what the Go engine's per-package findings cache did in the lint phase, additive within contract 3
	// as the withheld counts are. It changes no finding and no count above.
	Cache LintCache `json:"cache"`

	// Builds is additive within contract 3: every build a run checked when it checked more than one, the host's first
	// (#72n9mq0). It is absent for a run of one build, so such a run writes the record it always has.
	Builds []BuildRecord `json:"builds,omitempty"`
}

// BuildRecord is what one build of a run covered, and what the merge of every build's findings kept of it.
type BuildRecord struct {
	// Target names the build as go env would make it: `windows/arm64`, with ` cgo` when cgo is on and its tags after
	// ` tags `.
	Target string `json:"target"`
	Host   bool   `json:"host"`
	// CompiledFiles counts the files of ours the build compiles, and AnalyzedPackages the packages it analyzed: every
	// package its load holds, and none for a build whose load found a type error.
	CompiledFiles    int `json:"compiledFiles"`
	AnalyzedPackages int `json:"analyzedPackages"`
	// FindingsAdded counts the findings that stand which this build kept and the host's did not, and FindingsDropped
	// the findings this build kept that the merge left out, since their rule keeps only what every build with a say
	// on the file reports.
	FindingsAdded   int `json:"findingsAdded"`
	FindingsDropped int `json:"findingsDropped"`
}

// LintCache counts, by package, what the per-package findings cache did: every package the load reached was hit,
// restored from an entry with nothing run on it, and on the load from source nothing parsed or type-checked unless
// the early cutoff restored it by its second key, or missed, checked and analyzed from source.
// Of the misses, written got an entry and unstorable held what no entry can hold. FactsDropped counts the facts the
// written entries left out because nothing outside their package can name their object, and FactsSkipped the
// restored facts whose object the package's export data does not hold. Waves counts the times the lint phase analyzed
// packages: at most two, those whose own inputs moved and then the rest that missed, once the hits the early cutoff
// found among the packages above the first were restored. FromBinary counts the standard-library packages restored
// from the entries the binary carries, which nothing analyzed, apart from hit; it is written only when it is not 0,
// so a build with none writes the record it always has. Reloads counts the packages the run loaded again from source,
// though the cache held them, because a question of a fact computed on demand reached one restored with no answer
// stored (#w7zmvat wave 3), and ReloadRounds how many times the run loaded and linted again for them; each is written
// only when it is not 0. On is false under `--no-cache`, and every count 0
// but FromBinary, which the binary's entries can still give, FactsSkipped of what they restored, and Reloads of them.
type LintCache struct {
	On           bool `json:"on"`
	Hit          int  `json:"hit"`
	FromBinary   int  `json:"standardLibraryFromBinary,omitempty"`
	Missed       int  `json:"missed"`
	Written      int  `json:"written"`
	Unstorable   int  `json:"unstorable"`
	FactsDropped int  `json:"factsDropped"`
	FactsSkipped int  `json:"factsSkipped"`
	Waves        int  `json:"waves"`
	Reloads      int  `json:"reloads,omitempty"`
	ReloadRounds int  `json:"reloadRounds,omitempty"`
}

func (LintRecord) kind() Kind { return KindLint }

// MarshalJSON writes the withheld counts as `{}` when there are none, never `null`, so a reader of the field
// never has to tell "none withheld" from "not counted".
func (record LintRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has LintRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler LintRecord
	counted := withoutMarshaler(record)
	if counted.WithheldBySuppression == nil {
		counted.WithheldBySuppression = map[string]int{}
	}
	if counted.WithheldAsGenerated == nil {
		counted.WithheldAsGenerated = map[string]int{}
	}
	if counted.WithheldAsCopied == nil {
		counted.WithheldAsCopied = map[string]int{}
	}
	return encodeWithoutHtmlEscaping(counted)
}
