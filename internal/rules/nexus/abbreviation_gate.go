package nexus

// The candidate gate, expressed as lookups rather than as a regular expression.
//
// This decides whether a name is worth spending the rest of the rule on, and it runs once per
// identifier in the tree. Measured on the ahra tree that is 667,947 calls, and the regular
// expression form cost 1,830ms of a 2,825ms total, 64.8% of all rule time. Benchmarked against the
// real pattern: 2,247 ns/op for the regex, 29.68 ns/op for this shape, a factor of 76.
//
// The regex is not badly written and the port was faithful. Go's regexp is RE2, which does not
// backtrack, and a ninety-alternative alternation has different constant factors there than under
// V8. **A regex that is cheap in JavaScript is not automatically cheap in Go**, and a per-node gate
// is where that difference gets multiplied by two million. Nothing about the rule's logic changed.
//
// Every alternative in the pattern is a literal word, or a literal word plus a case boundary, so
// there is no backtracking behavior to preserve and a map lookup answers the same question.
//
// The direction of error is the whole discipline here, and it is inherited from the pattern's own
// comment: this must be a **superset** of what the branches match. A false positive costs one cheap
// traversal that reports nothing. A false negative silences the rule for that spelling, and a rule
// that stops firing looks exactly like a clean run. `abbreviation_gate_test.go` asserts the superset
// property over 82,367 distinct identifiers from the ahra tree, which is the only thing that proves
// it: two earlier attempts at this gate both lost `elapsedMs`, and a hand-written list does not
// contain the case its author misread.

// gateWholeWordNames are names that are an abbreviation entire.
//
// From the pattern's first arm, which anchors both ends.
var gateWholeWordNames = map[string]bool{
	"prop": true, "props": true, "param": true, "params": true, "ref": true, "config": true,
	"idx": true, "arg": true, "args": true, "acc": true, "char": true, "fn": true, "str": true,
	"val": true, "arr": true, "obj": true, "num": true, "res": true, "err": true, "req": true,
	"msg": true, "min": true, "max": true, "prev": true, "cur": true, "pct": true, "opts": true,
	"ctx": true, "db": true, "tx": true, "queryFn": true, "mutationFn": true,
}

// gatePrefixNames begin a camelCase name, so the next character is uppercase.
//
// From the pattern's second arm. Deliberately not the same set as the whole-word arm: `acc` and the
// two TanStack names appear only as whole words in the pattern, and widening here would be a change
// in behavior rather than a change in representation.
var gatePrefixNames = map[string]bool{
	"ctx": true, "db": true, "tx": true, "opts": true, "cur": true, "pct": true, "prev": true,
	"idx": true, "config": true, "prop": true, "props": true, "param": true, "params": true,
	"ref": true, "arg": true, "args": true, "char": true, "fn": true, "str": true, "val": true,
	"arr": true, "obj": true, "num": true, "res": true, "err": true, "req": true, "msg": true,
	"min": true, "max": true,
}

// gateSuffixNames end a camelCase name, so they are capitalized and reach the end.
//
// From the pattern's third arm.
var gateSuffixNames = map[string]bool{
	"Prop": true, "Props": true, "Param": true, "Params": true, "Ref": true, "Config": true,
	"Idx": true, "Arg": true, "Args": true, "Char": true, "Fn": true, "Str": true, "Val": true,
	"Arr": true, "Obj": true, "Num": true, "Res": true, "Err": true, "Req": true, "Msg": true,
	"Min": true, "Max": true,
}

// gateSegmentNames appear as a camelCase word anywhere in a name, followed by the end, another
// word, or a digit.
//
// From the pattern's fifth arm. `Ms` is handled separately because its arm tests the character
// *before* it as well.
var gateSegmentNames = map[string]bool{
	"Cwd": true, "Dir": true, "Env": true, "Cli": true, "Len": true, "Seq": true,
	"Db": true, "Tx": true, "Vars": true, "Var": true,
}

// longestAbbreviationLength bounds the substring scans below, so a long name does not cost work
// proportional to its length times the table size.
const longestAbbreviationLength = 10

// isAbbreviationCandidate reports whether a name could match any branch of the rule.
//
// A superset of the branches, never a subset. See the file comment for why that direction is not
// negotiable.
func isAbbreviationCandidate(name string) bool {
	if name == "" {
		return false
	}

	// First arm: the whole name is an abbreviation.
	if gateWholeWordNames[name] {
		return true
	}

	// Second arm: an abbreviation prefix followed by an uppercase letter.
	//
	// Scanning to the first uppercase letter finds the only split the arm can match, since the
	// prefix in the pattern is anchored at the start and is entirely lowercase.
	for index := 1; index < len(name) && index <= longestAbbreviationLength; index++ {
		if isUppercaseAsciiLetter(name[index]) {
			if gatePrefixNames[name[:index]] {
				return true
			}
			break
		}
	}

	// Third arm: a capitalized abbreviation at the end.
	//
	// Lengths run from two, not three: `Fn` is the shortest entry in the table, and starting at
	// three silently loses every name ending in it. The corpus caught that, on `accessorFn`,
	// `LanguageFn`, `noFn`, and a bare `Fn`.
	for length := 2; length <= 6 && length <= len(name); length++ {
		if gateSuffixNames[name[len(name)-length:]] {
			return true
		}
	}

	// Fourth arm: `[a-z]Ms($|[A-Z])`. A lowercase letter, then `Ms`, then the end or a new word.
	//
	// This arm is why the gate cannot be built by splitting the name at every uppercase letter and
	// checking the words: that split puts `Ms` in its own word and discards the lowercase letter
	// before it, which is precisely the boundary being tested. Two independent attempts lost
	// `elapsedMs` exactly here.
	for index := 1; index+2 <= len(name); index++ {
		if name[index] != 'M' || name[index+1] != 's' {
			continue
		}
		if !isLowercaseAsciiLetter(name[index-1]) {
			continue
		}
		atEnd := index+2 == len(name)
		if atEnd || isUppercaseAsciiLetter(name[index+2]) {
			return true
		}
	}

	// Fifth arm: a capitalized abbreviation segment, followed by the end, a new word, or a digit.
	//
	// The scan starts at zero rather than one, because this arm is unanchored in the pattern: a name
	// that *is* the segment, or starts with it, matches. Starting at one lost `Cwd`, `Dir`, `Env`,
	// `Len`, `Seq`, `Var`, and `DbD` against the corpus.
	for index := 0; index < len(name); index++ {
		if !isUppercaseAsciiLetter(name[index]) {
			continue
		}
		for length := 2; length <= 4 && index+length <= len(name); length++ {
			if !gateSegmentNames[name[index:index+length]] {
				continue
			}
			after := index + length
			if after == len(name) || isUppercaseAsciiLetter(name[after]) || isAsciiDigit(name[after]) {
				return true
			}
		}
	}

	return false
}

func isUppercaseAsciiLetter(character byte) bool {
	return character >= 'A' && character <= 'Z'
}

func isLowercaseAsciiLetter(character byte) bool {
	return character >= 'a' && character <= 'z'
}

func isAsciiDigit(character byte) bool {
	return character >= '0' && character <= '9'
}
