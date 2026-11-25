package check

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// BodyRecorder is the analyzer that records what each function body adds to what it builds of a package under
// Options.RecordBodies, U1000's (unused_code.Analyzer), and RecordedOf, its reader of those records off the analyzer's
// result on a package (unused_code.RecordedOf), false for a result that holds none. Both are U1000's, handed in, so
// this package names no rule package (#f5g7ym6), and the capture holds what RecordedOf reads as it is
// (FactReachPackage.Bodies).
type BodyRecorder struct {
	Analyzer   *analysis.Analyzer
	RecordedOf func(analyzerResult any) (any, bool)
}

// FactReachCapture is what a host's run held of its facts for the facts' reach (#aswfm7a), read from the run's resolver
// while it was alive: what cohere-go makes each package's host's side of, for an extra build to leave a function body
// to the host's findings only when every fact and answer the body reads is the same in the build.
type FactReachCapture struct {
	// Packages are, by package ID, each analyzed package the run computed in. A package the run restored, or whose
	// analysis was handed an unsure answer, is absent, which says the run has no side of it, so a build lints it whole.
	// So is one the store answered a question about (StoreAnswered).
	Packages map[string]*FactReachPackage

	// StoreAnswered are, by package ID, each analyzed package the run learned that Packages leaves out because the
	// store gave an answer about one of its objects (fact_resolver.Package.AnsweredFromStore), as a package loaded from
	// source to answer a question is answered from its entry. The answer was judged in another run, and nothing of what
	// it rests on in this one, so a function the run holds no answer of a kind about may be one it rests on: no kind's
	// record of the package is complete (fact_resolver.Kind.RecordsEveryJudgment), and the package is one to lint
	// whole.
	StoreAnswered map[string]bool

	// Answers are every answer the run holds, about every package's objects, as fact_resolver.Answered orders them, the
	// answers a build is asked of another package's objects before any body leaves. Each is an answer as
	// computedAnswers makes one, with Package nil for an object of a package the load holds none of, and with
	// TypesAlone set where its kind says the object's types alone decide it (fact_resolver.Kind.TypesAlone).
	Answers []ExportedFact

	// Askers are the bodies whose rules asked each answer, by kind's name and object (fact_resolver.Resolver.Askers):
	// an answer no rule asked, one a kind kept or computed inside another's judgment, has none.
	Askers map[string]map[types.Object][]fact_resolver.Asker

	// BodiesRead are the IDs of the packages whose bodies the run read for presence (FactReachPackage.Presence),
	// sorted: the packages it was given, never a dependency, whose side no build is handed.
	BodiesRead []string
}

// FactReachPackage is one analyzed package as the run held its facts.
type FactReachPackage struct {
	// Answers are the answers the run holds about the package's objects, its unexported functions' included, as
	// Answers orders them.
	Answers []ExportedFact

	// NoReturn is the package's no-return predicate, ctrlflow's as the package's analysis read it
	// (fact_resolver.Package.NoReturn): its own functions, unexported ones included, whose fact no action exports, and
	// those of other packages it calls statically.
	NoReturn func(function *types.Func) bool

	// OrderBound are the package's functions whose answers may differ in a walk of the package in another order, every
	// kind's bound (fact_resolver.Kind.OrderBound) over the functions it answered about, each once, in the order the
	// kinds' answers first came and then each bound's own. OrderKnown is false when a kind cannot say, or when the run
	// holds an answer about the package's objects of a kind that declares no bound, whose order dependence nobody has
	// argued, and then the package is one to lint whole.
	OrderBound []types.Object
	OrderKnown bool

	// Pushed are the facts the checker pushes to importers beside ctrlflow's no-return (#wxx4ar6), printf's wrapper
	// facts and the deprecation facts today, by the name of the analyzer that learns them: each object's
	// fact the package's action held once it ran, its own objects' and those its imports exported, in the fact's words
	// (fmt.Sprint), which the build reads alike. An analyzer the run never ran on the package, as where no enabled
	// rule requires it, has no map, and no build compares its facts there: no finding of the package read them.
	Pushed map[string]map[types.Object]string

	// Presence is, by its place, what each function body of the package holds that the presence rows read and no use
	// of stage 1's record says (Options.FactReachPresence), read off the package's syntax while the run still holds
	// it: the functions a body starts, whether it writes a candidate of consistency-no-any's of its own, and the
	// marker interfaces it declares. A body that holds none of them has no entry.
	Presence map[fact_reach.Place]fact_reach.BodyRecord

	// PresenceRead is whether the run read the package's bodies for Presence at all. One it never read holds nothing
	// in Presence that says its bodies hold nothing, so its side counts it unplaced, and a build lints it whole.
	PresenceRead bool

	// Markers are the method names of every marker interface the package's syntax declares, read with its bodies
	// (Options.FactReachPresence), sorted.
	Markers []string

	// Bodies are what U1000's action on the package recorded of each function body it walked, under
	// Options.RecordBodies (#xeafx79), with the digest of every file of the package, a later wave's counting, as
	// Options.BodyRecorder read them (unused_code.BodyRecords, which only cohere-go and U1000 read): what an extra
	// build's U1000 splices in place of walking a body it leaves standing. They are nil where nothing asked, where the
	// run was handed no recorder, where U1000 did not run on the package, or where it failed, and then the package's
	// side answers no body's record, and a build walks every body of it.
	//cohere:ignore consistency-no-any the records are U1000's type, which check cannot name without linking the rule
	Bodies any

	// RecordsHeld is whether Bodies holds what U1000 recorded at all: whether its action on the package ran with its
	// records asked, and did not fail, a package with no body among them. Where it is false, Bodies is empty because
	// nothing was recorded, not because nothing was there to record.
	RecordsHeld bool
}

// factReachKinds is what one captured package's answers are, by kind: each kind that answered about its objects, in the
// order its first answer came, and the package's functions each answered about.
type factReachKinds struct {
	order     []*fact_resolver.Kind
	functions map[*fact_resolver.Kind][]*types.Func
}

// captureFactReach is what resolver, the run's, holds of its facts for the facts' reach of analyzed, read once the
// run's passes are done, the pushed facts each action of graphs held, a later wave's on a package counting, as the
// findings read it, what presence read of each package's bodies, and what recorder's actions recorded of them. It reads
// every answer once (fact_resolver.Answered), then calls each kind's bound once per package. A kind that answered
// nothing about a package's objects is not called there: its state holds no judgment of the package, so its bound is
// empty, and known. An answer about a parameter is no function the kind answered about, and the one kind with a bound,
// nil-ness, answers only about functions. It asks each answer's kind once whether the object's types alone decide the
// answer, and marks the answer where they do.
func captureFactReach(
	resolver *fact_resolver.Resolver,
	analyzed []*packages.Package,
	graphs []*checker.Graph,
	presence *factReachPresence,
	recorder BodyRecorder,
) *FactReachCapture {
	capture := &FactReachCapture{Packages: make(map[string]*FactReachPackage, len(analyzed)),
		StoreAnswered: map[string]bool{}, Askers: resolver.Askers(), BodiesRead: presence.packages()}
	captured := make(map[*types.Package]*FactReachPackage, len(analyzed))
	kinds := make(map[*types.Package]*factReachKinds, len(analyzed))
	for _, analyzedPackage := range analyzed {
		if analyzedPackage.Types == nil || resolver.Unsure(analyzedPackage.Types) {
			continue
		}
		home := resolver.Package(analyzedPackage.Types)
		if home == nil {
			// Restored: ctrlflow never ran on it, so the resolver never learned it, and nothing was computed there.
			continue
		}
		if home.AnsweredFromStore() {
			capture.StoreAnswered[analyzedPackage.ID] = true
			continue
		}
		hosted := &FactReachPackage{NoReturn: home.NoReturn, OrderKnown: true}
		hosted.Presence, hosted.PresenceRead = presence.of(analyzedPackage.ID)
		hosted.Markers = presence.markersOf(analyzedPackage.ID)
		capture.Packages[analyzedPackage.ID], captured[analyzedPackage.Types] = hosted, hosted
		kinds[analyzedPackage.Types] = &factReachKinds{functions: map[*fact_resolver.Kind][]*types.Func{}}
	}
	capturePushed(captured, graphs)
	captureBodies(captured, graphs, recorder)
	answered := resolver.Answered()
	if len(answered) == 0 {
		return capture
	}
	owners := map[*types.Package]*packages.Package{}
	packages.Visit(analyzed, nil, func(owner *packages.Package) {
		if owner.Types != nil {
			owners[owner.Types] = owner
		}
	})
	capture.Answers = make([]ExportedFact, 0, len(answered))
	for _, answer := range answered {
		fact := ExportedFact{Package: owners[answer.Object.Pkg()], Analyzer: answer.Kind.Name,
			FactType: answerFactType, Object: answer.Object, Digest: cache.DigestOf(answer.Encoded), Answer: true,
			Encoded: answer.Encoded, TypesAlone: answer.Kind.TypesAlone != nil && answer.Kind.TypesAlone(answer.Object)}
		capture.Answers = append(capture.Answers, fact)
		hosted := captured[answer.Object.Pkg()]
		if hosted == nil {
			continue
		}
		hosted.Answers = append(hosted.Answers, fact)
		if answer.Kind.OrderBound == nil {
			hosted.OrderKnown = false
			continue
		}
		answeredKinds := kinds[answer.Object.Pkg()]
		if _, isAnswered := answeredKinds.functions[answer.Kind]; !isAnswered {
			answeredKinds.order = append(answeredKinds.order, answer.Kind)
			answeredKinds.functions[answer.Kind] = nil
		}
		if function, isFunction := answer.Object.(*types.Func); isFunction {
			answeredKinds.functions[answer.Kind] = append(answeredKinds.functions[answer.Kind], function)
		}
	}
	for packageTypes, hosted := range captured {
		answeredKinds := kinds[packageTypes]
		var bounded map[types.Object]bool
		for _, kind := range answeredKinds.order {
			bound, known := kind.OrderBound(resolver.Package(packageTypes), answeredKinds.functions[kind])
			hosted.OrderKnown = hosted.OrderKnown && known
			for _, function := range bound {
				if bounded == nil {
					bounded = map[types.Object]bool{}
				}
				if !bounded[function] {
					bounded[function] = true
					hosted.OrderBound = append(hosted.OrderBound, function)
				}
			}
		}
	}
	return capture
}

// capturePushed holds, on each package of captured, the pushed facts each of its actions in graphs held, in their
// words, of every analyzer that learns facts but ctrlflow, whose facts the resolver holds of each package already
// (fact_resolver.Package.NoReturn), an action that failed holding none an importer could trust. It reads the actions'
// facts once their passes are done, which no pass's read records: a listing a pass made would read the whole closure.
func capturePushed(captured map[*types.Package]*FactReachPackage, graphs []*checker.Graph) {
	for _, graph := range graphs {
		for action := range graph.All() {
			hosted := captured[action.Package.Types]
			if hosted == nil || action.Err != nil {
				continue
			}
			if len(action.Analyzer.FactTypes) == 0 || action.Analyzer == ctrlflow.Analyzer {
				continue
			}
			facts := map[types.Object]string{}
			for _, fact := range action.AllObjectFacts() {
				facts[fact.Object] = fmt.Sprint(fact.Fact)
			}
			if hosted.Pushed == nil {
				hosted.Pushed = map[string]map[types.Object]string{}
			}
			hosted.Pushed[action.Analyzer.Name] = facts
		}
	}
}

// captureBodies holds, on each package of captured, what recorder's action on it in graphs recorded of its bodies, and
// that it was recorded, an action that failed holding none a build could trust. A run not asked to record has U1000's
// results hold none, and the packages none, as a run without U1000 does (BodyRecorder.RecordedOf), and a run handed no
// recorder holds none either, so a caller that forgets it has its builds walk every body.
func captureBodies(captured map[*types.Package]*FactReachPackage, graphs []*checker.Graph, recorder BodyRecorder) {
	if recorder.Analyzer == nil || recorder.RecordedOf == nil {
		return
	}
	for _, graph := range graphs {
		for action := range graph.All() {
			if action.Analyzer != recorder.Analyzer || action.Err != nil {
				continue
			}
			hosted := captured[action.Package.Types]
			if hosted == nil {
				continue
			}
			if recorded, isRecorded := recorder.RecordedOf(action.Result); isRecorded {
				hosted.Bodies, hosted.RecordsHeld = recorded, true
			}
		}
	}
}
