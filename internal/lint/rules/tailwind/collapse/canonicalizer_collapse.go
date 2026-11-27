// The two questions enforce-canonical-classes asks of signatures once its family table has named a
// collapse (#sbb2hke): does the shorthand compile to what the pair compiles to together, and how does
// upstream spell it.
//
// Upstream's collapseGroup finds a collapse by signature, not by root. It signs the pair as one `@apply`,
// gathers every class that sets the same properties and keeps the first whose own signature is the pair's.
// The classes it gathers are the listed ones first, in getClassList order, since bareValueUtilities and
// arbitraryUtilities have signed the whole list before any collapse runs, and after them the pair's roots
// swapped for every other functional root. So `w-rail h-rail` is `size-14`, the listed class that sorts
// first, and not `size-rail`, which the root swap would print; and `w-px h-px` is `size-px` although
// `w-px` is a static, because the listed `size-px` signs as the pair does.
package tailwind

import (
	"regexp"
	"sort"
	"strings"
)

// CollapsesTo is whether output compiles to exactly what inputs compile to together, upstream's test
// that a replacement is safe for a combination. False whenever either side cannot be signed exactly, so a
// collapse this port cannot check is not reported.
func (canonicalizer *Canonicalizer) CollapsesTo(inputs []string, output string) bool {
	combined, known := canonicalizer.collapsedSignature(inputs)
	if !known {
		return false
	}
	signature, state := canonicalizer.classSignature(output)
	return state == SignatureExact && signature == combined
}

// CollapseSpelling is the class upstream writes for inputs collapsed together: the first listed class, in
// getClassList order, whose signature is the combination's. written is the root swap the caller printed,
// which upstream reaches only when no listed class signs alike, and it is returned whenever this port
// cannot tell which listed class upstream would take: a combination it cannot sign, a list it cannot sign
// whole, or an order it cannot rebuild. The inputs and written carry no variants and no importance, as
// upstream collapses each group with those taken off.
func (canonicalizer *Canonicalizer) CollapseSpelling(inputs []string, written string) string {
	combined, known := canonicalizer.collapsedSignature(inputs)
	if !known {
		return written
	}
	index := canonicalizer.shared.classIndex()
	if index.redeclared {
		return written
	}
	pool := canonicalizer.pool(SignatureProperties(combined))
	if pool.uncertain {
		return written
	}
	listed, ordered := firstInClassListOrder(pool.exact[combined])
	if !ordered {
		return written
	}
	return listed
}

// collapsedSignature is upstream's signature of the classes applied together, `.x { @apply a b }`. The
// bodies are concatenated in each order and the answer is known only when both orders sign alike, since
// upstream's order is its compile sort, which this port does not rebuild for a combination.
func (canonicalizer *Canonicalizer) collapsedSignature(inputs []string) (string, bool) {
	if len(inputs) != 2 {
		return "", false
	}
	type combined struct {
		signature string
		known     bool
	}
	key := inputs[0] + " " + inputs[1]
	if remembered, found := canonicalizer.combinations.Load(key); found {
		answer, isCombined := remembered.(combined)
		if !isCombined {
			// Invariant: combinations holds only the combined collapsedSignature stores below.
			panic("tailwind: the combination memo holds something other than a signature")
		}
		return answer.signature, answer.known
	}
	answer := combined{}
	forward, forwardKnown := canonicalizer.concatenatedSignature(inputs[0], inputs[1])
	backward, backwardKnown := canonicalizer.concatenatedSignature(inputs[1], inputs[0])
	if forwardKnown && backwardKnown && forward == backward {
		answer = combined{signature: forward, known: true}
	}
	canonicalizer.combinations.Store(key, answer)
	return answer.signature, answer.known
}

// concatenatedSignature signs first's body followed by second's, as ClassSignature signs one class.
func (canonicalizer *Canonicalizer) concatenatedSignature(first string, second string) (string, bool) {
	compiler := canonicalizer.shared.classIndex().compiler
	var nodes []*Node
	for _, className := range []string{first, second} {
		body, state := compiler.signatureNodes(className)
		if state != SignatureExact {
			return "", false
		}
		nodes = append(nodes, body...)
	}
	if !substituteThemeFunctions(nodes, compiler.compileTheme) {
		return "", false
	}
	return UtilitySignature(nodes, canonicalizer.system.Theme(), canonicalizer.features)
}

// classListFraction is upstream's IS_FRACTION, the values getClassList sorts after their utility's others.
var classListFraction = regexp.MustCompile(`^\d+/\d+$`)

// firstInClassListOrder is the class among matches that comes first in upstream's getClassList: sorted by
// compareClassNames, then each utility's fractions moved after its other classes (sortFractionsLast).
//
// A match's utility is read as its name up to the last dash, and its fraction as what follows. That
// reading is only trusted where it cannot decide the answer wrongly, so the order is refused when a
// fraction competes with a class of another utility, whose bucket this does not rebuild, and when a match
// carries a modifier, which upstream signs right after its class rather than where its name sorts.
func firstInClassListOrder(matches []string) (string, bool) {
	if len(matches) == 0 {
		return "", false
	}
	type listed struct {
		name     string
		utility  string
		fraction bool
	}
	entries := make([]listed, 0, len(matches))
	utilities := map[string]bool{}
	hasFraction := false
	for _, name := range matches {
		cut := strings.LastIndex(name, "-")
		utility, value := name, ""
		if cut > 0 {
			utility, value = name[:cut], name[cut+1:]
		}
		fraction := classListFraction.MatchString(value)
		if strings.Contains(name, "/") && !fraction {
			return "", false
		}
		hasFraction = hasFraction || fraction
		utilities[utility] = true
		entries = append(entries, listed{name: name, utility: utility, fraction: fraction})
	}
	if hasFraction && len(utilities) > 1 {
		return "", false
	}
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].fraction != entries[right].fraction {
			return !entries[left].fraction
		}
		return compareClassNames(entries[left].name, entries[right].name) < 0
	})
	return entries[0].name, true
}

// compareClassNames is upstream's utils/compare.ts, the order getClassList sorts by: character codes,
// except that two runs of digits compare as numbers, and a name that is a prefix of another comes first.
func compareClassNames(left string, right string) int {
	isDigit := func(character byte) bool { return character >= '0' && character <= '9' }
	shorter := len(left)
	if len(right) < shorter {
		shorter = len(right)
	}
	for position := 0; position < shorter; position++ {
		leftCharacter, rightCharacter := left[position], right[position]
		if isDigit(leftCharacter) && isDigit(rightCharacter) {
			leftEnd, rightEnd := position+1, position+1
			for leftEnd < len(left) && isDigit(left[leftEnd]) {
				leftEnd++
			}
			for rightEnd < len(right) && isDigit(right[rightEnd]) {
				rightEnd++
			}
			leftNumber, rightNumber := left[position:leftEnd], right[position:rightEnd]
			// Number(a) - Number(z) without parsing: two digit runs differ in value by their length once
			// leading zeros are set aside, and by their text when the lengths agree.
			leftTrimmed, rightTrimmed := strings.TrimLeft(leftNumber, "0"), strings.TrimLeft(rightNumber, "0")
			if len(leftTrimmed) != len(rightTrimmed) {
				return len(leftTrimmed) - len(rightTrimmed)
			}
			if leftTrimmed != rightTrimmed {
				return strings.Compare(leftTrimmed, rightTrimmed)
			}
			if leftNumber != rightNumber {
				return strings.Compare(leftNumber, rightNumber)
			}
			// Upstream continues from the character after the first digit, not after the run, so the rest of
			// both runs is compared again digit by digit; equal runs make that a no-op.
			continue
		}
		if leftCharacter != rightCharacter {
			return int(leftCharacter) - int(rightCharacter)
		}
	}
	return len(left) - len(right)
}
