package main

import (
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/variational_types/canonical"
	"github.com/system-inc/cohere/internal/variational_types/types"
)

// differencesKept is how many differences a build's proof spells out.
const differencesKept = 20

// The kinds of difference between the projection and the oracle.
const (
	differenceMismatched    = "Mismatched"
	differenceOnlyProjected = "OnlyProjected"
	differenceOnlyPlain     = "OnlyPlain"
	differenceAmbiguous     = "Ambiguous"
)

// plantedSuffix is what --plant-mismatch adds to one projected answer, so the proof must show it red.
const plantedSuffix = " (planted)"

// projection is the variational check's answers projected to one build: each expression whose answers hold the build,
// spelled as the oracle spells its own.
type projection struct {
	answers map[expressionKey]string
	// noPosition counts expressions with no position holding the build, left out as the oracle leaves its own out.
	noPosition int
	// ambiguous are the expressions more than one answer of which holds the build, each kept at its first.
	ambiguous []expressionKey
}

// projectTo is checked's answers in build.
func projectTo(fileSet *token.FileSet, checked []*checkedPackage, build int) projection {
	projected := projection{answers: map[expressionKey]string{}}
	bit := types.Mask(1) << build
	for _, each := range checked {
		for expression, answers := range each.variation.Types {
			held := 0
			for _, answer := range answers {
				if answer.Builds&bit == 0 {
					continue
				}
				held++
				if !expression.Pos().IsValid() {
					if held == 1 {
						projected.noPosition++
					}
					continue
				}
				if held == 1 {
					projected.answers[keyOf(fileSet, expression)] = canonical.Answer(answer.TypeAndValue)
				}
			}
			if held > 1 && expression.Pos().IsValid() {
				projected.ambiguous = append(projected.ambiguous, keyOf(fileSet, expression))
			}
		}
	}
	return projected
}

// plantMismatch changes the projected answer of the first expression by position, and is its key.
//
// Mutates: projected gets the planted answer.
func plantMismatch(projected projection) expressionKey {
	keys := sortedExpressionKeys(projected.answers)
	if len(keys) == 0 {
		return expressionKey{}
	}
	projected.answers[keys[0]] += plantedSuffix
	return keys[0]
}

// proveBuild compares the projection to build with the oracle's plain answers for it, every expression either side
// holds. An expression more than one of whose answers holds the build is ambiguous, counted apart and not compared.
func proveBuild(target string, projected projection, oracle *oracleBuild, sources *sourceCache) buildProof {
	proof := buildProof{
		Target:              target,
		OracleDuplicates:    oracle.duplicates,
		NoPositionPlain:     oracle.noPosition,
		NoPositionProjected: projected.noPosition,
		OraclePackageErrors: oracle.packageErrors,
		OracleFirstErrors:   oracle.firstErrors,
		OracleLoad:          oracle.load,
	}
	ambiguous := map[expressionKey]bool{}
	var ambiguities, mismatches, oneSided []difference
	for _, key := range projected.ambiguous {
		ambiguous[key] = true
		proof.Ambiguous++
		ambiguities = append(ambiguities,
			sources.differenceAt(differenceAmbiguous, key, projected.answers[key], oracle.answers[key]))
	}
	for key, projectedAnswer := range projected.answers {
		plain, inOracle := oracle.answers[key]
		switch {
		case ambiguous[key] && inOracle:
			continue
		case !inOracle:
			proof.OnlyProjected++
			oneSided = append(oneSided, sources.differenceAt(differenceOnlyProjected, key, projectedAnswer, ""))
		case plain == projectedAnswer:
			proof.Compared++
			proof.Matched++
		default:
			proof.Compared++
			proof.Mismatched++
			mismatches = append(mismatches, sources.differenceAt(differenceMismatched, key, projectedAnswer, plain))
		}
	}
	for key, plain := range oracle.answers {
		if _, projectedToo := projected.answers[key]; !projectedToo {
			proof.OnlyPlain++
			oneSided = append(oneSided, sources.differenceAt(differenceOnlyPlain, key, "", plain))
		}
	}
	proof.FirstMismatches = firstDifferences(mismatches)
	proof.FirstOneSided = firstDifferences(oneSided)
	proof.FirstAmbiguous = firstDifferences(ambiguities)
	proof.Green = proof.Mismatched == 0 && proof.OnlyProjected == 0 && proof.OnlyPlain == 0 && proof.Ambiguous == 0 &&
		proof.Compared > 0
	return proof
}

// firstDifferences is differences by position, the first differencesKept.
func firstDifferences(differences []difference) []difference {
	sort.Slice(differences, func(left int, right int) bool {
		return differences[left].sortKey() < differences[right].sortKey()
	})
	kept := []difference{}
	if len(differences) > differencesKept {
		differences = differences[:differencesKept]
	}
	return append(kept, differences...)
}

// sortedExpressionKeys is answers' keys by file, then offsets, then kind.
func sortedExpressionKeys(answers map[expressionKey]string) []expressionKey {
	keys := make([]expressionKey, 0, len(answers))
	for key := range answers {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left int, right int) bool {
		if keys[left].file != keys[right].file {
			return keys[left].file < keys[right].file
		}
		if keys[left].start != keys[right].start {
			return keys[left].start < keys[right].start
		}
		if keys[left].end != keys[right].end {
			return keys[left].end < keys[right].end
		}
		return keys[left].kind < keys[right].kind
	})
	return keys
}

// expressionTextLimit is how much of an expression's source a difference spells.
const expressionTextLimit = 120

// sourceCache reads each file of ours once, to spell a difference's line and text.
type sourceCache struct {
	contents map[string][]byte
}

// differenceAt is a difference of kind at key.
func (sources *sourceCache) differenceAt(kind string, key expressionKey, projected string, plain string) difference {
	source := sources.read(key.file)
	spelled := difference{
		Kind:      kind,
		File:      key.file,
		Start:     key.start,
		End:       key.end,
		Node:      key.kind,
		Projected: projected,
		Plain:     plain,
	}
	if key.start <= key.end && key.end <= len(source) {
		spelled.Line = 1 + strings.Count(string(source[:key.start]), "\n")
		spelled.Expression = strings.Join(strings.Fields(string(source[key.start:key.end])), " ")
		if len(spelled.Expression) > expressionTextLimit {
			spelled.Expression = spelled.Expression[:expressionTextLimit] + "..."
		}
	}
	return spelled
}

// read is file's bytes, read once, or none when it can't be read.
func (sources *sourceCache) read(file string) []byte {
	if cached, read := sources.contents[file]; read {
		return cached
	}
	contents, readError := os.ReadFile(file)
	if readError != nil {
		contents = nil
	}
	sources.contents[file] = contents
	return contents
}
