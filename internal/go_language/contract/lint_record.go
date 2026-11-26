package contract

// LintRecord summarizes the lint phase, for the lint line and its coverage notes.
type LintRecord struct {
	Findings int `json:"findings"`
	// TypeErrors is additive within contract 3, and cohere-go's alone: the type errors of the run's other builds,
	// checked unless `--host-build-only` is named, which the lint phase writes as compiler finding records beside the
	// Findings it counts, since only the merge of every build's verdict knows them (#rw6tg3a). The finding records of
	// the phase are Findings plus TypeErrors. Absent for a run of one build, so such a run writes the record it always
	// has.
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
	// FilesReplayed is additive within contract 3 (documentation/contract/Contract.md): of FilesWalked, the files whose
	// every rule's verdict cohere-go read from a per-file verdict store rather than computed, as cohere-swift's lint
	// verdict cache does. cohere-go leaves it unset and so unwritten: its cache restores packages (Cache below), and
	// how many files a restored package stands for is not a count it keeps, so what the field means for a Go run is for
	// cohere-go's owner to define. It is a pointer so a Swift record's count, 0 included, survives a reencode.
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

	// Cache is what cohere-go's per-package findings cache did in the lint phase, additive within contract 3
	// as the withheld counts are. It changes no finding and no count above.
	Cache LintCache `json:"cache"`

	// Builds is additive within contract 3: every build a run checked when it checked more than one, the host's first
	// (#72n9mq0). It is absent for a run of one build, so such a run writes the record it always has.
	Builds []BuildRecord `json:"builds,omitempty"`
	// BuildsAtOnce is additive within contract 3, and cohere-go's alone: how many of the extra builds ran side by
	// side, decided once from the memory cap, the host's live heap and what the heaviest build holds as its load ends
	// (#b2t9xmf, #rmzehfg). It changes no finding, no note and no key: a run one build at a time writes the same
	// records but this one. Absent when Builds is.
	BuildsAtOnce int `json:"buildsAtOnce,omitempty"`
	// BuildsWidthLiveBytes is additive within contract 3, and cohere-go's alone: the host's live heap that decision
	// was given, measured by a collection as the extra builds started with nothing else running, so a reader sees why a
	// width was chosen. Absent when the decision needed none: under `--single-threaded`, a limit other than the cap,
	// builds that restore nothing from the host for any reason but the facts census (#f9br7sg), or one build alone,
	// which no heap can widen.
	BuildsWidthLiveBytes uint64 `json:"buildsWidthLiveBytes,omitempty"`
	// BuildsWidthBuildBytes is additive within contract 3, and cohere-go's alone: the holding each extra build was
	// budgeted beyond that heap as its load ends: what the first build to start, the heaviest, held then, measured the
	// same way, in a run with a findings store, and in a run without one 1.2 times that, or 1.2 times that heap when
	// BuildsWidthFromHostLive (#ghb3rcn). With
	// BuildsWidthPeakFactor, the times that holding a build is taken to reach at its lint's peak, 3.5 in a run with a
	// findings store and 2.5 without one (#hv090f4), and the memory record's limit, a reader redoes the arithmetic:
	// floor((limit - live) / (holding * factor)), no more than the builds and at least one (#rmzehfg). Both are absent
	// when BuildsWidthLiveBytes is.
	BuildsWidthBuildBytes uint64  `json:"buildsWidthBuildBytes,omitempty"`
	BuildsWidthPeakFactor float64 `json:"buildsWidthPeakFactor,omitempty"`
	// BuildsWidthSlack is additive within contract 3, and cohere-go's alone: whether the width's slack added
	// BuildsAtOnce's last build, one past that floor, since one more fit by the largest ratio measured in the run's
	// mode, 3.22 with a findings store and 2.16 without one, with 512 MiB to spare (#ewkp7w0). Absent when it did not.
	BuildsWidthSlack bool `json:"buildsWidthSlack,omitempty"`
	// BuildsWidthFromHostLive is additive within contract 3, and cohere-go's alone: whether the width was decided
	// from the host's live heap alone, at the share gate every extra build waits at before its type check in a run
	// without a findings store, or in one with a store whose host missed at most 0.10 of the packages its store hit or
	// missed (#ybjbqm2), since every build's share there, its CheckedFromSourceAtGate over its PackagesLoaded, was at
	// or under 0.10 (#ghb3rcn). BuildsWidthBuildBytes is then that heap times 1.2, the holding budgeted each build, and
	// nothing was measured again. Absent when the first build's holding was measured instead, a share being over the
	// line or the run having a store its host mostly missed, which takes no gate, or the width needed no heap.
	BuildsWidthFromHostLive bool `json:"buildsWidthFromHostLive,omitempty"`
	// BuildsPeakProblems is additive within contract 3, and cohere-go's alone: a sentence for each extra build of
	// a run under `--timing` whose LintPeakOverLoadEnd passed the largest ratio measured in the run's mode, which the
	// slack spends to, naming the build, its ratio and the constant, each also noted on stderr (#ewkp7w0). Absent
	// when none did.
	BuildsPeakProblems []string `json:"buildsPeakProblems,omitempty"`
	// BuildsPoolSize is additive within contract 3, and cohere-go's alone: how many slots the run's one pool holds,
	// which every load and check of the run shares, the host's and every extra build's, so however many builds run side
	// by side the run never runs more of that work at once (#ckm0hsp). One per core, one under `--single-threaded`.
	// Absent when Builds is.
	BuildsPoolSize int `json:"buildsPoolSize,omitempty"`
	// OffReasons is additive within contract 3, and cohere-go's alone: why the settings turn each rule off, for
	// every off the go block's reasons explain (#v11k0g2), which the front door prints beside the rule in coverage.
	// Always written, `{}` when no off is explained, so a reader tells a child that explains its offs, where a rule
	// off and not named here has no reason, from one that never does, cohere-swift's.
	OffReasons map[string]string `json:"offReasons"`
	// ChildProcessesAfterLoad is additive within contract 3, and cohere-go's alone: how many go commands the run
	// started after its first load began, and ChildProcessesAfterLoadNamed names each, go and its first arguments,
	// sorted (#sktb2y8). On darwin each child started once cohere-go's heap has grown makes every resident page fault
	// again, so a run with extra builds starts every go command its builds need before that load, and reads 0 here; a
	// run of one build starts its own lister's two beside its census and reads 0 too (#q9ezwvw), except under
	// `--timing`, where it starts them within its first load. Both are absent when none started. They count the go
	// commands of the run's cached loads, every one the host's lint and its builds make: a load with no cache account,
	// as the rename's, records none, and neither does x/tools' or go/build's own go command, so 0 says no more than
	// that.
	ChildProcessesAfterLoad      int      `json:"childProcessesAfterLoad,omitempty"`
	ChildProcessesAfterLoadNamed []string `json:"childProcessesAfterLoadNamed,omitempty"`
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
	// factReads, deprecations, answers (one differs) or unnamedAnswers (one could not be named). ReloadRounds counts
	// every time the build was loaded again, and ReloadRoundsBy each of those under every reason it had: hostDecline,
	// for a package its check declined, or missingAnswer, for a package a question reached restored with no answer of
	// it kept. Each is written only when not 0.
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

	// SystemCacheRestored counts the standard-library packages the build restored from the system cache's table of its
	// own build context (#syscache), and SystemCacheWritten the entries it added to that table this run. Additive
	// within contract 3, and cohere-go's alone. Each is written only when not 0, so a build under
	// `--no-system-cache`, or one whose table held everything it reached, writes none.
	SystemCacheRestored int `json:"systemCacheRestored,omitempty"`
	SystemCacheWritten  int `json:"systemCacheWritten,omitempty"`
	// SharedBodyHits, additive within contract 3, counts the packages a build hit in its own findings cache whose body
	// it took from the run's memo of decoded bodies (#qqyq61s), the bodies the host's session read from its store and
	// those an earlier build read, rather than read the host's record its own names and decode the body again. Written
	// only when not 0, so a cold run, whose host read nothing from its store, and a run under `--no-cache` write none.
	SharedBodyHits int `json:"sharedBodyHits,omitempty"`
	// Packs, additive within contract 3, is what an extra build did with its packs (#b2t9xmf), absent on the host's
	// record, which keeps none, and for a build whose rules never ran.
	Packs *BuildPacks `json:"packs,omitempty"`

	// InheritedFiles counts the distinct files an extra build took the host's findings of at least one syntax-only
	// rule on, rather than walk them again in a package it checked from source (#y6q1x2j), and InheritedFindings the
	// findings it took, each rule's on each file once. A build that restores nothing from the host's run, as under
	// `--timing`, inherits nothing. Each is written only when not 0.
	InheritedFiles    int `json:"inheritedFiles,omitempty"`
	InheritedFindings int `json:"inheritedFindings,omitempty"`

	// StandingDeclarations, additive within contract 3, counts the standing declarations an extra build's confined
	// rules took the host's findings in, rather than hear those function bodies again, under `COHERE_STANDING_BODIES=1`
	// on the body reach census's path (#e4rd5dw), and StandingFindings the findings they took there. A declaration is
	// counted once per file of each package that took it, however many rules did. Each is written only when not 0.
	StandingDeclarations int `json:"standingDeclarations,omitempty"`
	StandingFindings     int `json:"standingFindings,omitempty"`

	// LoadMilliseconds is the build's wall time loading the workspace and LintMilliseconds running the rules over it,
	// every reload's included, so a run can be read build by build. DecideMilliseconds is the time an extra build spent
	// deciding which packages restore from the host's run, summed across the goroutines that decided, so it can exceed
	// the load and lint it ran within, and KeepMilliseconds the time the host's build spent keeping its packages for
	// the others, within its lint: their keys, which a run without a store computes only for this, and what each entry
	// holds of the run's answers, since the entries themselves are encoded when a build first asks for them.
	// WaitMilliseconds is the host's too: how long the extra builds waited after its lint for its walks of what each
	// package read of others to end, before any could start. ShareGateWaitMilliseconds, additive within contract 3,
	// is an extra build's alone (#ghb3rcn): how long its first load waited at the share gate before its type check, in
	// a run without a findings store, for every build to arrive and then its turn, which LoadMilliseconds leaves out,
	// so a build's load time is its own whatever its position. Each is rounded up to a whole millisecond, so a time
	// measured is never 0, and is written only when not 0 (#cqqs9tp).
	LoadMilliseconds          int `json:"loadMilliseconds,omitempty"`
	LintMilliseconds          int `json:"lintMilliseconds,omitempty"`
	DecideMilliseconds        int `json:"decideMilliseconds,omitempty"`
	KeepMilliseconds          int `json:"keepMilliseconds,omitempty"`
	WaitMilliseconds          int `json:"waitMilliseconds,omitempty"`
	ShareGateWaitMilliseconds int `json:"shareGateWaitMilliseconds,omitempty"`

	// HeldAtLoadEndBytes and HeldAtLintPeakBytes are an extra build's alone, and written only under `--timing`, whose
	// builds run one at a time: what the build held beyond the host's live heap as its load ended, measured by a
	// forced collection, and the most it held while its rules ran, sampled by one every quarter of a second (#rmzehfg).
	// Their ratio is what the width's buildsWidthPeakFactor stands for, so a tree whose ratio passes it shows in one
	// run. A build under `--timing` restores nothing from the host's run, so it holds more at its load's end than one
	// that restores, and its ratio reads lower.
	HeldAtLoadEndBytes  uint64 `json:"heldAtLoadEndBytes,omitempty"`
	HeldAtLintPeakBytes uint64 `json:"heldAtLintPeakBytes,omitempty"`
	// LintPeakOverLoadEnd is HeldAtLintPeakBytes over HeldAtLoadEndBytes, to the thousandth, written when both are
	// (#ewkp7w0): a ratio past the largest measured in the run's mode is named in the lint record's
	// BuildsPeakProblems.
	LintPeakOverLoadEnd float64 `json:"lintPeakOverLoadEnd,omitempty"`

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
	// CheckedFromSourceAtGate is additive within contract 3, and an extra build's alone (#ghb3rcn): the packages its
	// first load would check from source as it reached the point before its type check where, in a run without a
	// findings store, the share gate holds it, every one not restored. It is written whatever the run. It is a lower
	// bound on the first round of CheckedFromSource, since a restore decided late can still decline, and its share over
	// PackagesLoaded is the one the gate's decision reads, so a tree whose gate and final shares part widely is
	// findable from the record. Absent when 0.
	CheckedFromSourceAtGate int  `json:"checkedFromSourceAtGate,omitempty"`
	LintWaitMilliseconds    int  `json:"lintWaitMilliseconds,omitempty"`
	LintWaited              bool `json:"lintWaited,omitempty"`

	// FilesAlone, additive within contract 3 (#ytnvt8k), counts the files of ours in scope that this build alone
	// compiles and a rule ran on: the coverage it buys. NotLinted are the files of ours in scope the host's build
	// leaves out that no build analyzed, absolute and sorted, each under the first build that compiles it, and
	// NotLintedReason says why, as a build whose load found a type error analyzes no package. Such a file was compiled
	// and no rule ran on it, so the front door names it as not checked rather than as checked by this build. All three
	// are absent when there is nothing to count.
	FilesAlone      int      `json:"filesAlone,omitempty"`
	NotLinted       []string `json:"notLinted,omitempty"`
	NotLintedReason string   `json:"notLintedReason,omitempty"`

	// Dependence, additive within contract 3 and cohere-go's alone, is an extra build's under
	// `--build-dependence` in a run of every build (#qkq4gx5): which of the host's package-level declarations the
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

	// Restored, additive (#ss4t5g6), is what the judgment did for the packages the host restored from export data,
	// absent when it restored none.
	Restored *RestoredDependence `json:"restored,omitempty"`
}

// RestoredDependence is one build's judgment of what the host restored from export data rather than checked from
// source, which its graph does not hold: the system cache's standard-library table's packages, and a warm store's.
// Packages counts them. The standard library's are judged on a graph of their own, loaded from source beside the host's
// lint: StandardLibraryPackages counts the packages that load reached, LoadMilliseconds is its list, parse and type
// check and GraphMilliseconds its graph, both once a run, and StandardLibraryUnits and StandardLibraryDependentUnits
// are this build's judgment of it, which the dependencies' counts include. Each object of a restored package a unit of
// the host reads is answered by its counterpart in that graph: Answered counts those, WithoutCounterpart those whose
// path names nothing alike there, and WithoutPackage those of a package the graph does not hold, a store's or every one
// when the side load failed, the last two answered dependent, failing wide. StandardLibraryFromRecords, additive
// (#6a3kjvz), counts the standard library's packages the graph took from the records the findings store keeps of them,
// which the side load skipped, and StandardLibrarySideLoaded those the side load loaded, whose records it then writes;
// each is written only when not 0.
type RestoredDependence struct {
	Packages                      int `json:"packages"`
	StandardLibraryPackages       int `json:"standardLibraryPackages"`
	LoadMilliseconds              int `json:"loadMilliseconds"`
	GraphMilliseconds             int `json:"graphMilliseconds"`
	StandardLibraryUnits          int `json:"standardLibraryUnits"`
	StandardLibraryDependentUnits int `json:"standardLibraryDependentUnits"`
	Answered                      int `json:"answered"`
	WithoutCounterpart            int `json:"withoutCounterpart"`
	WithoutPackage                int `json:"withoutPackage"`

	StandardLibraryFromRecords int `json:"standardLibraryFromRecords,omitempty"`
	StandardLibrarySideLoaded  int `json:"standardLibrarySideLoaded,omitempty"`
}

// DependenceFacts is one build's facts beside the host's: Kinds counts them by analyzer and fact type, and Workspace
// and Dependencies count, by the scope of the unit, the units a differing fact reaches. Milliseconds is the
// comparison's wall time. Every build restores nothing from the host's run under the census, so its facts are its own.
// The standard library the host restored from the system cache's table is compared through the table's facts and
// answers (#ss4t5g6), but for a fact about an object no objectpath names, which no table holds. Unkeyed, additive
// (#h660y7d), counts the facts the census left out of the comparison, on either side, of a package the host restored
// whose standard-library graph is records, the store's, and the answers it could not place in any package that graph
// holds, and UnkeyedParts says why, part by part, their sum being Unkeyed. Both are always written, beside the callers
// the census counts, since those leave the facts out.
//
// WithoutSyntax, additive (#wzre5eg), counts the facts the census left out of the comparison, on either side, of a
// package both builds have by one ID that the build restored, from the system cache or from its own store, rather
// than checked from source: such a package has no syntax in the build, so the census compares none of its facts, the
// standard library's in every build that restores it as a plain run's does. A fact is counted where the census would
// have compared it, about the package itself or about an object in a file both builds compile, one both sides hold
// counted on each, and so is each fact the build's entry holds by a path alone. It is always written, as Unkeyed is: a
// census that counts no fact as different has compared only what Kinds counts, and this says what it did not.
//
// CompiledInOne, additive (#w5dbvdm), counts the facts the census left out, on either side, of a package both builds
// have by one ID, about an object in a file the two do not both compile: the other build has no such object, so the
// fact is no comparison's, whether the build checked the package from source or restored it, and neither Kinds nor
// WithoutSyntax counts it. Each is counted on the side that holds it. It is always written, as WithoutSyntax is, which
// it stands beside: the one says what was left out for want of the build's syntax, and this what was left out for
// want of the other build's file. A package one build alone has is no census's, and its facts are in no count.
type DependenceFacts struct {
	Kinds         []FactKindCounts `json:"kinds"`
	Workspace     FactCallers      `json:"workspace"`
	Dependencies  FactCallers      `json:"dependencies"`
	Milliseconds  int              `json:"milliseconds"`
	Unkeyed       int              `json:"unkeyed"`
	UnkeyedParts  UnkeyedFacts     `json:"unkeyedParts"`
	WithoutSyntax int              `json:"withoutSyntax"`
	CompiledInOne int              `json:"compiledInOne"`
}

// UnkeyedFacts are the facts a census left out, by why (#h660y7d). Unnamed are the host's facts no name a record gives
// reaches. Closures are the build's facts about an object no objectpath names, which no host can hold. Unplaced are
// the facts whose object no unit of the graph declares or holds, and the build's facts of the same package, kind and
// name as such a host fact's, or of its kind at all where the host's has no name. PathOnly are the host's facts held by
// a method's or a field's path alone, which nothing pairs exactly, and the build's facts of the kind in the unit that
// no named fact of the host's claims. Collisions are the facts of one kind that meet under one unit and name on either
// side, which nothing tells apart, the other side's fact under that key, and, where the host's meet, the build's facts
// of the kind in the unit that no named fact of the host's claims. Answers are the build's answers about a function, or
// about a parameter or result of one, the census could not place in the graph's units, or that two of the host's
// answers meet at, whose host side is unknown.
type UnkeyedFacts struct {
	Unnamed    int `json:"unnamed"`
	Closures   int `json:"closures"`
	Unplaced   int `json:"unplaced"`
	PathOnly   int `json:"pathOnly"`
	Collisions int `json:"collisions"`
	Answers    int `json:"answers"`
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
// written entries left out because nothing outside their package can name their object, and FactsSkipped the restored
// facts whose object the package's export data does not hold, once for each analyzer whose action restores them: a
// build restores a package's facts only where a pass above it inherits them, so it counts only the facts of the
// restores it makes. The record's count is the host's run's alone, so that describes the extra builds' own counts,
// which it does not carry. Waves counts the times the lint phase analyzed packages: at most two, those whose own inputs
// moved and then the rest that missed, once the hits the early cutoff found among the packages above the first were
// restored. FromSystemCache counts the standard-library packages restored from the system cache's tables (#syscache),
// the host's build's and every other build's summed, which nothing analyzed, apart from hit; it is written only when it
// is not 0. SystemCacheWritten counts the entries the run's builds added to their tables, summed, written only when not
// 0, and SystemCacheOff is set under `--no-system-cache`, which neither reads nor writes the system cache.
// SystemCacheNotKept is why the run kept no system cache though it was on, or could not keep it as it should, in one
// line, as a root it could not find, a directory it could not write or a trim that failed; it is written only when
// there is one, and a run under `--no-system-cache` has none. Reloads counts the packages the run loaded again from
// source, though the cache held them, because a question of a fact computed on demand reached one restored with no
// answer stored (#w7zmvat wave 3), and ReloadRounds how many times the run loaded and linted again for them; each is
// written only when it is not 0. The timing record carries neither: a run under `--timing` reads no cache, so it
// restores nothing and never reloads. Replays, beside them though no cache count, is how many questions of a fact
// computed on demand the lint phase answered by replaying the push's walk of a package, which a cold run makes too; it
// is written only when it is not 0. On is false under `--no-cache`, and every count 0 but FromSystemCache, which the
// system cache can still give, FactsSkipped of what they restored, and Reloads of them.
// Problems counts what kept the cache from being read or written, the host's build's and every other build's alike,
// each of which cohere-go also notes on stderr, so a run read through `--json` alone still shows it (#27r8r24).
// WriteFailed counts every miss whose entry could not be written, every build's, of which cohere-go notes only the
// first as a problem, so a store that refused 400 writes says 400 rather than 1; a dependence record that could not be
// written beside its entry counts there too. Each is written only when not 0. DependenceRecords, additive within
// contract 3 and cohere-go's alone, is written only by a run under `--build-dependence` with a store
// (#6a3kjvz). Packs, additive within contract 3, is what every extra build did with its packs, summed (#b2t9xmf),
// absent in a run with no extra build.
type LintCache struct {
	On                 bool   `json:"on"`
	Hit                int    `json:"hit"`
	FromSystemCache    int    `json:"standardLibraryFromSystemCache,omitempty"`
	SystemCacheWritten int    `json:"systemCacheWritten,omitempty"`
	SystemCacheOff     bool   `json:"systemCacheOff,omitempty"`
	SystemCacheNotKept string `json:"systemCacheNotKept,omitempty"`
	Missed             int    `json:"missed"`
	Written            int    `json:"written"`
	Unstorable         int    `json:"unstorable"`
	FactsDropped       int    `json:"factsDropped"`
	FactsSkipped       int    `json:"factsSkipped"`
	Waves              int    `json:"waves"`
	Reloads            int    `json:"reloads,omitempty"`
	ReloadRounds       int    `json:"reloadRounds,omitempty"`
	Replays            int    `json:"replays,omitempty"`
	Problems           int    `json:"problems,omitempty"`
	WriteFailed        int    `json:"writeFailed,omitempty"`

	DependenceRecords *DependenceRecords `json:"dependenceRecords,omitempty"`
	Packs             *BuildPacks        `json:"packs,omitempty"`
}

// BuildPacks counts what extra builds did with their packs (#b2t9xmf): each build keeps one pack of each store it reads
// records from, its findings cache's and the system cache's, a file holding every record it read or wrote there in its
// last run, with the stamp of each shard directory of the store they were read from, which it reads once rather than
// open each record. Read counts the packs read whole, and Missing those there were none of, as on a build's first run
// or a new executable's, which is no refusal. RecordsServed counts the records the packs served, which the build opened
// no file for: a record is served only while its shard's stamp is the one the pack holds, so the store holds exactly
// what the pack was made of. RecordsStale counts the records a pack held whose shard had moved since, as any write,
// removal or trim of a file there moves it, and RecordsMissed the records asked that no pack held, each read from the
// store, found or not. ShardReads counts the shard stamps read, each shard of each store once a run whatever the
// builds. RefusedBy counts each pack that was there and was ignored whole, by the first word of why: unreadable, short,
// sum, version or malformed, absent when none was; a build whose pack was refused reads its store as it would with
// none, and its findings are the same. Written counts the packs written at a build's save, since its run read or wrote
// a record its pack did not hold, a shard's stamp moved, or its pack was missing or refused, so a warm run whose packs
// served every record writes none.
type BuildPacks struct {
	Read          int            `json:"read"`
	Missing       int            `json:"missing"`
	RecordsServed int            `json:"recordsServed"`
	RecordsStale  int            `json:"recordsStale"`
	RecordsMissed int            `json:"recordsMissed"`
	ShardReads    int            `json:"shardReads"`
	RefusedBy     map[string]int `json:"refusedBy,omitempty"`
	Written       int            `json:"written"`
}

// DependenceRecords counts what the host's run did with block 1's dependence records, which the findings store keeps
// beside each package's entry under `--build-dependence` (#6a3kjvz): read, the records found under their packages'
// keys, each added to the host's graph in place of the package's syntax; written, those written beside their
// entries; backfilled, the hits by their first key with no record, made a miss once so their records are written;
// aliasSkipped, the hits by a second key's alias that hold with no record beside the alias's target, which go without
// one; and unreadable, the records that did not decode, each a problem, whose packages are checked from source so
// their records are written again, a hit by its first key among them counted in backfilled too, one by an alias in
// aliasSkipped; and droppedAfterBackfill, the hits made misses in a load that backfilled any, since a backfilled
// package asks the facts of everything below it, written only when not 0. aliasRead, additive (#6a3kjvz piece 4),
// counts the hits by an alias that hold whose record is the one beside the alias's target, written only when not 0.
// The standard library's, additive (#6a3kjvz piece 3), count the records of the packages the host restored from the
// system cache's table, which the store keeps apart: standardLibraryRead and standardLibraryUnreadable over the load,
// and standardLibraryWritten once the side load has ended, a write that failed counted in the cache's writeFailed. Each
// is written only when not 0.
type DependenceRecords struct {
	Read         int `json:"read"`
	Written      int `json:"written"`
	Backfilled   int `json:"backfilled"`
	AliasSkipped int `json:"aliasSkipped"`
	Unreadable   int `json:"unreadable"`

	DroppedAfterBackfill int `json:"droppedAfterBackfill,omitempty"`
	AliasRead            int `json:"aliasRead,omitempty"`

	StandardLibraryRead       int `json:"standardLibraryRead,omitempty"`
	StandardLibraryWritten    int `json:"standardLibraryWritten,omitempty"`
	StandardLibraryUnreadable int `json:"standardLibraryUnreadable,omitempty"`
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
