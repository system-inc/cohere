package contract

// LintRecord summarizes the lint phase, for the lint line and its coverage notes.
type LintRecord struct {
	Findings int `json:"findings"`
	// TypeErrors is additive within contract 3, and the Go engine's alone: the type errors of the run's other builds
	// (`--other-builds`), which the lint phase writes as compiler finding records beside the Findings it counts, since
	// only the merge of every build's verdict knows them (#rw6tg3a). The finding records of the phase are Findings
	// plus TypeErrors. Absent for a run of one build, so such a run writes the record it always has.
	TypeErrors          int `json:"typeErrors,omitempty"`
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
	// BuildsAtOnce is additive within contract 3, and the Go engine's alone: how many of the extra builds ran side by
	// side, decided once from the memory cap, the host's live heap and what the heaviest build holds as its load ends
	// (#b2t9xmf, #rmzehfg). It changes no finding, no note and no key: a run one build at a time writes the same
	// records but this one. Absent when Builds is.
	BuildsAtOnce int `json:"buildsAtOnce,omitempty"`
	// BuildsWidthLiveBytes is additive within contract 3, and the Go engine's alone: the host's live heap that decision
	// was given, measured by a collection as the extra builds started with nothing else running, so a reader sees why a
	// width was chosen. Absent when the decision needed none: under `--single-threaded`, a limit other than the cap,
	// builds that restore nothing from the host, or one build alone, which no heap can widen.
	BuildsWidthLiveBytes uint64 `json:"buildsWidthLiveBytes,omitempty"`
	// BuildsWidthBuildBytes is additive within contract 3, and the Go engine's alone: what the first extra build to
	// start, the heaviest, held beyond that heap as its load ended, measured the same way. With
	// BuildsWidthPeakFactor, the times that holding a build is taken to reach at its lint's peak, 3.5 in a run with a
	// findings store and 2.5 without one (#hv090f4), and the memory record's limit, a reader redoes the arithmetic:
	// floor((limit - live) / (holding * factor)), no more than the builds and at least one (#rmzehfg). Both are absent
	// when BuildsWidthLiveBytes is.
	BuildsWidthBuildBytes uint64  `json:"buildsWidthBuildBytes,omitempty"`
	BuildsWidthPeakFactor float64 `json:"buildsWidthPeakFactor,omitempty"`
	// BuildsPoolSize is additive within contract 3, and the Go engine's alone: how many slots the run's one pool holds,
	// which every load and check of the run shares, the host's and every extra build's, so however many builds run side
	// by side the run never runs more of that work at once (#ckm0hsp). One per core, one under `--single-threaded`.
	// Absent when Builds is.
	BuildsPoolSize int `json:"buildsPoolSize,omitempty"`
	// OffReasons is additive within contract 3, and the Go engine's alone: why the settings turn each rule off, for
	// every off the go block's reasons explain (#v11k0g2), which the front door prints beside the rule in coverage.
	// Always written, `{}` when no off is explained, so a reader tells an engine that explains its offs, where a rule
	// off and not named here has no reason, from one that never does, a Swift engine's.
	OffReasons map[string]string `json:"offReasons"`
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

	// EntriesKept counts the packages the host's build kept an entry of for the extra builds to restore from
	// (#b2t9xmf), the host's alone, and written only when not 0.
	EntriesKept int `json:"entriesKept,omitempty"`

	// RestoredFromHost counts the packages an extra build restored from the host's run rather than check: its own
	// findings cache did not hold them, their own inputs were the host's, and everything they read of other packages
	// read the same in the build. DeclinedAtLoad counts the packages its load checked from source after all, since
	// what they read of other packages' types did not hold, and DeclinedAtCheck those its check declined, since what
	// they read of facts or answers did not hold. DeclinedBy counts each decline, and each package that was never a
	// candidate, by why: ownInputs, noHostEntry, neverRestorable, readsUnknown, exportData, typeReads, lookups,
	// factReads, deprecations or answers. ReloadRounds counts every time the build was loaded again, and
	// ReloadRoundsBy each of those under every reason it had: hostDecline, for a package its check declined, or
	// missingAnswer, for a package a question reached restored with no answer of it kept. Each is written only when
	// not 0.
	RestoredFromHost int            `json:"restoredFromHost,omitempty"`
	DeclinedAtLoad   int            `json:"declinedAtLoad,omitempty"`
	DeclinedAtCheck  int            `json:"declinedAtCheck,omitempty"`
	DeclinedBy       map[string]int `json:"declinedBy,omitempty"`
	ReloadRounds     int            `json:"reloadRounds,omitempty"`
	ReloadRoundsBy   map[string]int `json:"reloadRoundsBy,omitempty"`

	// Untouched, additive within contract 3, counts the packages the host kept that an extra build's walk left
	// untouched (#qbvtkfm), restored unjudged and never analyzed, since nothing they read changed in the build, and
	// handed the host's findings, and Visited those it visited, each judged, then held or declined. Their files count
	// as the host's checked files, never as a gap. Each is written only when not 0.
	Untouched int `json:"untouched,omitempty"`
	Visited   int `json:"visited,omitempty"`

	// WrittenThrough counts the packages an extra build restored from the host's run whose entry it wrote under its
	// own key once their restore held, with a findings cache to write to, so its next run hits them in its own store
	// rather than check them from source: the host, hitting its own store, keeps nothing to restore them from then.
	// Written only when not 0, so a run under `--no-cache` writes none.
	WrittenThrough int `json:"writtenThrough,omitempty"`

	// LoadMilliseconds is the build's wall time loading the workspace and LintMilliseconds running the rules over it,
	// every reload's included, so a run can be read build by build. DecideMilliseconds is the time an extra build spent
	// deciding which packages restore from the host's run, summed across the goroutines that decided, so it can exceed
	// the load and lint it ran within, and KeepMilliseconds the time the host's build spent keeping its packages for
	// the others, within its lint: their keys, which a run without a store computes only for this, and what each entry
	// holds of the run's answers, since the entries themselves are encoded when a build first asks for them.
	// WaitMilliseconds is the host's too: how long the extra builds waited after its lint for its walks of what each
	// package read of others to end, before any could start. Each is written only when not 0.
	LoadMilliseconds   int `json:"loadMilliseconds,omitempty"`
	LintMilliseconds   int `json:"lintMilliseconds,omitempty"`
	DecideMilliseconds int `json:"decideMilliseconds,omitempty"`
	KeepMilliseconds   int `json:"keepMilliseconds,omitempty"`
	WaitMilliseconds   int `json:"waitMilliseconds,omitempty"`

	// HeldAtLoadEndBytes and HeldAtLintPeakBytes are an extra build's alone, and written only under `--timing`, whose
	// builds run one at a time: what the build held beyond the host's live heap as its load ended, measured by a
	// forced collection, and the most it held while its rules ran, sampled by one every quarter of a second (#rmzehfg).
	// Their ratio is what the width's buildsWidthPeakFactor stands for, so a tree whose ratio passes it shows in one
	// run. A build under `--timing` restores nothing from the host's run, so it holds more at its load's end than one
	// that restores, and its ratio reads lower.
	HeldAtLoadEndBytes  uint64 `json:"heldAtLoadEndBytes,omitempty"`
	HeldAtLintPeakBytes uint64 `json:"heldAtLintPeakBytes,omitempty"`

	// CheckedFromSource and PackagesLoaded are an extra build's alone (#b2t9xmf): the packages of its whole graph its
	// loads checked from source rather than restored, from its own findings cache or the host's run, summed over every
	// load, a reload's included, and every package its first load loaded, dependencies and test variants included.
	// CheckedFromSourceRounds is each load's count in order, written only when the build was loaded more than once
	// (#k8gty9e). The first load's count over PackagesLoaded is the share that weighs its lint, which takes the whole
	// of a budget of one build the extra builds share once the share is at or above one half, so no two near-full
	// builds lint at once. LintWaitMilliseconds is the wall time its lint waited for that weight, and LintWaited
	// whether it waited at all, so a wait under a millisecond still reads as one. None of them reaches a key or the
	// verdict, and each is written only when not 0 or false, so the host's record writes none.
	CheckedFromSource       int   `json:"checkedFromSource,omitempty"`
	CheckedFromSourceRounds []int `json:"checkedFromSourceRounds,omitempty"`
	PackagesLoaded          int   `json:"packagesLoaded,omitempty"`
	LintWaitMilliseconds    int   `json:"lintWaitMilliseconds,omitempty"`
	LintWaited              bool  `json:"lintWaited,omitempty"`

	// FilesAlone, additive within contract 3 (#ytnvt8k), counts the files of ours in scope that this build alone
	// compiles and a rule ran on: the coverage it buys. NotLinted are the files of ours in scope the host's build
	// leaves out that no build analyzed, absolute and sorted, each under the first build that compiles it, and
	// NotLintedReason says why, as a build whose load found a type error analyzes no package. Such a file was compiled
	// and no rule ran on it, so the front door names it as not checked rather than as checked by this build. All three
	// are absent when there is nothing to count.
	FilesAlone      int      `json:"filesAlone,omitempty"`
	NotLinted       []string `json:"notLinted,omitempty"`
	NotLintedReason string   `json:"notLintedReason,omitempty"`

	// Dependence, additive within contract 3 and the Go engine's alone, is an extra build's under
	// `--build-dependence` beside `--other-builds` (#qkq4gx5): which of the host's package-level declarations the
	// build can change, observed only, so it changes no finding. Absent unless the switch was on, and on the host's
	// record always.
	Dependence *BuildDependence `json:"dependence,omitempty"`
}

// BuildDependence is what one extra build can change of the host's package-level declarations, each a unit: a
// function, a type spec, or a var or const spec. A unit is dependent when its file is not one both builds compile, when
// an object it declares reaches a dependent object, or when its body reads one by a kind of use the change reaches
// (#ewgh523). Workspace counts the packages with a file in scope, their test variants included, and Dependencies every
// other package the host checked. GraphMilliseconds is the host's graph, built once from its load for every build, and
// JudgmentMilliseconds this build's judgment on it: its file sets compared with the host's, its build-only files
// parsed, and the fixed point, each in wall milliseconds.
//
// Facts is the build's facts census (#0ghk7vj), absent when the build's lint did not run: every fact the analyzers
// exported about an object in a file both builds compile, or about a package both have, compared with the host's.
type BuildDependence struct {
	Workspace            DependenceCounts `json:"workspace"`
	Dependencies         DependenceCounts `json:"dependencies"`
	GraphMilliseconds    int              `json:"graphMilliseconds"`
	JudgmentMilliseconds int              `json:"judgmentMilliseconds"`
	Facts                *DependenceFacts `json:"facts,omitempty"`
}

// DependenceFacts is one build's facts beside the host's: Kinds counts them by analyzer and fact type, and Workspace
// and Dependencies count, by the scope of the unit, the units a differing fact reaches. Milliseconds is the
// comparison's wall time. Every build checks every package from source under the census, restoring nothing from the
// host's run, so its facts are its own.
type DependenceFacts struct {
	Kinds        []FactKindCounts `json:"kinds"`
	Workspace    FactCallers      `json:"workspace"`
	Dependencies FactCallers      `json:"dependencies"`
	Milliseconds int              `json:"milliseconds"`
}

// FactKindCounts are one kind of fact's comparisons: the analyzer and the fact's type, the facts compared, those equal
// and those different, a fact only one build exported counted as different, and the first few that differ, sorted,
// each naming its package, object and both values. An answer the fact resolver computed on demand is a kind too, its
// analyzer the resolver's kind and its type "answer", compared only where both builds asked it: AskedInOne counts
// those only one build asked, a question rather than a fact that moved. The Workspace counts are those of facts about
// objects the workspace's units declare.
type FactKindCounts struct {
	Analyzer           string   `json:"analyzer"`
	FactType           string   `json:"factType"`
	Compared           int      `json:"compared"`
	Equal              int      `json:"equal"`
	Different          int      `json:"different"`
	AskedInOne         int      `json:"askedInOne,omitempty"`
	WorkspaceCompared  int      `json:"workspaceCompared"`
	WorkspaceEqual     int      `json:"workspaceEqual"`
	WorkspaceDifferent int      `json:"workspaceDifferent"`
	Examples           []string `json:"examples,omitempty"`
}

// FactCallers are what differing facts reach in one scope, each as units and their syntax nodes. Differing are the
// units declaring an object whose fact differs. FirstOrder are the units the dependence calls independent that call
// one of those statically, the callers a model judging each unit once would have to judge again; Bound treats each such
// caller as differing in turn and follows its callers too, until none is left, an upper bound on the callers to judge
// again, since judging a caller again can change its own facts but need not. Callers are the units called independent
// that call any dependent unit statically, the share that facts through call bodies put in play at most.
type FactCallers struct {
	DifferingUnits  int `json:"differingUnits"`
	DifferingNodes  int `json:"differingNodes"`
	FirstOrderUnits int `json:"firstOrderUnits"`
	FirstOrderNodes int `json:"firstOrderNodes"`
	BoundUnits      int `json:"boundUnits"`
	BoundNodes      int `json:"boundNodes"`
	CallerUnits     int `json:"callerUnits"`
	CallerNodes     int `json:"callerNodes"`
}

// DependenceCounts are one scope's units and nodes, dependent and in all, a unit's nodes being its syntax nodes. Of
// the packages with units, DependentPackages are those with a dependent unit or a unit only the build compiles, and
// DependentPackageNodes their nodes, what a rule run whole on each of them would redo. BuildOnlyUnits and
// BuildOnlyNodes are the units only the build compiles, of the scope's packages and of packages only the build has,
// which no count of the host's holds.
type DependenceCounts struct {
	Units                 int `json:"units"`
	DependentUnits        int `json:"dependentUnits"`
	Nodes                 int `json:"nodes"`
	DependentNodes        int `json:"dependentNodes"`
	Packages              int `json:"packages"`
	DependentPackages     int `json:"dependentPackages"`
	DependentPackageNodes int `json:"dependentPackageNodes"`
	BuildOnlyUnits        int `json:"buildOnlyUnits"`
	BuildOnlyNodes        int `json:"buildOnlyNodes"`
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
// only when it is not 0. The timing record carries neither: a run under `--timing` reads no cache, so it restores
// nothing and never reloads. Replays, beside them though no cache count, is how many questions of a fact computed
// on demand the lint phase answered by replaying the push's walk of a package, which a cold run makes too; it is
// written only when it is not 0. On is false under `--no-cache`, and every count 0
// but FromBinary, which the binary's entries can still give, FactsSkipped of what they restored, and Reloads of them.
// Problems counts what kept the cache from being read or written, the host's build's and every other build's alike,
// each of which the engine also notes on stderr, so a run read through `--json` alone still shows it (#27r8r24).
// WriteFailed counts every miss whose entry could not be written, every build's, of which the engine notes only the
// first as a problem, so a store that refused 400 writes says 400 rather than 1. Each is written only when not 0.
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
	Replays      int  `json:"replays,omitempty"`
	Problems     int  `json:"problems,omitempty"`
	WriteFailed  int  `json:"writeFailed,omitempty"`
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
	if counted.OffReasons == nil {
		counted.OffReasons = map[string]string{}
	}
	return encodeWithoutHtmlEscaping(counted)
}
