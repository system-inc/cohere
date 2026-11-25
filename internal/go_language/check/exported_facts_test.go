package check

import (
	"errors"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// waveFact is the fact waveAnalyzer exports about its package, the wave that exported it, so a census can tell one
// wave's fact from another's.
type waveFact struct {
	Wave int
}

// AFact makes waveFact an analysis.Fact.
func (*waveFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// waveAnalyzer is one wave's analyzer named fact_wave: it exports a waveFact of wave about its package unless wave is
// 0, and fails after when fails is set. Each wave gets an analyzer of its own under the one name, as a census keys an
// action by its analyzer's name, so two analyses of one package are two waves of one action.
func waveAnalyzer(wave int, fails bool) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:      "fact_wave",
		Doc:       "exports the wave that ran it about its package, and may fail after",
		FactTypes: []analysis.Fact{new(waveFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			if wave != 0 {
				pass.ExportPackageFact(&waveFact{Wave: wave})
			}
			if fails {
				return nil, errors.New("wave failed after exporting")
			}
			return nil, nil
		},
	}
}

// probedOf is probed, the reach workspace's package that imports nothing, failing the test when the load holds none.
func probedOf(test *testing.T, analyzed []*packages.Package) *packages.Package {
	test.Helper()
	for _, analyzedPackage := range analyzed {
		if analyzedPackage.PkgPath == "example.com/reach/probed" {
			return analyzedPackage
		}
	}
	test.Fatal("the reach workspace holds no probed")
	return nil
}

// waveFactsOf is what a census of the waves' graphs over probed holds of fact_wave, each wave analyzing probed in its
// own graph, in order.
func waveFactsOf(test *testing.T, waves ...*analysis.Analyzer) []ExportedFact {
	test.Helper()
	probed := probedOf(test, loadReach(test))
	var graphs []*checker.Graph
	for _, wave := range waves {
		graph, analyzeError := analyzeWrapped([]*analysis.Analyzer{wave}, []*packages.Package{probed}, nil)
		if analyzeError != nil {
			test.Fatalf("analyzing probed: %v", analyzeError)
		}
		graphs = append(graphs, graph)
	}
	var facts []ExportedFact
	for _, fact := range exportedFacts(graphs) {
		if fact.Analyzer == "fact_wave" {
			facts = append(facts, fact)
		}
	}
	return facts
}

// wavesOf is the wave each of facts holds, in order, -1 for one that is not a waveFact.
func wavesOf(facts []ExportedFact) []int {
	var waves []int
	for _, fact := range facts {
		held, isWave := fact.Fact.(*waveFact)
		if !isWave {
			waves = append(waves, -1)
			continue
		}
		waves = append(waves, held.Wave)
	}
	return waves
}

// An exported fact's digest is of its value (factValueDigest), so two builds whose facts of one kind hold different
// values compare different and two that hold equal values compare equal. A digest left at zero would make every fact
// of a kind compare equal to every other, whatever it holds.
func TestAnExportedFactKeepsItsValueDigest(test *testing.T) {
	test.Parallel()
	one := exportedFact(actionKey{}, nil, &valueFact{Count: 1})
	alsoOne := exportedFact(actionKey{}, nil, &valueFact{Count: 1})
	two := exportedFact(actionKey{}, nil, &valueFact{Count: 2})
	if one.Undigestable || two.Undigestable {
		test.Fatalf("want both facts digested, got undigestable %t and %t", one.Undigestable, two.Undigestable)
	}
	if one.Digest == two.Digest {
		test.Errorf("a fact counting 1 and one counting 2 both digest to %v, want them apart", one.Digest)
	}
	if one.Digest != alsoOne.Digest {
		test.Errorf("two facts counting 1 digest to %v and %v, want them alike", one.Digest, alsoOne.Digest)
	}
}

// A fact factValueDigest cannot walk, one whose interface field holds a chan, is marked undigestable, which a census
// reads as differing: unmarked, it would keep the zero digest, and two such facts would compare equal.
func TestAnExportedFactWithoutADigestIsMarked(test *testing.T) {
	test.Parallel()
	held := exportedFact(actionKey{}, nil, &valueFact{Held: make(channelStringer)})
	if !held.Undigestable {
		test.Errorf("a fact holding a chan is undigestable %t, digest %v, want it marked", held.Undigestable,
			held.Digest)
	}
}

// A computed answer's digest is of its JSON, as the host's side of a census digests a restored answer
// (restoredAnswer), so two builds' answers about one object compare by what they answered: probed's Probed, which
// reachProbe answers true, is held once, about its object, with the digest of "true".
func TestAComputedAnswerKeepsTheDigestOfItsJson(test *testing.T) {
	test.Parallel()
	analyzed := loadReach(test)
	options := DispatchOptions()
	resolver := fact_resolver.New(answerlessStore{})
	options.Resolver = resolver
	_, analyzeError := analyzeWrapped([]*analysis.Analyzer{reachProbeAnalyzer}, analyzed, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the reach workspace: %v", analyzeError)
	}
	var answers []ExportedFact
	for _, answer := range computedAnswers(resolver, analyzed) {
		if answer.Analyzer == reachProbe.Name {
			answers = append(answers, answer)
		}
	}
	if len(answers) != 1 {
		test.Fatalf("the census holds %d answers of %s, want Probed's alone: %v", len(answers), reachProbe.Name,
			answeredNames(answers))
	}
	answer := answers[0]
	if !answer.Answer || answer.FactType != answerFactType || answer.Object.Name() != "Probed" ||
		string(answer.Encoded) != "true" {
		test.Errorf("the answer is %+v, want an answer about Probed encoded true", answer)
	}
	if answer.Digest != cache.DigestOf([]byte("true")) {
		test.Errorf("the answer encoded %q digests to %v, want %v, its JSON's", answer.Encoded, answer.Digest,
			cache.DigestOf([]byte("true")))
	}
}

// An action that failed exported nothing an importer could trust, though the checker keeps what it exported before it
// failed: a later wave that fails on a package leaves the census the earlier wave's fact, never its own.
func TestAFailedFactWaveDoesNotReplaceASuccessfulOne(test *testing.T) {
	test.Parallel()
	facts := waveFactsOf(test, waveAnalyzer(1, false), waveAnalyzer(2, true))
	if waves := wavesOf(facts); !slices.Equal(waves, []int{1}) {
		test.Errorf("the census holds the facts of waves %v, want the first wave's alone", waves)
	}
}

// A later wave's action on a package is the one that counts: one that exports nothing leaves the census nothing of the
// earlier wave, which exported a fact there.
func TestALaterFactWaveCanRemoveAnEarlierExport(test *testing.T) {
	test.Parallel()
	facts := waveFactsOf(test, waveAnalyzer(1, false), waveAnalyzer(0, false))
	if len(facts) != 0 {
		test.Errorf("the census holds the facts of waves %v, want none: the later wave exported none",
			wavesOf(facts))
	}
}
