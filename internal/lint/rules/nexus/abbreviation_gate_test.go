package nexus

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/text_rules/abbreviation"
)

// The gate must be a superset of what the vocabulary reports, and this is the only thing that proves
// it.
//
// It runs the vocabulary itself with no gate in front of it, which is stronger than the regular
// expression this used to compare against: that pattern was a hand-written description of the
// branches, and the vocabulary is the branches.
//
// The asymmetry is the whole point and it is stated in the rule's own comment: a false positive here
// costs one cheap traversal, while a false negative silently stops the rule firing, and a rule that
// stops firing looks exactly like a clean run. So over-approximation passes and under-approximation
// fails loudly.
//
// The corpus is generated from the vocabulary's own tables rather than written by hand, because a
// hand-written list is exactly where this goes wrong. Two independent attempts at this gate lost
// `elapsedMs` to the same subtle cause: splitting a name at every uppercase letter puts `Ms` in its
// own word and discards the lowercase letter before it, and that boundary is precisely what the
// `[a-z]Ms($|[A-Z])` arm tests. A list somebody wrote from reading the pattern does not contain the
// case they misread. abbreviation.SyntheticNames sets every abbreviation between every kind of
// character a name can hold on either side of it, which holds `userMs` and `userMsName`, and
// abbreviation.SyntheticCleanNames adds the expansions the rule tells a writer to use, which it must
// not report.
func TestAbbreviationGateHasNoFalseNegatives(test *testing.T) {
	test.Parallel()

	corpus := slices.Concat(abbreviation.SyntheticNames(vocabulary), abbreviation.SyntheticCleanNames(vocabulary))

	falseNegatives := []string{}
	for _, name := range corpus {
		if _, reported := findAbbreviation(name, true); reported && !isAbbreviationCandidate(name) {
			falseNegatives = append(falseNegatives, name)
		}
	}

	if len(falseNegatives) != 0 {
		shown := falseNegatives
		if len(shown) > 20 {
			shown = shown[:20]
		}
		test.Fatalf("gate misses %d names the vocabulary reports, which would silence the rule for each: %v",
			len(falseNegatives), shown)
	}
}

// Over-approximation is allowed, but a gate that admits everything is not a gate.
//
// This is the vacuous-pass guard: a gate returning true unconditionally has zero false negatives and
// is worthless, and the test above would call it correct. Measuring how many clean names the gate
// admits turns "no false negatives" into a claim about a real filter. The clean names are where the
// measure means something: each holds an expansion of some abbreviation and none is reported, so
// every one the gate admits is a traversal spent for nothing. And a gate returning false
// unconditionally would admit no clean name and pass that measure, so the gate must also admit the
// synthetic names the vocabulary reports.
func TestAbbreviationGateStaysSelective(test *testing.T) {
	test.Parallel()

	clean := abbreviation.SyntheticCleanNames(vocabulary)
	// A clean set that came back nearly empty would make any fraction of it meaningless.
	if len(clean) < 2000 {
		test.Fatalf("only %d clean names, too few to measure the gate by", len(clean))
	}
	admitted := 0
	for _, name := range clean {
		if isAbbreviationCandidate(name) {
			admitted++
		}
	}

	synthetic := abbreviation.SyntheticNames(vocabulary)
	syntheticAdmitted := 0
	reported := 0
	reportedAndAdmitted := 0
	for _, name := range synthetic {
		if isAbbreviationCandidate(name) {
			syntheticAdmitted++
		}
		if _, found := findAbbreviation(name, true); found {
			reported++
			if isAbbreviationCandidate(name) {
				reportedAndAdmitted++
			}
		}
	}
	if reportedAndAdmitted == 0 {
		test.Fatalf("gate admits none of the %d synthetic names the vocabulary reports, so it filters nothing in",
			reported)
	}

	// Measured on 2026-11-25: the gate admitted 0 of 29,280 clean names, 0%. The bound, 1%, leaves
	// headroom for a vocabulary that grows, but not for a gate that puts the expensive work back.
	const maximumAdmittedFraction = 0.01
	if float64(admitted) > float64(len(clean))*maximumAdmittedFraction {
		test.Fatalf("gate admits %d of %d clean names (%.1f%%), which is too coarse to be worth having",
			admitted, len(clean), 100*float64(admitted)/float64(len(clean)))
	}
	test.Logf("clean %d, gate admits %d; synthetic %d, gate admits %d, vocabulary reports %d, gate admits %d of those",
		len(clean), admitted, len(synthetic), syntheticAdmitted, reported, reportedAndAdmitted)
}

// The arms the two failed attempts lost, pinned by name so a regression names itself.
//
// Every one of these is a spelling the vocabulary reports. They are here in addition to the corpus
// test, not instead of it: the corpus proves the gate today, and these say which shapes were
// historically easy to drop.
func TestAbbreviationGateCoversTheArmsThatWereLost(test *testing.T) {
	test.Parallel()

	cases := []string{
		// The `[a-z]Ms($|[A-Z])` arm. Splitting at uppercase discards the lowercase letter before
		// `Ms`, which is the boundary the arm tests.
		"elapsedMs", "timeoutMs", "delayMs", "elapsedMsTotal",
		// Mid-name word segments, the `(Cwd|Dir|Env|...)($|[A-Z0-9])` arm.
		"workingCwd", "outputDir", "processEnv", "outputDirName", "maxLen", "nextSeq",
		// Whole-word names.
		"props", "params", "ctx", "opts", "queryFn",
		// Prefix arm.
		"ctxValue", "optsBag", "idxOf", "configName",
		// Suffix arm.
		"eventProps", "routeParams", "elementRef", "handlerFn",
	}
	for _, name := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if _, reported := findAbbreviation(name, true); !reported {
				test.Fatalf("the vocabulary does not report %q, so it proves nothing", name)
			}
			if !isAbbreviationCandidate(name) {
				test.Fatalf("gate misses %q, which the vocabulary reports", name)
			}
		})
	}
}
