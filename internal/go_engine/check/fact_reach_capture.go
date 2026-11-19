package check

import (
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// FactReachCapture is what a host's run held of its facts for the facts' reach (#aswfm7a), read from the run's resolver
// while it was alive: what the engine makes each package's host's side of, for an extra build to leave a function body
// to the host's findings only when every fact and answer the body reads is the same in the build.
type FactReachCapture struct {
	// Packages are, by package ID, each analyzed package the run computed in. A package the run restored, or whose
	// analysis was handed an unsure answer, is absent, which says the run has no side of it, so a build lints it whole.
	Packages map[string]*FactReachPackage

	// Answers are every answer the run holds, about every package's objects, as fact_resolver.Answered orders them, the
	// answers a build is asked of another package's objects before any body leaves. Each is an answer as
	// computedAnswers makes one, with Package nil for an object of a package the load holds none of.
	Answers []ExportedFact
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
}

// factReachKinds is what one captured package's answers are, by kind: each kind that answered about its objects, in the
// order its first answer came, and the package's functions each answered about.
type factReachKinds struct {
	order     []*fact_resolver.Kind
	functions map[*fact_resolver.Kind][]*types.Func
}

// captureFactReach is what resolver, the run's, holds of its facts for the facts' reach of analyzed, read once the
// run's passes are done. It reads every answer once (fact_resolver.Answered), then calls each kind's bound once per
// package. A kind that answered nothing about a package's objects is not called there: its state holds no judgment of
// the package, so its bound is empty, and known. An answer about a parameter is no function the kind answered about,
// and the one kind with a bound, nil-ness, answers only about functions.
func captureFactReach(resolver *fact_resolver.Resolver, analyzed []*packages.Package) *FactReachCapture {
	capture := &FactReachCapture{Packages: make(map[string]*FactReachPackage, len(analyzed))}
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
		hosted := &FactReachPackage{NoReturn: home.NoReturn, OrderKnown: true}
		capture.Packages[analyzedPackage.ID], captured[analyzedPackage.Types] = hosted, hosted
		kinds[analyzedPackage.Types] = &factReachKinds{functions: map[*fact_resolver.Kind][]*types.Func{}}
	}
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
			Encoded: answer.Encoded}
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
